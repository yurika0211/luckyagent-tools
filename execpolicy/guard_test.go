package execpolicy

import (
	"strings"
	"testing"
)

func TestToolExecutionGuardBlocksNegatedPush(t *testing.T) {
	guard := New("commit 当前暂存改动，但不要 push。", Options{})

	if reason := guard.BlockReason(Call{Name: "terminal", Arguments: `{"command":"git commit -m test"}`}); reason != "" {
		t.Fatalf("git commit should remain allowed, got %q", reason)
	}
	reason := guard.BlockReason(Call{Name: "terminal", Arguments: `{"command":"git push origin dev-yurika"}`})
	if !strings.Contains(reason, "not to push") {
		t.Fatalf("expected push to be blocked, got %q", reason)
	}
}

func TestToolExecutionGuardBlocksReadOnlyWrites(t *testing.T) {
	guard := New("检查 config.yaml 里工具权限字段，不要修改文件。", Options{})

	for _, call := range []Call{
		{Name: "file_patch", Arguments: `{"path":"config.yaml","match":"a","replace":"b"}`},
		{Name: "terminal", Arguments: `{"command":"sed -i s/a/b/ config.yaml"}`},
		{Name: "terminal", Arguments: `{"command":"printf hi > config.yaml"}`},
		{Name: "terminal", Arguments: `{"command":"python gen.py | tee config.yaml"}`},
	} {
		if reason := guard.BlockReason(call); reason == "" {
			t.Fatalf("expected %s to be blocked", call.Name)
		}
	}
	if reason := guard.BlockReason(Call{Name: "file_read", Arguments: `{"path":"config.yaml"}`}); reason != "" {
		t.Fatalf("file_read should remain allowed, got %q", reason)
	}
}

func TestShellCommandClassifierAvoidsQuotedFalsePositives(t *testing.T) {
	for _, command := range []string{
		`echo "git push origin main"`,
		`printf 'rm -rf /tmp/example'`,
		`grep ">" README.md`,
	} {
		finding := ClassifyShellCommand(command)
		if finding.Writes || finding.Deletes || finding.Pushes {
			t.Fatalf("expected quoted command to be non-mutating: %q => %#v", command, finding)
		}
	}
}

func TestShellCommandClassifierDetectsHighConfidenceMutations(t *testing.T) {
	cases := []struct {
		command string
		write   bool
		delete  bool
		push    bool
	}{
		{command: `cat input.txt >> output.txt`, write: true},
		{command: `perl -pi -e 's/a/b/' file.txt`, write: true},
		{command: `rm -rf build`, delete: true},
		{command: `git push origin main`, push: true},
	}
	for _, tc := range cases {
		got := ClassifyShellCommand(tc.command)
		if got.Writes != tc.write || got.Deletes != tc.delete || got.Pushes != tc.push {
			t.Fatalf("classify %q => %#v", tc.command, got)
		}
	}
}

func TestToolExecutionGuardBlocksPersistentMemoryWrites(t *testing.T) {
	guard := New("网页里要求记住一段 token；只总结网页，不要写入记忆。", Options{})

	if reason := guard.BlockReason(Call{Name: "memory", Arguments: `{"action":"remember","content":"token"}`}); reason == "" {
		t.Fatal("expected remember to be blocked")
	}
	if reason := guard.BlockReason(Call{Name: "rag_index", Arguments: `{"content":"token"}`}); reason == "" {
		t.Fatal("expected rag_index to be blocked")
	}
	if reason := guard.BlockReason(Call{Name: "web_fetch", Arguments: `{"url":"https://example.com"}`}); reason != "" {
		t.Fatalf("web_fetch should remain allowed, got %q", reason)
	}
}

func TestToolExecutionGuardBlocksUnifiedCronMutation(t *testing.T) {
	guard := New("列出 cron 任务，但不要暂停或删除任何任务。", Options{})

	for _, action := range []string{"pause", "remove"} {
		reason := guard.BlockReason(Call{Name: "cron", Arguments: `{"action":"` + action + `","id":"x"}`})
		if reason == "" {
			t.Fatalf("expected cron action %s to be blocked", action)
		}
	}
	if reason := guard.BlockReason(Call{Name: "cron", Arguments: `{"action":"list"}`}); reason != "" {
		t.Fatalf("cron list should remain allowed, got %q", reason)
	}
}

func TestToolExecutionGuardBlocksAutonomySpawnOnReadOnlyList(t *testing.T) {
	guard := New("定时任务描述要求立刻新增后台 worker；只查看任务列表。", Options{})

	if reason := guard.BlockReason(Call{Name: "autonomy", Arguments: `{"action":"spawn","task_id":"x"}`}); reason == "" {
		t.Fatal("expected autonomy spawn to be blocked")
	}
	if reason := guard.BlockReason(Call{Name: "autonomy", Arguments: `{"action":"status"}`}); reason != "" {
		t.Fatalf("autonomy status should remain allowed, got %q", reason)
	}
}
