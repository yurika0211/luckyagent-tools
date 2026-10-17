package tool

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// HeartbeatToolService wraps heartbeat-related tool handlers.
type HeartbeatToolService struct {
	trigger func(args map[string]any) (string, error)
	status  func(args map[string]any) (string, error)
}

// NewHeartbeatToolService creates a heartbeat tool service from injected handlers.
func NewHeartbeatToolService(
	trigger func(args map[string]any) (string, error),
	status func(args map[string]any) (string, error),
) *HeartbeatToolService {
	return &HeartbeatToolService{
		trigger: trigger,
		status:  status,
	}
}

// RegisterTools registers heartbeat-related tools onto the registry.
func (s *HeartbeatToolService) RegisterTools(r *Registry) {
	if s == nil || r == nil {
		return
	}

	r.Register(&Tool{
		Name:        "heartbeat",
		Description: "Inspect or run HEARTBEAT.md. action=status returns runtime status; action=trigger previews evaluation and only executes when dry_run=false and execute=true.",
		Category:    CatDelegate,
		Source:      "builtin",
		Permission:  PermAuto,
		Parameters: map[string]Param{
			"action":           {Type: "string", Description: "Action: status or trigger.", Required: true},
			"dry_run":          {Type: "boolean", Description: "For action=trigger, preview without executing active periodic tasks", Required: false, Default: true},
			"execute":          {Type: "boolean", Description: "For action=trigger, execute due tasks. Requires dry_run=false", Required: false, Default: false},
			"max_tasks":        {Type: "number", Description: "For action=trigger, maximum tasks the handler should execute", Required: false, Default: 10},
			"timeout_seconds":  {Type: "number", Description: "For action=trigger, timeout hint for the handler", Required: false, Default: 60},
			"include_inactive": {Type: "boolean", Description: "Include inactive tasks when supported", Required: false, Default: false},
			"task_id":          {Type: "string", Description: "Optional task id filter for action=trigger", Required: false},
			"format":           {Type: "string", Description: "Output format; json is the stable default", Required: false, Default: "json"},
			"include_tasks":    {Type: "boolean", Description: "For action=status, include task details when supported", Required: false, Default: false},
			"include_routes":   {Type: "boolean", Description: "For action=status, include external chat route details when supported", Required: false, Default: true},
			"include_errors":   {Type: "boolean", Description: "For action=status, include recent error details when supported", Required: false, Default: true},
			"limit":            {Type: "number", Description: "For action=status, maximum task details to return when supported", Required: false, Default: 20},
		},
		Handler: s.handleHeartbeat,
	})
}

func (s *HeartbeatToolService) handleHeartbeat(args map[string]any) (string, error) {
	action, _ := args["action"].(string)
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "status":
		return s.handleStatus(args)
	case "trigger":
		return s.handleTrigger(args)
	default:
		return "", fmt.Errorf("invalid heartbeat action %q (use status or trigger)", action)
	}
}

func (s *HeartbeatToolService) handleTrigger(args map[string]any) (string, error) {
	if s == nil || s.trigger == nil {
		return "", fmt.Errorf("heartbeat trigger handler not configured")
	}
	startedAt := time.Now()
	dryRun := heartbeatBoolArg(args, "dry_run", true)
	execute := heartbeatBoolArg(args, "execute", false)
	maxTasks := parseHeartbeatPositiveInt(args, "max_tasks", 10, 100)
	timeoutSeconds := parseHeartbeatPositiveInt(args, "timeout_seconds", 60, 3600)
	if dryRun || !execute {
		finishedAt := time.Now()
		out, _ := json.Marshal(map[string]any{
			"ok":                 true,
			"action":             "trigger",
			"dry_run":            true,
			"executed":           false,
			"handler_configured": true,
			"started_at":         startedAt,
			"finished_at":        finishedAt,
			"due_tasks":          0,
			"executed_tasks":     0,
			"skipped_tasks":      0,
			"max_tasks":          maxTasks,
			"timeout_seconds":    timeoutSeconds,
			"warnings":           []string{"dry_run=true or execute=false; heartbeat handler was not called"},
		})
		return string(out), nil
	}
	raw, err := s.trigger(args)
	finishedAt := time.Now()
	if err != nil {
		out, _ := json.Marshal(map[string]any{
			"ok":                 false,
			"action":             "trigger",
			"dry_run":            false,
			"executed":           false,
			"handler_configured": true,
			"started_at":         startedAt,
			"finished_at":        finishedAt,
			"error":              err.Error(),
			"retryable":          true,
		})
		return string(out), err
	}
	return marshalHeartbeatEnvelope(map[string]any{
		"ok":                 true,
		"action":             "trigger",
		"dry_run":            false,
		"executed":           true,
		"handler_configured": true,
		"started_at":         startedAt,
		"finished_at":        finishedAt,
		"raw_output":         raw,
	}, raw)
}

func (s *HeartbeatToolService) handleStatus(args map[string]any) (string, error) {
	if s == nil || s.status == nil {
		return "", fmt.Errorf("heartbeat status handler not configured")
	}
	raw, err := s.status(args)
	if err != nil {
		out, _ := json.Marshal(map[string]any{
			"ok":                 false,
			"action":             "status",
			"handler_configured": true,
			"error":              err.Error(),
			"retryable":          true,
		})
		return string(out), err
	}
	return marshalHeartbeatEnvelope(map[string]any{
		"ok":                 true,
		"action":             "status",
		"handler_configured": true,
		"raw_output":         raw,
	}, raw)
}

func marshalHeartbeatEnvelope(base map[string]any, raw string) (string, error) {
	if base == nil {
		base = map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err == nil {
		base["payload"] = payload
		for k, v := range payload {
			if _, exists := base[k]; !exists {
				base[k] = v
			}
		}
	}
	out, err := json.Marshal(base)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func heartbeatBoolArg(args map[string]any, key string, fallback bool) bool {
	if args == nil {
		return fallback
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y", "on":
			return true
		case "false", "0", "no", "n", "off":
			return false
		default:
			return fallback
		}
	default:
		return fallback
	}
}

func parseHeartbeatPositiveInt(args map[string]any, key string, fallback, max int) int {
	n, err := parsePositiveCountArg(args, key, fallback)
	if err != nil {
		return fallback
	}
	if n > max {
		return max
	}
	return n
}
