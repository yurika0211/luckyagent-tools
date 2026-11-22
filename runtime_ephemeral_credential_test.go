package tool

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEphemeralCredentialToolInjectsAndRedactsSecret(t *testing.T) {
	provider := testCredentialProvider{}

	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("echoed super-secret"))
	}))
	defer server.Close()

	oldValidator := ephemeralCredentialURLValidator
	ephemeralCredentialURLValidator = func(string) error { return nil }
	defer func() { ephemeralCredentialURLValidator = oldValidator }()

	service := NewEphemeralCredentialToolService(provider)
	registry := NewRegistry()
	service.RegisterTools(registry)
	created, err := service.create(ExecutionContext{Context: context.Background(), SessionID: "session-a"}, map[string]any{
		"credential_ref": "github-main",
		"name":           "get-user",
		"description":    "Read the current user",
		"url_template":   server.URL + "/user",
		"auth_type":      "bearer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(created.Output, "super-secret") {
		t.Fatalf("creation output leaked secret: %s", created.Output)
	}
	name := parseTemporaryToolName(t, created.Output)
	generated, ok := registry.Get(name)
	if !ok || generated == nil {
		t.Fatalf("generated tool %q was not registered", name)
	}

	result, err := generated.ContextDetailedHandler(ExecutionContext{Context: context.Background(), SessionID: "session-a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuthorization != "Bearer super-secret" {
		t.Fatalf("authorization = %q", gotAuthorization)
	}
	if strings.Contains(result.Output, "super-secret") {
		t.Fatalf("tool result leaked secret: %s", result.Output)
	}
	if !strings.Contains(result.Output, "[REDACTED_CREDENTIAL]") {
		t.Fatalf("tool result was not redacted: %s", result.Output)
	}
	if _, err := generated.ContextDetailedHandler(ExecutionContext{Context: context.Background(), SessionID: "session-a"}, map[string]any{"token": "should-not-be-passed"}); err == nil {
		t.Fatal("temporary tool accepted a secret-shaped argument")
	}

	if _, err := generated.ContextDetailedHandler(ExecutionContext{Context: context.Background(), SessionID: "session-b"}, nil); err == nil {
		t.Fatal("temporary tool was callable from another session")
	}
}

func TestEphemeralCredentialToolSendsIdempotencyKeyForWrites(t *testing.T) {
	provider := testCredentialProvider{}
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))
	defer server.Close()
	oldValidator := ephemeralCredentialURLValidator
	ephemeralCredentialURLValidator = func(string) error { return nil }
	defer func() { ephemeralCredentialURLValidator = oldValidator }()

	service := NewEphemeralCredentialToolService(provider)
	registry := NewRegistry()
	service.RegisterTools(registry)
	created, err := service.create(ExecutionContext{Context: context.Background(), SessionID: "session-write"}, map[string]any{
		"credential_ref": "github-main", "name": "create", "description": "Create a resource",
		"method": "POST", "url_template": server.URL + "/resource", "idempotency_key": "operation-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(created.Output, "idempotency_key=operation-123") {
		t.Fatalf("creation output did not expose the retry key: %s", created.Output)
	}
	name := parseTemporaryToolName(t, created.Output)
	generated, ok := registry.Get(name)
	if !ok {
		t.Fatal("generated tool missing")
	}
	if _, err := generated.ContextDetailedHandler(ExecutionContext{Context: context.Background(), SessionID: "session-write"}, nil); err != nil {
		t.Fatal(err)
	}
	if gotKey != "operation-123" {
		t.Fatalf("Idempotency-Key = %q", gotKey)
	}
}

func TestUncertainOperationErrorRequiresVerification(t *testing.T) {
	err := &UncertainOperationError{Operation: "create-resource", Cause: context.DeadlineExceeded}
	if !strings.Contains(err.Error(), "unknown") || !strings.Contains(err.Error(), "verify before retry") {
		t.Fatalf("unexpected uncertainty error: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("uncertainty error did not preserve the cause")
	}
}

func TestEphemeralCredentialToolExpiresAndRevokes(t *testing.T) {
	service := NewEphemeralCredentialToolService(testCredentialProvider{})
	registry := NewRegistry()
	service.RegisterTools(registry)
	created, err := service.create(ExecutionContext{Context: context.Background(), SessionID: "session-a"}, map[string]any{
		"credential_ref": "ref-1",
		"name":           "temporary",
		"description":    "Temporary read",
		"url_template":   "https://example.com/resource",
		"ttl_seconds":    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	name := parseTemporaryToolName(t, created.Output)
	service.mu.Lock()
	service.tools["session-a"][name].expiresAt = time.Now().Add(-time.Second)
	service.mu.Unlock()
	if got := service.VisibleTools("session-a"); len(got) != 0 {
		t.Fatalf("expired temporary tools still visible: %d", len(got))
	}
	if _, ok := registry.Get(name); ok {
		t.Fatalf("expired temporary tool %q still registered", name)
	}

	created, err = service.create(ExecutionContext{Context: context.Background(), SessionID: "session-a"}, map[string]any{
		"credential_ref": "ref-1",
		"name":           "temporary-2",
		"description":    "Temporary read",
		"url_template":   "https://example.com/resource",
	})
	if err != nil {
		t.Fatal(err)
	}
	name = parseTemporaryToolName(t, created.Output)
	service.RevokeSession("session-a")
	if _, ok := registry.Get(name); ok {
		t.Fatalf("revoked temporary tool %q still registered", name)
	}
}

func TestEphemeralCredentialToolRejectsSensitiveStaticHeaders(t *testing.T) {
	_, err := parseEphemeralCredentialSpec(map[string]any{
		"credential_ref": "ref-1",
		"name":           "temporary",
		"description":    "Temporary read",
		"url_template":   "https://example.com/resource",
		"headers_json":   `{"Authorization":"should-not-be-here"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot carry credentials") {
		t.Fatalf("expected sensitive header rejection, got %v", err)
	}
}

func parseTemporaryToolName(t *testing.T, output string) string {
	t.Helper()
	for _, field := range strings.Fields(output) {
		if strings.HasPrefix(field, "tool_name=") {
			return strings.TrimPrefix(field, "tool_name=")
		}
	}
	t.Fatalf("tool name missing from output: %s", output)
	return ""
}
