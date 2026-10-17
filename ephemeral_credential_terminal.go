package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yurika0211/luckyagent-tools/credentials"
	"github.com/yurika0211/luckyagent-tools/sandbox"
)

const (
	ephemeralCredentialTerminalFactoryName    = "create_ephemeral_credential_terminal_tool"
	ephemeralCredentialTerminalPrefix         = "tmp_terminal_credential_"
	ephemeralCredentialTerminalMaxCommand     = 32 << 10
	ephemeralCredentialTerminalDefaultTTL     = 10 * time.Minute
	ephemeralCredentialTerminalMaxTTL         = time.Hour
	ephemeralCredentialTerminalDefaultTimeout = 30 * time.Second
	ephemeralCredentialTerminalMaxTimeout     = 5 * time.Minute
)

var ephemeralCredentialEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// EphemeralCredentialTerminalToolService owns session-scoped terminal
// wrappers. The wrapper stores only credential_ref and a declarative command
// template; the credential value exists only while the child process runs.
type EphemeralCredentialTerminalToolService struct {
	mu       sync.Mutex
	resolver CredentialProvider
	registry *Registry
	tools    map[string]map[string]*ephemeralCredentialTerminalTool
}

type ephemeralCredentialTerminalTool struct {
	scope         string
	name          string
	displayName   string
	description   string
	credentialRef string
	command       string
	envName       string
	parameters    map[string]Param
	expiresAt     time.Time
	timeout       time.Duration
}

func NewEphemeralCredentialTerminalToolService(resolver CredentialProvider) *EphemeralCredentialTerminalToolService {
	return &EphemeralCredentialTerminalToolService{
		resolver: resolver,
		tools:    make(map[string]map[string]*ephemeralCredentialTerminalTool),
	}
}

// RegisterTools exposes only the factory. Generated wrappers are hidden from
// the global model list and are appended for their owning session by Agent.
func (s *EphemeralCredentialTerminalToolService) RegisterTools(r *Registry) {
	if s == nil || r == nil {
		return
	}
	s.mu.Lock()
	s.registry = r
	s.mu.Unlock()
	r.Register(s.factoryTool())
}

func (s *EphemeralCredentialTerminalToolService) factoryTool() *Tool {
	return &Tool{
		Name:        ephemeralCredentialTerminalFactoryName,
		Description: "Create a short-lived session-scoped terminal tool backed by a stored credential. The secret is injected only into one controlled child process environment and is never put in command arguments, shell session state, tool output, or logs.",
		Category:    CatBuiltin,
		Source:      "builtin",
		Permission:  PermApprove,
		Parameters: map[string]Param{
			"credential_ref":   {Type: "string", Description: "Stored credential reference. Never supply the secret itself.", Required: true},
			"name":             {Type: "string", Description: "Human-readable operation name. The runtime generates the temporary tool name.", Required: true},
			"description":      {Type: "string", Description: "Short description shown when the temporary tool is available.", Required: true},
			"command_template": {Type: "string", Description: "Fixed shell command template. Use {parameter} placeholders for business inputs; use the generated environment variable for the credential.", Required: true},
			"env_name":         {Type: "string", Description: "Environment variable name exposed to the child process, for example API_TOKEN. Defaults to LUCKY_CREDENTIAL.", Required: false, Default: "LUCKY_CREDENTIAL"},
			"input_schema":     {Type: "string", Description: "Optional JSON Schema object describing command input placeholders.", Required: false, Default: `{"type":"object","properties":{},"required":[]}`},
			"ttl_seconds":      {Type: "number", Description: "Lifetime in seconds (default 600, maximum 3600).", Required: false, Default: 600},
			"timeout_seconds":  {Type: "number", Description: "Per-call child process timeout (default 30, maximum 300).", Required: false, Default: 30},
		},
		ContextDetailedHandler: s.create,
	}
}

func (s *EphemeralCredentialTerminalToolService) VisibleTools(scope string) []map[string]any {
	scope = strings.TrimSpace(scope)
	if s == nil || scope == "" {
		return nil
	}
	for _, name := range s.expire(scope, time.Now()) {
		s.unregister(name)
	}
	s.mu.Lock()
	items := make([]*ephemeralCredentialTerminalTool, 0, len(s.tools[scope]))
	for _, item := range s.tools[scope] {
		items = append(items, item)
	}
	s.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, item.asTool().ToOpenAIFormat())
	}
	return result
}

func (s *EphemeralCredentialTerminalToolService) RevokeSession(scope string) {
	scope = strings.TrimSpace(scope)
	if s == nil || scope == "" {
		return
	}
	s.mu.Lock()
	items := s.tools[scope]
	delete(s.tools, scope)
	s.mu.Unlock()
	for name := range items {
		s.unregister(name)
	}
}

func (s *EphemeralCredentialTerminalToolService) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	var names []string
	for scope, items := range s.tools {
		for name := range items {
			names = append(names, name)
		}
		delete(s.tools, scope)
	}
	s.mu.Unlock()
	for _, name := range names {
		s.unregister(name)
	}
	return nil
}

func (s *EphemeralCredentialTerminalToolService) create(execCtx ExecutionContext, args map[string]any) (ToolCallResult, error) {
	scope := strings.TrimSpace(execCtx.SessionID)
	if scope == "" {
		return ToolCallResult{}, fmt.Errorf("temporary credential terminal tools require a session")
	}
	if strings.EqualFold(strings.TrimSpace(execCtx.Source), "autonomy") {
		return ToolCallResult{}, fmt.Errorf("temporary credential terminal tools cannot be created in background autonomy tasks")
	}
	spec, err := parseEphemeralCredentialTerminalSpec(args)
	if err != nil {
		return ToolCallResult{}, err
	}
	s.mu.Lock()
	if s.registry == nil {
		s.mu.Unlock()
		return ToolCallResult{}, fmt.Errorf("temporary credential terminal registry is unavailable")
	}
	name, err := randomEphemeralCredentialTerminalName()
	if err != nil {
		s.mu.Unlock()
		return ToolCallResult{}, err
	}
	item := &ephemeralCredentialTerminalTool{
		scope: scope, name: name, displayName: spec.displayName, description: spec.description,
		credentialRef: spec.credentialRef, command: spec.command, envName: spec.envName,
		parameters: spec.parameters, expiresAt: time.Now().Add(spec.ttl), timeout: spec.timeout,
	}
	s.tools[scope] = appendEphemeralTerminalTool(s.tools[scope], item)
	registry := s.registry
	s.mu.Unlock()
	registry.Register(item.asToolBound(s))
	return ToolCallResult{Output: fmt.Sprintf("Temporary credential terminal tool created. tool_name=%s name=%s credential_ref=%s env_name=%s expires_at=%s", item.name, item.displayName, item.credentialRef, item.envName, item.expiresAt.UTC().Format(time.RFC3339))}, nil
}

type parsedEphemeralCredentialTerminalSpec struct {
	displayName   string
	description   string
	credentialRef string
	command       string
	envName       string
	parameters    map[string]Param
	ttl           time.Duration
	timeout       time.Duration
}

func parseEphemeralCredentialTerminalSpec(args map[string]any) (parsedEphemeralCredentialTerminalSpec, error) {
	var spec parsedEphemeralCredentialTerminalSpec
	spec.displayName = strings.TrimSpace(credentialStringArg(args, "name"))
	spec.description = strings.TrimSpace(credentialStringArg(args, "description"))
	if spec.displayName == "" || len(spec.displayName) > 120 {
		return spec, fmt.Errorf("name is required and must be at most 120 characters")
	}
	if spec.description == "" || len(spec.description) > 500 {
		return spec, fmt.Errorf("description is required and must be at most 500 characters")
	}
	var err error
	spec.credentialRef, err = credentials.ValidateID(credentialStringArg(args, "credential_ref"))
	if err != nil {
		return spec, err
	}
	spec.command = strings.TrimSpace(credentialStringArg(args, "command_template"))
	if err := validateEphemeralTerminalCommand(spec.command); err != nil {
		return spec, err
	}
	spec.envName = strings.TrimSpace(credentialStringArg(args, "env_name"))
	if spec.envName == "" {
		spec.envName = "LUCKY_CREDENTIAL"
	}
	if err := validateEphemeralCredentialEnvName(spec.envName); err != nil {
		return spec, err
	}
	spec.parameters, err = parseEphemeralInputSchema(credentialStringArg(args, "input_schema"))
	if err != nil {
		return spec, err
	}
	if err := validateEphemeralTerminalTemplateParameters(spec.command, spec.parameters); err != nil {
		return spec, err
	}
	ttlSeconds := credentialIntArg(args, "ttl_seconds", int(ephemeralCredentialTerminalDefaultTTL/time.Second))
	if ttlSeconds <= 0 || time.Duration(ttlSeconds)*time.Second > ephemeralCredentialTerminalMaxTTL {
		return spec, fmt.Errorf("ttl_seconds must be between 1 and %d", int(ephemeralCredentialTerminalMaxTTL/time.Second))
	}
	spec.ttl = time.Duration(ttlSeconds) * time.Second
	timeoutSeconds := credentialIntArg(args, "timeout_seconds", int(ephemeralCredentialTerminalDefaultTimeout/time.Second))
	if timeoutSeconds <= 0 || time.Duration(timeoutSeconds)*time.Second > ephemeralCredentialTerminalMaxTimeout {
		return spec, fmt.Errorf("timeout_seconds must be between 1 and %d", int(ephemeralCredentialTerminalMaxTimeout/time.Second))
	}
	spec.timeout = time.Duration(timeoutSeconds) * time.Second
	return spec, nil
}

func validateEphemeralTerminalCommand(command string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("command_template is required")
	}
	if len(command) > ephemeralCredentialTerminalMaxCommand {
		return fmt.Errorf("command_template exceeds %d bytes", ephemeralCredentialTerminalMaxCommand)
	}
	if strings.IndexByte(command, 0) >= 0 {
		return fmt.Errorf("command_template cannot contain NUL bytes")
	}
	return nil
}

func validateEphemeralCredentialEnvName(name string) error {
	if !ephemeralCredentialEnvName.MatchString(name) {
		return fmt.Errorf("env_name must be a valid environment variable name")
	}
	upper := strings.ToUpper(name)
	for _, blocked := range []string{"PATH", "HOME", "PWD", "OLDPWD", "TMPDIR", "IFS", "ENV", "BASH_ENV", "SHELLOPTS", "CDPATH", "LD_PRELOAD", "LD_LIBRARY_PATH"} {
		if upper == blocked {
			return fmt.Errorf("env_name %q cannot replace a shell control variable", name)
		}
	}
	if strings.HasPrefix(upper, "LD_") {
		return fmt.Errorf("env_name %q cannot use the LD_ namespace", name)
	}
	return nil
}

func (s *EphemeralCredentialTerminalToolService) execute(execCtx ExecutionContext, scope, name string, args map[string]any) (ToolCallResult, error) {
	if strings.EqualFold(strings.TrimSpace(execCtx.Source), "autonomy") {
		return ToolCallResult{}, fmt.Errorf("temporary credential terminal tools cannot run in background autonomy tasks")
	}
	if err := rejectEphemeralSecretArguments(args); err != nil {
		return ToolCallResult{}, err
	}
	item, err := s.lookup(scope, name)
	if err != nil {
		return ToolCallResult{}, err
	}
	command, err := renderEphemeralTerminalCommand(item.command, args, item.parameters)
	if err != nil {
		return ToolCallResult{}, err
	}
	requestCtx := execCtx.Context
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	commandCtx, cancel := context.WithTimeout(requestCtx, item.timeout)
	defer cancel()
	workdir, _ := args["_cwd"].(string)
	var output string
	resolver := credentialResolver{provider: s.resolver}
	err = resolver.withCredential(commandCtx, item.credentialRef, func(secret []byte) error {
		cmd, buildErr := buildEphemeralCredentialCommand(commandCtx, execCtx.Sandbox, command, workdir, item.envName, secret)
		if buildErr != nil {
			return buildErr
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		output = redactEphemeralTerminalOutput(stdout.String(), stderr.String(), secret)
		if runErr != nil {
			if commandCtx.Err() != nil {
				return fmt.Errorf("temporary credential terminal command timed out after %d seconds", int(item.timeout/time.Second))
			}
			output += fmt.Sprintf("\n[exit code: %v]", runErr)
		}
		return nil
	})
	if err != nil {
		return ToolCallResult{}, err
	}
	return ToolCallResult{Output: output}, nil
}

func buildEphemeralCredentialCommand(ctx context.Context, snapshot *sandbox.Snapshot, command, workdir, envName string, secret []byte) (*exec.Cmd, error) {
	if snapshot != nil {
		return snapshot.CommandWithEphemeralEnv(ctx, command, workdir, map[string]string{envName: string(secret)})
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if workdir != "" {
		cmd.Dir = workdir
	}
	// Do not put the secret in the command string or argv. It exists only in
	// the child environment and disappears when the child exits.
	cmd.Env = append(os.Environ(), envName+"="+string(secret))
	return cmd, nil
}

func redactEphemeralTerminalOutput(stdout, stderr string, secret []byte) string {
	redact := func(value string) string {
		if len(secret) == 0 {
			return value
		}
		return strings.ReplaceAll(value, string(secret), "[REDACTED_CREDENTIAL]")
	}
	output := redact(stdout)
	if stderr != "" {
		output += "\n[stderr]\n" + redact(stderr)
	}
	if len(output) > 10000 {
		output = output[:10000] + "\n... (truncated)"
	}
	return output
}

func renderEphemeralTerminalCommand(template string, args map[string]any, parameters map[string]Param) (string, error) {
	matches := ephemeralTemplateToken.FindAllStringIndex(template, -1)
	if len(matches) == 0 {
		return template, nil
	}
	var b strings.Builder
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		b.WriteString(template[last:start])
		token := template[start:end]
		// ${TOKEN} is the shell's environment-variable syntax. Only bare
		// {parameter} tokens belong to the temporary tool input schema.
		if start > 0 && template[start-1] == '$' {
			b.WriteString(token)
			last = end
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(token, "{"), "}")
		value, ok := args[name]
		if !ok || value == nil {
			return "", fmt.Errorf("missing temporary tool parameter %q", name)
		}
		if _, ok := parameters[name]; !ok {
			return "", fmt.Errorf("unknown temporary tool parameter %q", name)
		}
		b.WriteString(shellQuoteEphemeralTerminalValue(fmt.Sprint(value)))
		last = end
	}
	b.WriteString(template[last:])
	return b.String(), nil
}

func validateEphemeralTerminalTemplateParameters(template string, parameters map[string]Param) error {
	for _, match := range ephemeralTemplateToken.FindAllStringIndex(template, -1) {
		start, end := match[0], match[1]
		if start > 0 && template[start-1] == '$' {
			continue
		}
		name := template[start+1 : end-1]
		if _, ok := parameters[name]; !ok {
			return fmt.Errorf("template parameter %q is missing from input_schema", name)
		}
	}
	return nil
}

func shellQuoteEphemeralTerminalValue(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (s *EphemeralCredentialTerminalToolService) lookup(scope, name string) (*ephemeralCredentialTerminalTool, error) {
	if strings.TrimSpace(scope) == "" {
		return nil, fmt.Errorf("temporary credential terminal tool requires a session")
	}
	for _, expired := range s.expire(scope, time.Now()) {
		s.unregister(expired)
	}
	s.mu.Lock()
	item := s.tools[scope][name]
	s.mu.Unlock()
	if item == nil {
		return nil, fmt.Errorf("temporary credential terminal tool %q is not available in this session", name)
	}
	return item, nil
}

func (s *EphemeralCredentialTerminalToolService) expire(scope string, now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.tools[scope]
	if len(items) == 0 {
		return nil
	}
	var expired []string
	for name, item := range items {
		if !now.Before(item.expiresAt) {
			delete(items, name)
			expired = append(expired, name)
		}
	}
	if len(items) == 0 {
		delete(s.tools, scope)
	}
	return expired
}

func (s *EphemeralCredentialTerminalToolService) unregister(name string) {
	if s == nil || s.registry == nil || strings.TrimSpace(name) == "" {
		return
	}
	s.registry.Unregister(name)
}

func appendEphemeralTerminalTool(items map[string]*ephemeralCredentialTerminalTool, item *ephemeralCredentialTerminalTool) map[string]*ephemeralCredentialTerminalTool {
	if items == nil {
		items = make(map[string]*ephemeralCredentialTerminalTool)
	}
	items[item.name] = item
	return items
}

func randomEphemeralCredentialTerminalName() (string, error) {
	name, err := randomEphemeralToolName()
	if err != nil {
		return "", err
	}
	return strings.Replace(name, ephemeralCredentialToolPrefix, ephemeralCredentialTerminalPrefix, 1), nil
}

func (e *ephemeralCredentialTerminalTool) asTool() *Tool {
	return &Tool{
		Name: e.name, Description: e.description, Parameters: e.parameters,
		Category: CatBuiltin, Source: "ephemeral_credential_terminal", Permission: PermApprove,
		ShellAware: true, ParallelSafe: false, HiddenFromModel: true,
		ContextDetailedHandler: func(exec ExecutionContext, args map[string]any) (ToolCallResult, error) {
			return ToolCallResult{}, fmt.Errorf("temporary credential terminal wrapper is detached")
		},
	}
}

func (e *ephemeralCredentialTerminalTool) asToolBound(service *EphemeralCredentialTerminalToolService) *Tool {
	t := e.asTool()
	t.ContextDetailedHandler = func(execCtx ExecutionContext, args map[string]any) (ToolCallResult, error) {
		if strings.TrimSpace(execCtx.SessionID) != e.scope {
			return ToolCallResult{}, fmt.Errorf("temporary credential terminal tool %q is not available in this session", e.name)
		}
		return service.execute(execCtx, e.scope, e.name, args)
	}
	return t
}
