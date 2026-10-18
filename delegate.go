package tool

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	delegateruntime "github.com/yurika0211/luckyagent-orchestrator/delegate"
)

// Compatibility names for the independent delegation engine.
type DelegateConfig = delegateruntime.DelegateConfig

var DefaultDelegateConfig = delegateruntime.DefaultDelegateConfig

func ExtractDelegateWorkspace(contextStr string) string {
	return delegateruntime.ExtractDelegateWorkspaceWithPolicy(contextStr, delegateWorkspacePolicy())
}

type TaskStatus = delegateruntime.TaskStatus

const StatusPending = delegateruntime.StatusPending
const StatusRunning = delegateruntime.StatusRunning
const StatusCompleted = delegateruntime.StatusCompleted
const StatusFailed = delegateruntime.StatusFailed
const StatusCancelled = delegateruntime.StatusCancelled

type DelegateTask = delegateruntime.DelegateTask
type AgentExecutorFunc = delegateruntime.AgentExecutorFunc
type AgentExecutorOptions = delegateruntime.AgentExecutorOptions
type AgentExecutorWithOptionsFunc = delegateruntime.AgentExecutorWithOptionsFunc

var DelegateTaskID = delegateruntime.DelegateTaskID
var DelegateExecutionSessionID = delegateruntime.DelegateExecutionSessionID

type ParallelDelegateTask = delegateruntime.ParallelDelegateTask
type ParallelDelegateResult = delegateruntime.ParallelDelegateResult

// DelegateManager binds the independent delegation engine to tool definitions.
// All task state and execution lifecycle are owned by the embedded engine.
type DelegateManager struct {
	*delegateruntime.DelegateManager
}

func NewDelegateManager(cfg DelegateConfig) *DelegateManager {
	return &DelegateManager{DelegateManager: delegateruntime.NewDelegateManager(cfg, delegateWorkspacePolicy())}
}

func delegateWorkspacePolicy() delegateruntime.WorkspacePolicy {
	return delegateruntime.WorkspacePolicy{Validate: validatePath, Expand: expandSandboxPath}
}

func delegateStringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func delegateOwnerFromExecution(exec ExecutionContext, description, contextStr string) delegateruntime.Owner {
	sessionID := strings.TrimSpace(exec.SessionID)
	if sessionID == "" {
		return delegateruntime.Owner{}
	}
	source := strings.TrimSpace(exec.Source)
	if source == "" {
		source = "unknown"
	}
	payload := strings.Join([]string{sessionID, source, strings.TrimSpace(exec.UserRequest), strings.TrimSpace(description), strings.TrimSpace(contextStr)}, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return delegateruntime.Owner{SessionID: sessionID, Source: source, RequestKey: fmt.Sprintf("%x", digest[:])}
}

// SubagentTool creates, inspects, waits for, and cancels delegated tasks.
func SubagentTool(dm *DelegateManager) *Tool {
	task := DelegateTaskTool(dm)
	status := TaskStatusTool(dm)
	wait := WaitForTasksTool(dm)
	list := ListTasksTool(dm)
	cancel := DelegateCancelTool(dm)
	byAction := map[string]*Tool{
		"delegate": task,
		"status":   status,
		"wait":     wait,
		"list":     list,
		"cancel":   cancel,
	}
	call := func(exec *ExecutionContext, args map[string]any) (ToolCallResult, error) {
		selected := byAction[toolAction(args)]
		if selected == nil {
			return ToolCallResult{}, fmt.Errorf("invalid subagent action %q (use delegate, status, wait, list, or cancel)", args["action"])
		}
		if exec != nil && selected.ContextDetailedHandler != nil {
			return selected.ContextDetailedHandler(*exec, args)
		}
		if selected.DetailedHandler != nil {
			return selected.DetailedHandler(args)
		}
		out, err := selected.Handler(args)
		return ToolCallResult{Output: out}, err
	}
	return &Tool{
		Name:        "subagent",
		Description: "Manage delegated sub-agent tasks. action=delegate creates one and returns task_id; action=status checks one task; action=wait waits for task_ids and returns terminal results; action=list lists tasks; action=cancel cancels one.",
		Category:    CatDelegate,
		Source:      "builtin",
		Permission:  PermAuto,
		PermissionFor: func(args map[string]any) PermissionLevel {
			switch toolAction(args) {
			case "delegate", "cancel":
				return PermApprove
			default:
				return PermAuto
			}
		},
		Parameters: mergedToolParams(map[string]Param{
			"action": {Type: "string", Description: "Action: delegate, status, wait, list, or cancel.", Required: true},
		}, task.Parameters, status.Parameters, wait.Parameters, list.Parameters, cancel.Parameters),
		Handler: func(args map[string]any) (string, error) {
			result, err := call(nil, args)
			return result.Output, err
		},
		ContextDetailedHandler: func(exec ExecutionContext, args map[string]any) (ToolCallResult, error) {
			return call(&exec, args)
		},
	}
}

// DelegateTaskTool 创建子代理委派工具
func DelegateTaskTool(dm *DelegateManager) *Tool {
	return &Tool{
		Name:        "delegate_task",
		Description: "Delegate a task to a sub-agent. The sub-agent will work independently and return results.",
		Category:    CatDelegate,
		Source:      "builtin",
		Permission:  PermApprove, // 委派任务需要审批
		Parameters: map[string]Param{
			"description": {
				Type:        "string",
				Description: "Description of the task to delegate",
				Required:    true,
			},
			"context": {
				Type:        "string",
				Description: "Additional context or instructions for the sub-agent",
				Required:    false,
			},
			"timeout": {
				Type:        "number",
				Description: "Timeout in seconds (default 120, clamped to 5-1800)",
				Required:    false,
				Default:     120,
			},
			"mode": {
				Type:        "string",
				Description: "Execution planning mode: single, auto, parallel, pipeline, debate, or autonomy_queue. Default single.",
				Required:    false,
				Default:     "single",
			},
			"max_children": {
				Type:        "number",
				Description: "Maximum child tasks allowed when planning multi-agent execution. Default 3.",
				Required:    false,
				Default:     3,
			},
			"include_trace": {
				Type:        "boolean",
				Description: "Whether task_status may return the stored planner trace when requested.",
				Required:    false,
				Default:     false,
			},
			"allow_recursive_delegate": {
				Type:        "boolean",
				Description: "Whether delegated child agents may create more delegate tasks. Default false.",
				Required:    false,
				Default:     false,
			},
		},
		Handler: dm.HandleDelegate,
		ContextDetailedHandler: func(exec ExecutionContext, args map[string]any) (ToolCallResult, error) {
			out, err := dm.HandleDelegateWithOwner(args, delegateOwnerFromExecution(exec, delegateStringArg(args, "description"), delegateStringArg(args, "context")))
			return ToolCallResult{Output: out}, err
		},
	}
}

// TaskStatusTool 创建任务状态查询工具
func TaskStatusTool(dm *DelegateManager) *Tool {
	return &Tool{
		Name:        "task_status",
		Description: "Check the status of a delegated task.",
		Category:    CatDelegate,
		Source:      "builtin",
		Permission:  PermAuto, // 查询状态自动批准
		Parameters: map[string]Param{
			"task_id": {
				Type:        "string",
				Description: "ID of the task to check",
				Required:    true,
			},
			"include_result": {
				Type:        "boolean",
				Description: "Whether to include the inline result text",
				Required:    false,
				Default:     true,
			},
			"include_events": {
				Type:        "boolean",
				Description: "Whether to include unified task events and reduced observation",
				Required:    false,
				Default:     false,
			},
			"include_trace": {
				Type:        "boolean",
				Description: "Whether to include the stored planner trace artifact",
				Required:    false,
				Default:     false,
			},
		},
		Handler: dm.HandleStatus,
		ContextDetailedHandler: func(exec ExecutionContext, args map[string]any) (ToolCallResult, error) {
			out, err := dm.HandleStatusForSession(args, strings.TrimSpace(exec.SessionID))
			return ToolCallResult{Output: out}, err
		},
	}
}

// WaitForTasksTool waits until every requested delegated task reaches a
// terminal state. It is read-only and intentionally auto-approved.
func WaitForTasksTool(dm *DelegateManager) *Tool {
	return &Tool{
		Name:        "wait_for_tasks",
		Description: "Wait for delegated tasks to complete, fail, or be cancelled, then return their statuses and results.",
		Category:    CatDelegate,
		Source:      "builtin",
		Permission:  PermAuto,
		Parameters: map[string]Param{
			"task_ids": {
				Type:        "array",
				Description: "IDs of delegated tasks to wait for",
				Required:    true,
			},
			"timeout": {
				Type:        "number",
				Description: "Maximum total wait in seconds (default 120, maximum 1800; fractional seconds allowed)",
				Required:    false,
				Default:     delegateruntime.DefaultTimeoutSeconds,
			},
			"poll_interval_ms": {
				Type:        "number",
				Description: "Status polling interval in milliseconds (default 200, clamped to 10-5000)",
				Required:    false,
				Default:     int(delegateruntime.DefaultWaitPollInterval / time.Millisecond),
			},
			"include_result": {
				Type:        "boolean",
				Description: "Whether completed task result text is included",
				Required:    false,
				Default:     true,
			},
		},
		Handler: func(args map[string]any) (string, error) {
			return dm.HandleWaitForTasks(context.Background(), args)
		},
		ContextDetailedHandler: func(exec ExecutionContext, args map[string]any) (ToolCallResult, error) {
			out, err := dm.HandleWaitForTasksForSession(exec.Context, args, strings.TrimSpace(exec.SessionID))
			return ToolCallResult{Output: out}, err
		},
	}
}

// ListTasksTool 创建任务列表工具
func ListTasksTool(dm *DelegateManager) *Tool {
	return &Tool{
		Name:        "list_tasks",
		Description: "List all delegated tasks and their statuses.",
		Category:    CatDelegate,
		Source:      "builtin",
		Permission:  PermAuto,
		Parameters: map[string]Param{
			"status": {
				Type:        "string",
				Description: "Optional status filter: pending, running, completed, failed, or cancelled",
				Required:    false,
			},
			"limit": {
				Type:        "number",
				Description: "Maximum tasks to return (default 20, max 100)",
				Required:    false,
				Default:     delegateruntime.DefaultListLimit,
			},
			"order": {
				Type:        "string",
				Description: "Sort order by started_at: desc or asc",
				Required:    false,
				Default:     "desc",
			},
			"include_result": {
				Type:        "boolean",
				Description: "Whether to include result summaries for each listed task",
				Required:    false,
				Default:     false,
			},
			"source": {
				Type:        "string",
				Description: "Optional unified task source filter: tool, http, autonomy, or gateway",
				Required:    false,
			},
		},
		Handler: dm.HandleList,
		ContextDetailedHandler: func(exec ExecutionContext, args map[string]any) (ToolCallResult, error) {
			out, err := dm.HandleListForSession(args, strings.TrimSpace(exec.SessionID))
			return ToolCallResult{Output: out}, err
		},
	}
}

// DelegateCancelTool 创建任务取消工具
func DelegateCancelTool(dm *DelegateManager) *Tool {
	return &Tool{
		Name:        "delegate_cancel",
		Description: "Cancel a pending or running delegated task.",
		Category:    CatDelegate,
		Source:      "builtin",
		Permission:  PermApprove,
		Parameters: map[string]Param{
			"task_id": {
				Type:        "string",
				Description: "ID of the task to cancel",
				Required:    true,
			},
			"reason": {
				Type:        "string",
				Description: "Optional cancellation reason",
				Required:    false,
			},
		},
		Handler: dm.HandleCancel,
		ContextDetailedHandler: func(exec ExecutionContext, args map[string]any) (ToolCallResult, error) {
			out, err := dm.HandleCancelForSession(args, strings.TrimSpace(exec.SessionID))
			return ToolCallResult{Output: out}, err
		},
	}
}

// DelegateParallelTool 创建并行委派工具
func (dm *DelegateManager) DelegateParallelTool() *Tool {
	return &Tool{
		Name:        "delegate_parallel",
		Description: "Delegate multiple tasks to sub-agents in parallel. Sub-agents work concurrently and results are aggregated.",
		Category:    CatDelegate,
		Source:      "builtin",
		Permission:  PermApprove,
		Parameters: map[string]Param{
			"tasks": {
				Type:        "array",
				Description: "List of task descriptions to delegate",
				Required:    true,
			},
			"context": {
				Type:        "string",
				Description: "Shared context or instructions for all sub-agents",
				Required:    false,
			},
			"timeout": {
				Type:        "number",
				Description: "Timeout in seconds for each task (default 120)",
				Required:    false,
				Default:     120,
			},
		},
		Handler: dm.HandleDelegateParallel,
	}
}
