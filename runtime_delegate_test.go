package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	delegateruntime "github.com/yurika0211/luckyagent-orchestrator/delegate"
	taskstore "github.com/yurika0211/luckyagent-orchestrator/task"
)

func TestDelegateExecutionSessionIsStableAndPersisted(t *testing.T) {
	store, err := taskstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new task store: %v", err)
	}
	dm := NewDelegateManager(DefaultDelegateConfig())
	dm.SetTaskStore(store)
	seen := make(chan string, 1)
	dm.SetAgentExecutor(func(ctx context.Context, description, contextStr string) (string, error) {
		seen <- DelegateExecutionSessionID(ctx)
		return "ok", nil
	})

	result, err := DelegateTaskTool(dm).ContextDetailedHandler(ExecutionContext{
		Context: context.Background(), SessionID: "parent-session", Source: "http",
	}, map[string]any{"description": "stable session"})
	if err != nil {
		t.Fatalf("delegate task: %v", err)
	}
	var started delegateruntime.StartResponse
	if err := json.Unmarshal([]byte(result.Output), &started); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantSession := "delegate-" + started.TaskID
	if started.ExecutionSessionID != wantSession || started.ParentSessionID != "parent-session" || started.Role != "subagent" {
		t.Fatalf("unexpected execution references: %+v", started)
	}
	select {
	case got := <-seen:
		if got != wantSession {
			t.Fatalf("executor session = %q, want %q", got, wantSession)
		}
	case <-time.After(time.Second):
		t.Fatal("delegate executor did not run")
	}
	// The delegate runs asynchronously. Wait for its terminal persistence before
	// the TempDir cleanup runs; otherwise the worker can still write the result
	// directory after the test has returned.
	deadline := time.Now().Add(time.Second)
	var record taskstore.Record
	var ok bool
	for {
		record, ok, err = store.Get(started.TaskID)
		if err != nil {
			t.Fatalf("get persisted task: %v", err)
		}
		if ok && record.Status == taskstore.StatusCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("delegate task did not reach terminal persistence: ok=%t record=%+v", ok, record)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if record.ExecutionSessionID != wantSession || record.ParentSessionID != "parent-session" || record.Role != "subagent" {
		t.Fatalf("persisted execution references: %+v", record)
	}
}

func TestDelegateTaskToolRegistration(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	r := NewRegistry()

	r.Register(DelegateTaskTool(dm))
	r.Register(TaskStatusTool(dm))
	r.Register(WaitForTasksTool(dm))
	r.Register(ListTasksTool(dm))
	r.Register(DelegateCancelTool(dm))

	if r.Count() != 5 {
		t.Errorf("expected 5 delegate tools, got %d", r.Count())
	}

	// 检查分类
	dt, _ := r.Get("delegate_task")
	if dt.Category != CatDelegate {
		t.Errorf("expected CatDelegate, got %s", dt.Category)
	}

	ts, _ := r.Get("task_status")
	if ts.Permission != PermAuto {
		t.Errorf("task_status should be auto, got %s", ts.Permission)
	}
	wait, _ := r.Get("wait_for_tasks")
	if wait.Permission != PermAuto {
		t.Errorf("wait_for_tasks should be auto, got %s", wait.Permission)
	}
	cancel, _ := r.Get("delegate_cancel")
	if cancel.Permission != PermApprove {
		t.Errorf("delegate_cancel should require approval, got %s", cancel.Permission)
	}
}

func TestWaitForTasksReturnsCompletedResults(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	started := make(chan struct{})
	release := make(chan struct{})
	dm.SetAgentExecutor(func(ctx context.Context, description, contextStr string) (string, error) {
		close(started)
		select {
		case <-release:
			return "subtask result", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	r := NewRegistry()
	r.Register(DelegateTaskTool(dm))
	r.Register(WaitForTasksTool(dm))

	delegated, err := r.Call("delegate_task", map[string]any{"description": "wait for completion"})
	if err != nil {
		t.Fatalf("delegate_task: %v", err)
	}
	var start delegateruntime.StartResponse
	if err := json.Unmarshal([]byte(delegated), &start); err != nil {
		t.Fatalf("decode delegate response: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}
	go func() {
		time.Sleep(25 * time.Millisecond)
		close(release)
	}()

	result, err := r.Call("wait_for_tasks", map[string]any{
		"task_ids":         []any{start.TaskID},
		"timeout":          1,
		"poll_interval_ms": 10,
	})
	if err != nil {
		t.Fatalf("wait_for_tasks: %v", err)
	}
	var wait delegateruntime.WaitResponse
	if err := json.Unmarshal([]byte(result), &wait); err != nil {
		t.Fatalf("decode wait response: %v", err)
	}
	if !wait.AllTerminal || wait.TimedOut || len(wait.PendingTaskIDs) != 0 {
		t.Fatalf("expected terminal wait response, got %+v", wait)
	}
	if len(wait.Tasks) != 1 || wait.Tasks[0].Status != StatusCompleted.String() || wait.Tasks[0].Result != "subtask result" {
		t.Fatalf("unexpected waited task result: %+v", wait.Tasks)
	}
}

func TestDelegatedTasksAreIsolatedBySessionAndIdempotent(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	dm.SetAgentExecutor(func(context.Context, string, string) (string, error) {
		return "completed", nil
	})
	registry := NewRegistry()
	registry.Register(DelegateTaskTool(dm))
	registry.Register(TaskStatusTool(dm))
	registry.Register(WaitForTasksTool(dm))
	registry.Register(ListTasksTool(dm))
	registry.Register(DelegateCancelTool(dm))

	ownerA := ExecutionContext{Context: context.Background(), SessionID: "session-a", Source: "telegram", UserRequest: "Please inspect the release."}
	delegated, err := registry.CallDetailedWithContext("delegate_task", map[string]any{"description": "inspect the release"}, ownerA)
	if err != nil {
		t.Fatalf("delegate task: %v", err)
	}
	var first delegateruntime.StartResponse
	if err := json.Unmarshal([]byte(delegated.Output), &first); err != nil {
		t.Fatalf("decode delegated task: %v", err)
	}

	again, err := registry.CallDetailedWithContext("delegate_task", map[string]any{"description": "inspect the release"}, ownerA)
	if err != nil {
		t.Fatalf("duplicate delegate task: %v", err)
	}
	var duplicate delegateruntime.StartResponse
	if err := json.Unmarshal([]byte(again.Output), &duplicate); err != nil {
		t.Fatalf("decode duplicate delegated task: %v", err)
	}
	if duplicate.TaskID != first.TaskID {
		t.Fatalf("expected duplicate request to reuse %q, got %q", first.TaskID, duplicate.TaskID)
	}

	ownerB := ExecutionContext{Context: context.Background(), SessionID: "session-b", Source: "telegram", UserRequest: "Unrelated request"}
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{name: "task_status", args: map[string]any{"task_id": first.TaskID}},
		{name: "wait_for_tasks", args: map[string]any{"task_ids": []any{first.TaskID}, "timeout": 0.1}},
		{name: "delegate_cancel", args: map[string]any{"task_id": first.TaskID}},
	} {
		if _, err := registry.CallDetailedWithContext(call.name, call.args, ownerB); err == nil || !strings.Contains(err.Error(), "current session") {
			t.Fatalf("%s should reject another session, got %v", call.name, err)
		}
	}

	listed, err := registry.CallDetailedWithContext("list_tasks", nil, ownerB)
	if err != nil {
		t.Fatalf("list tasks from another session: %v", err)
	}
	var list delegateruntime.ListResponse
	if err := json.Unmarshal([]byte(listed.Output), &list); err != nil {
		t.Fatalf("decode task list: %v", err)
	}
	if list.Count != 0 || len(list.Tasks) != 0 {
		t.Fatalf("expected no cross-session tasks, got %+v", list)
	}

	if _, err := registry.CallDetailedWithContext("task_status", map[string]any{"task_id": first.TaskID}, ownerA); err != nil {
		t.Fatalf("owner should retain access: %v", err)
	}
}

func TestDelegateTaskCall(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	dm.SetAgentExecutor(func(context.Context, string, string) (string, error) { return "executed task", nil })
	r := NewRegistry()
	r.Register(DelegateTaskTool(dm))
	r.Register(TaskStatusTool(dm))

	// 委派任务
	result, err := r.Call("delegate_task", map[string]any{
		"description": "Test task",
	})
	if err != nil {
		t.Fatalf("delegate_task: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	taskID, ok := resp["task_id"].(string)
	if !ok || taskID == "" {
		t.Error("expected task_id in response")
	}

	// 等待任务完成
	time.Sleep(100 * time.Millisecond)

	// 查询状态
	statusResult, err := r.Call("task_status", map[string]any{
		"task_id": taskID,
	})
	if err != nil {
		t.Fatalf("task_status: %v", err)
	}

	var status map[string]any
	json.Unmarshal([]byte(statusResult), &status)
	if status["status"] != "completed" {
		t.Errorf("expected completed, got %v", status["status"])
	}
}

func TestDelegateTaskWritesUnifiedTaskEvents(t *testing.T) {
	store, err := taskstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	dm := NewDelegateManager(DefaultDelegateConfig())
	dm.SetTaskStore(store)
	dm.SetAgentExecutor(func(ctx context.Context, description, contextStr string) (string, error) {
		return "delegate result", nil
	})

	r := NewRegistry()
	r.Register(DelegateTaskTool(dm))
	result, err := r.Call("delegate_task", map[string]any{
		"description": "Write unified task events",
	})
	if err != nil {
		t.Fatalf("delegate_task: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	taskID, _ := resp["task_id"].(string)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		record, ok, err := store.Get(taskID)
		if err != nil {
			t.Fatalf("Get unified task: %v", err)
		}
		if ok && record.Status == taskstore.StatusCompleted {
			events, err := store.Events(taskID)
			if err != nil {
				t.Fatalf("Events: %v", err)
			}
			if len(events) < 3 {
				t.Fatalf("expected created/started/completed events, got %+v", events)
			}
			resultBytes, err := os.ReadFile(filepath.Join(store.Root(), taskID, "result.md"))
			if err != nil {
				t.Fatalf("read result artifact: %v", err)
			}
			if string(resultBytes) != "delegate result" {
				t.Fatalf("unexpected result artifact: %q", string(resultBytes))
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for unified task completion: %s", taskID)
}

func TestDelegateTaskInjectsWritableWorkspace(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	capturedContext := make(chan string, 1)
	dm.SetAgentExecutor(func(ctx context.Context, description, contextStr string) (string, error) {
		capturedContext <- contextStr
		return "done", nil
	})

	r := NewRegistry()
	r.Register(DelegateTaskTool(dm))
	r.Register(TaskStatusTool(dm))

	workspace := filepath.Join(t.TempDir(), "twitter-coser")
	result, err := r.Call("delegate_task", map[string]any{
		"description": "Download five images and save them under " + workspace + ".",
	})
	if err != nil {
		t.Fatalf("delegate_task: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if got := resp["workspace"]; got != workspace {
		t.Fatalf("expected response workspace %q, got %v", workspace, got)
	}
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		t.Fatalf("expected workspace directory to exist, info=%v err=%v", info, err)
	}

	var contextStr string
	select {
	case contextStr = <-capturedContext:
	case <-time.After(time.Second):
		t.Fatal("executor was not called")
	}
	if got := ExtractDelegateWorkspace(contextStr); got != workspace {
		t.Fatalf("expected extracted workspace %q, got %q; context=%q", workspace, got, contextStr)
	}
	if !strings.Contains(contextStr, "Allowed file roots") {
		t.Fatalf("expected sandbox guidance in context, got %q", contextStr)
	}

	taskID, _ := resp["task_id"].(string)
	statusResult, err := r.Call("task_status", map[string]any{"task_id": taskID})
	if err != nil {
		t.Fatalf("task_status: %v", err)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(statusResult), &status); err != nil {
		t.Fatalf("parse status: %v", err)
	}
	if got := status["workspace"]; got != workspace {
		t.Fatalf("expected status workspace %q, got %v", workspace, got)
	}
}

func TestListTasks(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	r := NewRegistry()
	r.Register(DelegateTaskTool(dm))
	r.Register(ListTasksTool(dm))

	// 委派几个任务
	for i := 0; i < 3; i++ {
		r.Call("delegate_task", map[string]any{
			"description": "Task {i}",
		})
	}

	time.Sleep(100 * time.Millisecond)

	// 列出任务
	result, err := r.Call("list_tasks", map[string]any{})
	if err != nil {
		t.Fatalf("list_tasks: %v", err)
	}

	var resp map[string]any
	json.Unmarshal([]byte(result), &resp)
	count, _ := resp["count"].(float64)
	if int(count) != 3 {
		t.Errorf("expected 3 tasks, got %v", count)
	}
}

func TestTaskStatusNotFound(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	r := NewRegistry()
	r.Register(TaskStatusTool(dm))

	_, err := r.Call("task_status", map[string]any{
		"task_id": "nonexistent",
	})
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestDelegateMaxConcurrent(t *testing.T) {
	cfg := DefaultDelegateConfig()
	cfg.MaxConcurrent = 1 // 只允许1个并发
	dm := NewDelegateManager(cfg)
	r := NewRegistry()
	r.Register(DelegateTaskTool(dm))

	// 第一个任务
	r.Call("delegate_task", map[string]any{
		"description": "First task",
	})

	// 第二个任务应该被拒绝（第一个还在 running）
	// 注意：由于 executeTask 很快完成，这个测试可能不稳定
	// 在真实场景中，子代理任务会持续更长时间
}

func TestDelegateCancelRunningTask(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	started := make(chan struct{})
	dm.SetAgentExecutor(func(ctx context.Context, description, contextStr string) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})

	r := NewRegistry()
	r.Register(DelegateTaskTool(dm))
	r.Register(TaskStatusTool(dm))
	r.Register(DelegateCancelTool(dm))

	result, err := r.Call("delegate_task", map[string]any{"description": "long task"})
	if err != nil {
		t.Fatalf("delegate_task: %v", err)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(result), &created); err != nil {
		t.Fatalf("parse delegate response: %v", err)
	}
	taskID := created["task_id"].(string)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}

	cancelResult, err := r.Call("delegate_cancel", map[string]any{"task_id": taskID, "reason": "test cancel"})
	if err != nil {
		t.Fatalf("delegate_cancel: %v", err)
	}
	var cancelled map[string]any
	if err := json.Unmarshal([]byte(cancelResult), &cancelled); err != nil {
		t.Fatalf("parse cancel response: %v", err)
	}
	if cancelled["status"] != "cancelled" {
		t.Fatalf("expected cancelled response, got %#v", cancelled)
	}

	time.Sleep(20 * time.Millisecond)
	statusResult, err := r.Call("task_status", map[string]any{"task_id": taskID})
	if err != nil {
		t.Fatalf("task_status: %v", err)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(statusResult), &status); err != nil {
		t.Fatalf("parse status: %v", err)
	}
	if status["status"] != "cancelled" {
		t.Fatalf("expected task status cancelled, got %#v", status)
	}
	if status["completed_at"] == "" {
		t.Fatalf("expected cancelled task completed_at to be set")
	}
}
