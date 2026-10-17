package tool

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yurika0211/luckyagent-tools/credentials"
)

const (
	ephemeralCredentialFactoryName = "create_ephemeral_credential_tool"
	ephemeralCredentialToolPrefix  = "tmp_credential_"
	ephemeralCredentialDefaultTTL  = 10 * time.Minute
	ephemeralCredentialMaxTTL      = time.Hour
	ephemeralCredentialMaxBody     = 1 << 20
	ephemeralCredentialMaxOutput   = 1 << 20
)

var ephemeralTemplateToken = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9_]*)\}`)

// Kept as a variable so focused tests can use an httptest server without
// weakening the production public-address validation.
var ephemeralCredentialURLValidator = validateFetchURL

// EphemeralCredentialToolService creates short-lived, session-scoped HTTP
// tools. The generated tool stores only a credential reference; it never
// stores the decrypted credential value.
type EphemeralCredentialToolService struct {
	mu       sync.Mutex
	resolver CredentialProvider
	registry *Registry
	tools    map[string]map[string]*ephemeralCredentialTool
}

type ephemeralCredentialTool struct {
	scope         string
	name          string
	displayName   string
	description   string
	credentialRef string
	method        string
	urlTemplate   string
	bodyTemplate  string
	authType      string
	headerName    string
	staticHeaders map[string]string
	parameters    map[string]Param
	expiresAt     time.Time
	maxResponse   int
}

// NewEphemeralCredentialToolService creates the in-memory temporary-tool
// manager. The credential store is opened only for the duration of a call.
func NewEphemeralCredentialToolService(resolver CredentialProvider) *EphemeralCredentialToolService {
	return &EphemeralCredentialToolService{
		resolver: resolver,
		tools:    make(map[string]map[string]*ephemeralCredentialTool),
	}
}

// RegisterTools exposes only the factory. Generated wrappers are hidden from
// the global registry's model list and are added per session by the agent.
func (s *EphemeralCredentialToolService) RegisterTools(r *Registry) {
	if s == nil || r == nil {
		return
	}
	s.mu.Lock()
	s.registry = r
	s.mu.Unlock()
	r.Register(s.factoryTool())
}

// factoryTool is the model-facing tool that creates a temporary credential
// bound HTTP tool after the host's normal approval gate allows the call.
func (s *EphemeralCredentialToolService) factoryTool() *Tool {
	return &Tool{
		Name:        ephemeralCredentialFactoryName,
		Description: "Create a short-lived session-scoped HTTP tool backed by a stored credential. The credential stays inside the tool executor and is never returned to the model. Use only declarative HTTP templates; no scripts or shell commands.",
		Category:    CatBuiltin,
		Source:      "builtin",
		Permission:  PermApprove,
		Parameters: map[string]Param{
			"credential_ref":     {Type: "string", Description: "Stored credential reference. The secret value must never be supplied as an argument.", Required: true},
			"name":               {Type: "string", Description: "Human-readable operation name. The runtime generates the actual temporary tool name.", Required: true},
			"description":        {Type: "string", Description: "Short description shown to the model when the temporary tool is available.", Required: true},
			"method":             {Type: "string", Description: "HTTP method. GET, HEAD, OPTIONS, POST, PUT, PATCH, and DELETE are supported; mutation methods require approval.", Required: false, Default: "GET"},
			"url_template":       {Type: "string", Description: "HTTP or HTTPS URL template. Use {parameter} placeholders only in the path or query; the host is fixed at creation time.", Required: true},
			"body_template":      {Type: "string", Description: "Optional JSON template. Use {parameter} placeholders as JSON values, for example {\"query\":{query}}.", Required: false},
			"auth_type":          {Type: "string", Description: "Authentication mode: bearer, api_key, or raw_header.", Required: false, Default: "bearer"},
			"header_name":        {Type: "string", Description: "Header used for raw_header; optional for bearer and api_key.", Required: false},
			"headers_json":       {Type: "string", Description: "Optional static non-sensitive request headers as a JSON object. Authentication headers are rejected here.", Required: false},
			"input_schema":       {Type: "string", Description: "Optional JSON Schema object describing the temporary tool inputs.", Required: false, Default: `{"type":"object","properties":{},"required":[]}`},
			"ttl_seconds":        {Type: "number", Description: "Lifetime in seconds (default 600, maximum 3600).", Required: false, Default: 600},
			"max_response_bytes": {Type: "number", Description: "Maximum response bytes returned by the temporary tool (default 32768, maximum 1048576).", Required: false, Default: 32768},
		},
		ContextDetailedHandler: s.create,
	}
}

// VisibleTools returns schemas for temporary tools belonging to one session.
// The wrappers remain hidden from Registry.ListModelVisible so another
// session cannot discover them.
func (s *EphemeralCredentialToolService) VisibleTools(scope string) []map[string]any {
	scope = strings.TrimSpace(scope)
	if s == nil || scope == "" {
		return nil
	}
	names := s.expire(scope, time.Now())
	for _, name := range names {
		s.unregister(name)
	}
	s.mu.Lock()
	items := make([]*ephemeralCredentialTool, 0, len(s.tools[scope]))
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

// RevokeSession removes all temporary tools belonging to a session.
func (s *EphemeralCredentialToolService) RevokeSession(scope string) {
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

// Close removes every generated wrapper from the registry.
func (s *EphemeralCredentialToolService) Close() error {
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

func (s *EphemeralCredentialToolService) create(exec ExecutionContext, args map[string]any) (ToolCallResult, error) {
	scope := strings.TrimSpace(exec.SessionID)
	if scope == "" {
		return ToolCallResult{}, fmt.Errorf("temporary credential tools require a session")
	}
	if strings.EqualFold(strings.TrimSpace(exec.Source), "autonomy") {
		return ToolCallResult{}, fmt.Errorf("temporary credential tools cannot be created in background autonomy tasks")
	}
	spec, err := parseEphemeralCredentialSpec(args)
	if err != nil {
		return ToolCallResult{}, err
	}
	s.mu.Lock()
	if s.registry == nil {
		s.mu.Unlock()
		return ToolCallResult{}, fmt.Errorf("temporary credential tool registry is unavailable")
	}
	name, err := randomEphemeralToolName()
	if err != nil {
		s.mu.Unlock()
		return ToolCallResult{}, err
	}
	item := &ephemeralCredentialTool{
		scope: scope, name: name, displayName: spec.displayName, description: spec.description,
		credentialRef: spec.credentialRef, method: spec.method, urlTemplate: spec.urlTemplate,
		bodyTemplate: spec.bodyTemplate, authType: spec.authType, headerName: spec.headerName,
		staticHeaders: spec.staticHeaders, parameters: spec.parameters, expiresAt: time.Now().Add(spec.ttl),
		maxResponse: spec.maxResponse,
	}
	s.tools[scope] = appendEphemeralTool(s.tools[scope], item)
	registry := s.registry
	s.mu.Unlock()
	registry.Register(item.asToolBound(s))
	return ToolCallResult{Output: fmt.Sprintf("Temporary credential tool created. tool_name=%s name=%s credential_ref=%s expires_at=%s", item.name, item.displayName, item.credentialRef, item.expiresAt.UTC().Format(time.RFC3339))}, nil
}

type parsedEphemeralSpec struct {
	displayName   string
	description   string
	credentialRef string
	method        string
	urlTemplate   string
	bodyTemplate  string
	authType      string
	headerName    string
	staticHeaders map[string]string
	parameters    map[string]Param
	ttl           time.Duration
	maxResponse   int
}

func parseEphemeralCredentialSpec(args map[string]any) (parsedEphemeralSpec, error) {
	var spec parsedEphemeralSpec
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
	spec.method = strings.ToUpper(strings.TrimSpace(credentialStringArg(args, "method")))
	if spec.method == "" {
		spec.method = http.MethodGet
	}
	if err := validateHTTPMethod(spec.method, true); err != nil {
		return spec, err
	}
	spec.urlTemplate = strings.TrimSpace(credentialStringArg(args, "url_template"))
	if err := validateEphemeralURLTemplate(spec.urlTemplate); err != nil {
		return spec, err
	}
	spec.bodyTemplate = strings.TrimSpace(credentialStringArg(args, "body_template"))
	if len(spec.bodyTemplate) > ephemeralCredentialMaxBody {
		return spec, fmt.Errorf("body_template exceeds %d bytes", ephemeralCredentialMaxBody)
	}
	spec.authType = strings.ToLower(strings.TrimSpace(credentialStringArg(args, "auth_type")))
	if spec.authType == "" {
		spec.authType = "bearer"
	}
	switch spec.authType {
	case "bearer":
		spec.headerName = "Authorization"
	case "api_key":
		spec.headerName = "X-API-Key"
	case "raw_header":
		spec.headerName = strings.TrimSpace(credentialStringArg(args, "header_name"))
		if spec.headerName == "" {
			return spec, fmt.Errorf("header_name is required for raw_header")
		}
	default:
		return spec, fmt.Errorf("auth_type must be bearer, api_key, or raw_header")
	}
	if err := validateCredentialHeaderName(spec.headerName); err != nil {
		return spec, err
	}
	spec.staticHeaders, err = parseStaticHeaders(credentialStringArg(args, "headers_json"))
	if err != nil {
		return spec, err
	}
	spec.parameters, err = parseEphemeralInputSchema(credentialStringArg(args, "input_schema"))
	if err != nil {
		return spec, err
	}
	if err := validateTemplateParameters(spec.urlTemplate, spec.bodyTemplate, spec.parameters); err != nil {
		return spec, err
	}
	ttlSeconds := credentialIntArg(args, "ttl_seconds", int(ephemeralCredentialDefaultTTL/time.Second))
	if ttlSeconds <= 0 || time.Duration(ttlSeconds)*time.Second > ephemeralCredentialMaxTTL {
		return spec, fmt.Errorf("ttl_seconds must be between 1 and %d", int(ephemeralCredentialMaxTTL/time.Second))
	}
	spec.ttl = time.Duration(ttlSeconds) * time.Second
	spec.maxResponse = credentialIntArg(args, "max_response_bytes", 32<<10)
	if spec.maxResponse <= 0 || spec.maxResponse > ephemeralCredentialMaxOutput {
		return spec, fmt.Errorf("max_response_bytes must be between 1 and %d", ephemeralCredentialMaxOutput)
	}
	return spec, nil
}

func (s *EphemeralCredentialToolService) execute(exec ExecutionContext, scope, name string, args map[string]any) (ToolCallResult, error) {
	if strings.EqualFold(strings.TrimSpace(exec.Source), "autonomy") {
		return ToolCallResult{}, fmt.Errorf("temporary credential tools cannot run in background autonomy tasks")
	}
	if err := rejectEphemeralSecretArguments(args); err != nil {
		return ToolCallResult{}, err
	}
	item, err := s.lookup(scope, name)
	if err != nil {
		return ToolCallResult{}, err
	}
	rawURL, err := renderURLTemplate(item.urlTemplate, args, item.parameters)
	if err != nil {
		return ToolCallResult{}, err
	}
	if err := ephemeralCredentialURLValidator(rawURL); err != nil {
		return ToolCallResult{}, fmt.Errorf("validate temporary credential URL: %w", err)
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return ToolCallResult{}, fmt.Errorf("parse temporary credential URL: %w", err)
	}
	body, err := renderJSONTemplate(item.bodyTemplate, args, item.parameters)
	if err != nil {
		return ToolCallResult{}, err
	}
	if len(body) > ephemeralCredentialMaxBody {
		return ToolCallResult{}, fmt.Errorf("rendered request body exceeds %d bytes", ephemeralCredentialMaxBody)
	}
	requestCtx := exec.Context
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	req, err := http.NewRequestWithContext(requestCtx, item.method, rawURL, strings.NewReader(body))
	if err != nil {
		return ToolCallResult{}, fmt.Errorf("create temporary credential request: %w", err)
	}
	for key, value := range item.staticHeaders {
		req.Header.Set(key, value)
	}
	req.Header.Set("User-Agent", "luckyagent-ephemeral-credential-tool")
	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("temporary credential request stopped after 5 redirects")
			}
			if !strings.EqualFold(next.URL.Hostname(), parsedURL.Hostname()) {
				return fmt.Errorf("temporary credential request cannot redirect to another host")
			}
			if err := ephemeralCredentialURLValidator(next.URL.String()); err != nil {
				return fmt.Errorf("temporary credential redirect validation failed: %w", err)
			}
			return nil
		},
	}

	var output string
	resolver := credentialResolver{provider: s.resolver}
	err = resolver.withCredential(requestCtx, item.credentialRef, func(secret []byte) error {
		value := string(secret)
		switch item.authType {
		case "bearer":
			req.Header.Set(item.headerName, "Bearer "+value)
		case "api_key", "raw_header":
			req.Header.Set(item.headerName, value)
		}
		defer req.Header.Del(item.headerName)
		resp, requestErr := client.Do(req)
		if requestErr != nil {
			return fmt.Errorf("send temporary credential request: %w", requestErr)
		}
		defer resp.Body.Close()
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(item.maxResponse)+1))
		if readErr != nil {
			return fmt.Errorf("read temporary credential response: %w", readErr)
		}
		truncated := len(data) > item.maxResponse
		if truncated {
			data = data[:item.maxResponse]
		}
		data = bytes.ReplaceAll(data, secret, []byte("[REDACTED_CREDENTIAL]"))
		output = formatEphemeralHTTPResult(resp, data, truncated)
		return nil
	})
	if err != nil {
		return ToolCallResult{}, err
	}
	return ToolCallResult{Output: output}, nil
}

func rejectEphemeralSecretArguments(args map[string]any) error {
	for key := range args {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "value", "secret", "password", "token", "api_key", "apikey", "credential":
			return fmt.Errorf("temporary credential tool arguments cannot contain secret field %q", key)
		}
	}
	return nil
}

func (s *EphemeralCredentialToolService) lookup(scope, name string) (*ephemeralCredentialTool, error) {
	if strings.TrimSpace(scope) == "" {
		return nil, fmt.Errorf("temporary credential tool requires a session")
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("temporary credential tool name is required")
	}
	expired := s.expire(scope, time.Now())
	for _, item := range expired {
		s.unregister(item)
	}
	s.mu.Lock()
	item := s.tools[scope][name]
	s.mu.Unlock()
	if item == nil {
		return nil, fmt.Errorf("temporary credential tool %q is not available in this session", name)
	}
	return item, nil
}

func (s *EphemeralCredentialToolService) expire(scope string, now time.Time) []string {
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

func (s *EphemeralCredentialToolService) unregister(name string) {
	if s == nil || s.registry == nil || strings.TrimSpace(name) == "" {
		return
	}
	s.registry.Unregister(name)
}

func appendEphemeralTool(items map[string]*ephemeralCredentialTool, item *ephemeralCredentialTool) map[string]*ephemeralCredentialTool {
	if items == nil {
		items = make(map[string]*ephemeralCredentialTool)
	}
	items[item.name] = item
	return items
}

func (e *ephemeralCredentialTool) asTool() *Tool {
	return &Tool{
		Name:            e.name,
		Description:     e.description,
		Parameters:      e.parameters,
		Category:        CatBuiltin,
		Source:          "ephemeral_credential",
		Permission:      PermApprove,
		ParallelSafe:    false,
		HiddenFromModel: true,
		ContextDetailedHandler: func(exec ExecutionContext, args map[string]any) (ToolCallResult, error) {
			return nilEphemeralServiceResult(), fmt.Errorf("temporary credential wrapper is detached")
		},
	}
}

// nilEphemeralServiceResult keeps the wrapper construction explicit; the
// service replaces this handler with a bound closure before registration.
func nilEphemeralServiceResult() ToolCallResult { return ToolCallResult{} }

type credentialResolver struct{ provider CredentialProvider }

func (r credentialResolver) withCredential(ctx context.Context, ref string, fn func([]byte) error) error {
	if r.provider == nil {
		return fmt.Errorf("credential home directory is unavailable")
	}
	return r.provider.WithCredential(ctx, ref, fn)
}

func randomEphemeralToolName() (string, error) {
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate temporary credential tool id: %w", err)
	}
	return ephemeralCredentialToolPrefix + hex.EncodeToString(buf), nil
}

func credentialStringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, _ := args[key].(string)
	return value
}

func credentialIntArg(args map[string]any, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if n, err := strconv.Atoi(string(value)); err == nil {
			return n
		}
	}
	return fallback
}

func validateEphemeralURLTemplate(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("url_template is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("url_template must be an absolute HTTP or HTTPS URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("url_template must use http or https")
	}
	if strings.Contains(parsed.Host, "{") || strings.Contains(parsed.Host, "}") {
		return fmt.Errorf("url_template host cannot contain parameters")
	}
	if parsed.User != nil {
		return fmt.Errorf("url_template cannot contain user info")
	}
	return nil
}

func parseEphemeralInputSchema(raw string) (map[string]Param, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]Param{}, nil
	}
	var schema struct {
		Type       string                    `json:"type"`
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return nil, fmt.Errorf("parse input_schema: %w", err)
	}
	if schema.Type != "" && schema.Type != "object" {
		return nil, fmt.Errorf("input_schema.type must be object")
	}
	if len(schema.Properties) > 32 {
		return nil, fmt.Errorf("input_schema supports at most 32 parameters")
	}
	required := make(map[string]struct{}, len(schema.Required))
	for _, name := range schema.Required {
		required[strings.TrimSpace(name)] = struct{}{}
	}
	result := make(map[string]Param, len(schema.Properties))
	for name, rawDef := range schema.Properties {
		if !validEphemeralParameterName(name) {
			return nil, fmt.Errorf("invalid input parameter name %q", name)
		}
		typeName, _ := rawDef["type"].(string)
		if typeName == "" {
			typeName = "string"
		}
		switch typeName {
		case "string", "number", "integer", "boolean", "array", "object":
		default:
			return nil, fmt.Errorf("unsupported input parameter type %q", typeName)
		}
		description, _ := rawDef["description"].(string)
		schemaFields := make(map[string]any)
		for key, value := range rawDef {
			if key != "type" && key != "description" && key != "default" {
				schemaFields[key] = value
			}
		}
		_, isRequired := required[name]
		result[name] = Param{Type: typeName, Description: description, Required: isRequired, Schema: schemaFields}
	}
	return result, nil
}

func parseStaticHeaders(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}, nil
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return nil, fmt.Errorf("parse headers_json: %w", err)
	}
	if len(headers) > 32 {
		return nil, fmt.Errorf("at most 32 static headers are allowed")
	}
	for name, value := range headers {
		if err := validateRequestHeader(name, value); err != nil {
			return nil, err
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "authorization", "cookie", "set-cookie", "proxy-authorization", "x-api-key", "x-auth-token":
			return nil, fmt.Errorf("static header %q cannot carry credentials", name)
		}
	}
	return headers, nil
}

func validateCredentialHeaderName(name string) error {
	if err := validateRequestHeader(name, "placeholder"); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "host", "cookie", "set-cookie", "proxy-authorization", "content-length", "transfer-encoding":
		return fmt.Errorf("credential header %q is not allowed", name)
	}
	return nil
}

func validateTemplateParameters(urlTemplate, bodyTemplate string, parameters map[string]Param) error {
	for _, template := range []string{urlTemplate, bodyTemplate} {
		for _, match := range ephemeralTemplateToken.FindAllStringSubmatch(template, -1) {
			if _, ok := parameters[match[1]]; !ok {
				return fmt.Errorf("template parameter %q is missing from input_schema", match[1])
			}
		}
	}
	return nil
}

func validEphemeralParameterName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for i, r := range name {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') || (i > 0 && r == '_') {
			continue
		}
		return false
	}
	return true
}

func renderURLTemplate(template string, args map[string]any, parameters map[string]Param) (string, error) {
	var firstErr error
	rendered := ephemeralTemplateToken.ReplaceAllStringFunc(template, func(token string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(token, "{"), "}")
		value, ok := args[name]
		if !ok || value == nil {
			firstErr = fmt.Errorf("missing temporary tool parameter %q", name)
			return token
		}
		if _, ok := parameters[name]; !ok {
			firstErr = fmt.Errorf("unknown temporary tool parameter %q", name)
			return token
		}
		return url.PathEscape(fmt.Sprint(value))
	})
	if firstErr != nil {
		return "", firstErr
	}
	return rendered, nil
}

func renderJSONTemplate(template string, args map[string]any, parameters map[string]Param) (string, error) {
	if strings.TrimSpace(template) == "" {
		return "", nil
	}
	var firstErr error
	rendered := ephemeralTemplateToken.ReplaceAllStringFunc(template, func(token string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(token, "{"), "}")
		value, ok := args[name]
		if !ok || value == nil {
			firstErr = fmt.Errorf("missing temporary tool parameter %q", name)
			return token
		}
		if _, ok := parameters[name]; !ok {
			firstErr = fmt.Errorf("unknown temporary tool parameter %q", name)
			return token
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			firstErr = fmt.Errorf("encode temporary tool parameter %q: %w", name, err)
			return token
		}
		return string(encoded)
	})
	if firstErr != nil {
		return "", firstErr
	}
	if !json.Valid([]byte(rendered)) {
		return "", fmt.Errorf("body_template must render valid JSON; use placeholders as JSON values")
	}
	return rendered, nil
}

func formatEphemeralHTTPResult(resp *http.Response, data []byte, truncated bool) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("HTTP %s\n", resp.Status))
	if ct := strings.TrimSpace(resp.Header.Get("Content-Type")); ct != "" {
		b.WriteString("Content-Type: " + ct + "\n")
	}
	if truncated {
		b.WriteString("[response truncated]\n")
	}
	b.WriteString("\n")
	b.Write(data)
	return b.String()
}

func (e *ephemeralCredentialTool) asToolBound(service *EphemeralCredentialToolService) *Tool {
	t := e.asTool()
	t.ContextDetailedHandler = func(exec ExecutionContext, args map[string]any) (ToolCallResult, error) {
		if strings.TrimSpace(exec.SessionID) != e.scope {
			return ToolCallResult{}, fmt.Errorf("temporary credential tool %q is not available in this session", e.name)
		}
		return service.execute(exec, e.scope, e.name, args)
	}
	return t
}
