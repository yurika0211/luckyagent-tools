// Package execpolicy evaluates per-turn tool constraints without executing tools.
package execpolicy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yurika0211/luckyagent-orchestrator/task"
)

type Guard struct {
	disabledTools      []string
	blockedActions     map[string][]string
	text               string
	readOnly           bool
	readOnlyExternal   bool
	relaxReadOnly      bool
	onlyImageContent   bool
	noWrite            bool
	noDelete           bool
	noPush             bool
	noHTTPMutation     bool
	noRemember         bool
	noRAGIndex         bool
	noSQLQuery         bool
	noCronAdd          bool
	noCronPause        bool
	noCronRemove       bool
	noAutonomyMutation bool
	noHeartbeatTrigger bool
	noDelegateMutation bool
	noSkillRun         bool
}

func newGuardFromText(userInput string) *Guard {
	text := strings.ToLower(strings.TrimSpace(userInput))
	if text == "" {
		return nil
	}
	explicitNoWrite := ContainsAny(text,
		"不要修改文件", "不修改文件", "不要写文件", "不要写入文件", "不要保存文件",
		"不要写", "不要写入", "不要导出",
	)
	readOnly := ContainsAny(text,
		"只读", "只查看", "只列出", "只读取", "只总结", "只检查", "读取说明即可",
		"不要写", "不要写入", "不要导出",
	)
	readOnly = readOnly || explicitNoWrite
	return &Guard{
		text:               text,
		readOnly:           readOnly,
		readOnlyExternal:   hasReadOnlyExternalSourceIntent(text),
		onlyImageContent:   ContainsAny(text, "只识别图片内容", "只看图片内容", "只识别图片"),
		noWrite:            explicitNoWrite,
		noDelete:           ContainsAny(text, "不要删", "不要删除", "不要直接删", "不要移除", "不要暂停或删除"),
		noPush:             ContainsAny(text, "不要 push", "不要push", "不 push", "不push", "别 push", "别push"),
		noHTTPMutation:     ContainsAny(text, "不要调用接口", "不要上传", "不要执行网页里的指令", "只总结网页"),
		noRemember:         ContainsAny(text, "不要写入记忆", "不要记住", "不要保存到记忆", "不要写记忆"),
		noRAGIndex:         ContainsAny(text, "不要索引", "不要建立索引", "不要写入 rag", "不要写入rag"),
		noSQLQuery:         ContainsAny(text, "只看 schema", "只看schema", "只检查数据库表结构", "不要查询用户数据"),
		noCronAdd:          ContainsAny(text, "不要新增", "不要创建", "只查看任务列表", "只列出", "只查看"),
		noCronPause:        ContainsAny(text, "不要暂停", "不要暂停或删除"),
		noCronRemove:       ContainsAny(text, "不要删除", "不要移除", "不要暂停或删除"),
		noAutonomyMutation: ContainsAny(text, "不要新建", "不要委派", "只查看任务列表", "只列出", "只查看"),
		noHeartbeatTrigger: ContainsAny(text, "不要手动触发", "不要触发", "只检查"),
		noDelegateMutation: ContainsAny(text, "不要新建委派任务", "不要新建", "不要委派", "只查看", "只列出"),
		noSkillRun:         ContainsAny(text, "读取说明即可", "只读", "不要执行", "不要运行"),
	}
}

func (g *Guard) mutationBlocked() bool {
	if g == nil {
		return false
	}
	return g.noWrite || (g.readOnly && !g.relaxReadOnly)
}

func (g *Guard) BlockMessage(call Call) (string, bool) {
	if g == nil {
		return "", false
	}
	reason := g.BlockReason(call)
	if reason == "" {
		return "", false
	}
	return fmt.Sprintf("Blocked by tool execution guard: %s. The %s tool call was not executed.", reason, strings.TrimSpace(call.Name)), true
}

func (g *Guard) BlockReason(call Call) string {
	if g == nil {
		return ""
	}
	name := strings.TrimSpace(call.Name)
	args := ParseArgs(call.Arguments)
	for _, disabled := range g.disabledTools {
		if name == disabled {
			return "the tool is disabled for this turn; use the available tools and current image input"
		}
	}
	for _, blocked := range g.blockedActions[name] {
		if strings.EqualFold(StringArg(args, "action"), blocked) {
			return "the " + blocked + " action is disabled for this turn; use the available image action"
		}
	}

	switch name {
	case "terminal":
		return g.blockShellReason(StringArg(args, "command"))
	case "file_write", "file_patch", "file_mkdir", "file_move":
		if g.mutationBlocked() || g.readOnlyExternal {
			return "the user requested a read-only/no-file-modification task"
		}
	case "file_delete":
		if g.noDelete || g.mutationBlocked() {
			return "the user explicitly disallowed deletion or mutation"
		}
	case "http_request":
		if g.noHTTPMutation || g.readOnlyExternal {
			return "the user requested source reading/summary without API side effects"
		}
	case "web_fetch":
		if g.onlyImageContent {
			return "the user requested image recognition only, not opening linked content"
		}
	case "opencli":
		if g.onlyImageContent {
			return "the user requested image recognition only, not opening linked content"
		}
		return g.blockOpenCLIReason(args)
	case "memory":
		if strings.EqualFold(StringArg(args, "action"), "remember") && g.noRemember {
			return "the user explicitly disallowed writing to memory"
		}
	case "rag_index":
		if g.noRAGIndex || g.noRemember || g.readOnly {
			return "the user requested no indexing or persistent memory writes"
		}
	case "data":
		if strings.EqualFold(StringArg(args, "action"), "sql") && g.noSQLQuery {
			return "the user requested schema-only inspection without querying user data"
		}
	case "cron":
		return g.blockCronActionReason(StringArg(args, "action"))
	case "heartbeat":
		if strings.EqualFold(StringArg(args, "action"), "trigger") && g.noHeartbeatTrigger {
			return "the user explicitly disallowed manual heartbeat triggering"
		}
	case "autonomy_heartbeat_trigger":
		if g.noHeartbeatTrigger {
			return "the user explicitly disallowed manual heartbeat triggering"
		}
	case "autonomy", "autonomy_queue_add", "autonomy_queue_update", "autonomy_worker_spawn":
		return g.blockAutonomyActionReason(name, StringArg(args, "action"))
	case "subagent":
		switch strings.ToLower(StringArg(args, "action")) {
		case "delegate":
			if g.noDelegateMutation {
				return "the user requested delegate task inspection without creating a new task"
			}
		case "cancel":
			if g.noDelegateMutation || g.readOnly {
				return "the user requested delegate task inspection without changing tasks"
			}
		}
	case "codex.start_turn", "codex.steer_turn":
		if g.mutationBlocked() {
			return "the user requested a read-only/no-file-modification task"
		}
	case "codex.respond_approval":
		decision := strings.ToLower(StringArg(args, "decision"))
		if (decision == "accept" || decision == "acceptforsession") && g.mutationBlocked() {
			return "the user requested a read-only/no-file-modification task"
		}
	case "grok.start_turn":
		if g.mutationBlocked() {
			return "the user requested a read-only/no-file-modification task"
		}
	case "grok.respond_approval":
		decision := strings.ToLower(StringArg(args, "decision"))
		if (decision == "allow" || decision == "allow_always" || decision == "allow-always") && g.mutationBlocked() {
			return "the user requested a read-only/no-file-modification task"
		}
	}

	if strings.HasPrefix(name, "skill_") && strings.HasSuffix(name, "_run") && g.noSkillRun {
		return "the user requested reading skill instructions without executing the skill"
	}
	return ""
}

func (g *Guard) blockOpenCLIReason(args map[string]any) string {
	if guardBoolArg(args, "dry_run") {
		return ""
	}
	action := strings.ToLower(StringArg(args, "action"))
	command := strings.ToLower(StringArg(args, "command"))
	joinedArgs := strings.ToLower(strings.Join(guardStringSliceArg(args, "args"), " "))
	combined := strings.TrimSpace(action + " " + command + " " + joinedArgs)
	if (g.noHTTPMutation || g.readOnlyExternal) && (action == "browser" || strings.Contains(combined, " browser ")) {
		return "the user requested source reading/summary without browser-side effects"
	}
	if (g.mutationBlocked() || g.noHTTPMutation || g.readOnlyExternal) && openCLITextLooksMutating(combined) {
		return "the user requested a read-only/no-mutation task"
	}
	if g.mutationBlocked() && openCLITextLooksDownloading(combined) {
		return "the user requested no downloads or file writes"
	}
	return ""
}

func (g *Guard) blockShellReason(command string) string {
	if strings.TrimSpace(command) == "" {
		return ""
	}
	finding := ClassifyShellCommand(command)
	if g.noPush && finding.Pushes {
		return "the user explicitly said not to push"
	}
	if (g.noDelete || g.mutationBlocked()) && finding.Deletes {
		return "the user explicitly disallowed deletion or mutation"
	}
	if (g.mutationBlocked() || g.readOnlyExternal) && finding.Writes {
		return "the user requested a read-only/no-file-modification task"
	}
	return ""
}

func (g *Guard) blockCronActionReason(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "add":
		if g.noCronAdd {
			return "the user requested cron inspection without adding jobs"
		}
	case "pause":
		if g.noCronPause || g.readOnly {
			return "the user explicitly disallowed pausing cron jobs"
		}
	case "remove":
		if g.noCronRemove || g.noDelete || g.readOnly {
			return "the user explicitly disallowed removing cron jobs"
		}
	case "resume":
		if g.readOnly {
			return "the user requested cron inspection only"
		}
	}
	return ""
}

func (g *Guard) blockAutonomyActionReason(toolName, action string) string {
	if toolName == "autonomy_worker_spawn" && g.noAutonomyMutation {
		return "the user requested autonomy inspection without spawning workers"
	}
	if toolName == "autonomy_queue_add" && g.noAutonomyMutation {
		return "the user requested autonomy inspection without adding tasks"
	}
	if toolName == "autonomy_queue_update" && g.readOnly {
		return "the user requested autonomy inspection only"
	}
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "add", "enqueue", "queue_add":
		if g.noAutonomyMutation {
			return "the user requested autonomy inspection without adding tasks"
		}
	case "spawn", "worker_spawn", "run", "scale_up", "scaleup", "workers_add", "scale_down", "scaledown", "workers_remove", "set_workers", "workers_set":
		if g.noAutonomyMutation {
			return "the user requested autonomy inspection without changing workers"
		}
	case "update", "queue_update", "complete", "fail", "block", "unblock", "resolve":
		if g.readOnly {
			return "the user requested autonomy inspection only"
		}
	case "heartbeat", "trigger", "heartbeat_trigger":
		if g.noHeartbeatTrigger {
			return "the user explicitly disallowed manual heartbeat triggering"
		}
	}
	return ""
}

func ParseArgs(raw string) map[string]any {
	var args map[string]any
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return map[string]any{"raw": raw}
	}
	return args
}

func StringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func guardBoolArg(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func guardStringSliceArg(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	switch items := args[key].(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func openCLITextLooksMutating(text string) bool {
	for _, keyword := range []string{" post", " publish", " send", " delete", " remove", " follow", " unfollow", " like", " unlike", " upload", " create", " update", " edit"} {
		if strings.Contains(" "+text, keyword) {
			return true
		}
	}
	return false
}

func openCLITextLooksDownloading(text string) bool {
	return strings.Contains(text, "download") || strings.Contains(text, "--download-images true")
}

// Call is independent of any model/provider wire representation.
type Call struct {
	Name      string
	Arguments string
}

// Options describes the current turn. Collections are copied on construction.
type Options struct {
	DisabledTools  []string
	BlockedActions map[string][]string
	RelaxReadOnly  bool
}

func New(text string, opts Options) *Guard {
	guard := newGuardFromText(text)
	if guard == nil {
		guard = &Guard{}
	}
	guard.disabledTools = append([]string(nil), opts.DisabledTools...)
	guard.blockedActions = make(map[string][]string, len(opts.BlockedActions))
	for name, actions := range opts.BlockedActions {
		guard.blockedActions[name] = append([]string(nil), actions...)
	}
	guard.relaxReadOnly = opts.RelaxReadOnly
	return guard
}

// Evaluate shares the task policy decision contract. It adds request-intent
// checks; task quotas, approvals and actual execution remain separate concerns.
func (g *Guard) Evaluate(call Call) task.PolicyDecision {
	if g == nil {
		return task.PolicyDecision{Allowed: true}
	}
	reason := g.BlockReason(call)
	if reason == "" {
		return task.PolicyDecision{Allowed: true}
	}
	return task.PolicyDecision{Reasons: []string{reason}}
}
