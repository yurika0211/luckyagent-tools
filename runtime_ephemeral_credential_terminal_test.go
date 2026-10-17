package tool

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestEphemeralCredentialTerminalInjectsAndRedactsSecret(t *testing.T) {
	provider := testCredentialProvider{}

	service := NewEphemeralCredentialTerminalToolService(provider)
	registry := NewRegistry()
	service.RegisterTools(registry)
	created, err := service.create(ExecutionContext{Context: context.Background(), SessionID: "session-a"}, map[string]any{
		"credential_ref":   "github-main",
		"name":             "github-cli",
		"description":      "Run the authenticated GitHub CLI",
		"command_template": `printf '%s\n' "$TOKEN"`,
		"env_name":         "TOKEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(created.Output, "super-secret") {
		t.Fatalf("creation output leaked secret: %s", created.Output)
	}
	name := parseTerminalTemporaryToolName(t, created.Output)
	generated, ok := registry.Get(name)
	if !ok || generated == nil {
		t.Fatalf("generated tool %q was not registered", name)
	}

	result, err := generated.ContextDetailedHandler(ExecutionContext{Context: context.Background(), SessionID: "session-a"}, map[string]any{
		"_cwd": "/tmp",
		"_env": map[string]string{"TOKEN": "must-not-be-used"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Output, "super-secret") {
		t.Fatalf("terminal result leaked secret: %s", result.Output)
	}
	if !strings.Contains(result.Output, "[REDACTED_CREDENTIAL]") {
		t.Fatalf("terminal result was not redacted: %s", result.Output)
	}

	if _, err := generated.ContextDetailedHandler(ExecutionContext{Context: context.Background(), SessionID: "session-a"}, map[string]any{
		"token": "should-not-be-passed",
	}); err == nil {
		t.Fatal("temporary terminal tool accepted a secret-shaped argument")
	}
	if _, err := generated.ContextDetailedHandler(ExecutionContext{Context: context.Background(), SessionID: "session-b"}, nil); err == nil {
		t.Fatal("temporary terminal tool was callable from another session")
	}
}

func TestEphemeralCredentialTerminalRendersShellQuotedParameters(t *testing.T) {
	spec, err := parseEphemeralCredentialTerminalSpec(map[string]any{
		"credential_ref":   "ref-1",
		"name":             "lookup",
		"description":      "Lookup one item",
		"command_template": `printf '%s' {value}`,
		"input_schema":     `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderEphemeralTerminalCommand(spec.command, map[string]any{"value": "a; echo leaked"}, spec.parameters)
	if err != nil {
		t.Fatal(err)
	}
	if rendered != "printf '%s' 'a; echo leaked'" {
		t.Fatalf("parameter was not shell quoted: %q", rendered)
	}
	if rendered, err := renderEphemeralTerminalCommand(`printf '%s' "${TOKEN}"`, nil, nil); err != nil || rendered != `printf '%s' "${TOKEN}"` {
		t.Fatalf("environment variable syntax was treated as a parameter: %q, %v", rendered, err)
	}
}

func TestEphemeralCredentialTerminalExpiresAndRevokes(t *testing.T) {
	service := NewEphemeralCredentialTerminalToolService(testCredentialProvider{})
	registry := NewRegistry()
	service.RegisterTools(registry)
	created, err := service.create(ExecutionContext{Context: context.Background(), SessionID: "session-a"}, map[string]any{
		"credential_ref":   "ref-1",
		"name":             "temporary",
		"description":      "Temporary command",
		"command_template": "true",
		"ttl_seconds":      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	name := parseTerminalTemporaryToolName(t, created.Output)
	service.mu.Lock()
	service.tools["session-a"][name].expiresAt = time.Now().Add(-time.Second)
	service.mu.Unlock()
	if got := service.VisibleTools("session-a"); len(got) != 0 {
		t.Fatalf("expired temporary terminal tools still visible: %d", len(got))
	}
	if _, ok := registry.Get(name); ok {
		t.Fatalf("expired temporary terminal tool %q still registered", name)
	}
}

func TestEphemeralCredentialTerminalDoesNotPutSecretInArgv(t *testing.T) {
	cmd, err := buildEphemeralCredentialCommand(context.Background(), nil, `printf '%s' "$TOKEN"`, t.TempDir(), "TOKEN", []byte("super-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(cmd.Args, "\x00"), "super-secret") {
		t.Fatalf("secret appeared in child argv: %q", cmd.Args)
	}
	found := false
	for _, entry := range cmd.Env {
		if entry == "TOKEN=super-secret" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("secret was not present in child environment: %v", cmd.Env)
	}
}

func parseTerminalTemporaryToolName(t *testing.T, output string) string {
	t.Helper()
	for _, field := range strings.Fields(output) {
		if strings.HasPrefix(field, "tool_name=") {
			return strings.TrimPrefix(field, "tool_name=")
		}
	}
	t.Fatalf("tool name missing from output: %s", output)
	return ""
}
