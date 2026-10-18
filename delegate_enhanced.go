package tool

import (
	delegateruntime "github.com/yurika0211/luckyagent-orchestrator/delegate"
)

// Compatibility names for the independent delegation engine.
type TaskPriority = delegateruntime.TaskPriority

const PriorityLow = delegateruntime.PriorityLow
const PriorityNormal = delegateruntime.PriorityNormal
const PriorityHigh = delegateruntime.PriorityHigh
const PriorityCritical = delegateruntime.PriorityCritical

var ParseTaskPriority = delegateruntime.ParseTaskPriority

type DelegateTarget = delegateruntime.DelegateTarget

const TargetAgent = delegateruntime.TargetAgent
const TargetSkill = delegateruntime.TargetSkill
const TargetMCP = delegateruntime.TargetMCP

type PrioritizedTask = delegateruntime.PrioritizedTask
type TaskCache = delegateruntime.TaskCache

var NewTaskCache = delegateruntime.NewTaskCache

type PriorityTaskQueue = delegateruntime.PriorityTaskQueue

var NewPriorityTaskQueue = delegateruntime.NewPriorityTaskQueue

// DelegateToSkillTool creates a tool for delegating tasks to skills.
func DelegateToSkillTool(dm *DelegateManager) *Tool {
	return &Tool{
		Name:        "delegate_to_skill",
		Description: "Delegate a task to a specific skill plugin. The skill's tools will be used to complete the task.",
		Category:    CatDelegate,
		Source:      "builtin",
		Permission:  PermApprove,
		Parameters: map[string]Param{
			"skill_name": {
				Type:        "string",
				Description: "Name of the skill to delegate to",
				Required:    true,
			},
			"description": {
				Type:        "string",
				Description: "Description of the task to delegate",
				Required:    true,
			},
			"priority": {
				Type:        "string",
				Description: "Task priority: low, normal, high, critical",
				Required:    false,
				Default:     "normal",
			},
		},
		Handler: dm.HandleDelegateToSkill,
	}
}

// DelegateToMCPTool creates a tool for delegating tasks to MCP servers.
func DelegateToMCPTool(dm *DelegateManager) *Tool {
	return &Tool{
		Name:        "delegate_to_mcp",
		Description: "Delegate a task to an MCP server by calling a specific tool.",
		Category:    CatDelegate,
		Source:      "builtin",
		Permission:  PermApprove,
		Parameters: map[string]Param{
			"server_name": {
				Type:        "string",
				Description: "Name of the MCP server",
				Required:    true,
			},
			"tool_name": {
				Type:        "string",
				Description: "Name of the tool on the MCP server to call",
				Required:    true,
			},
			"arguments": {
				Type:        "object",
				Description: "Arguments to pass to the MCP tool",
				Required:    false,
			},
			"priority": {
				Type:        "string",
				Description: "Task priority: low, normal, high, critical",
				Required:    false,
				Default:     "normal",
			},
		},
		Handler: dm.HandleDelegateToMCP,
	}
}
