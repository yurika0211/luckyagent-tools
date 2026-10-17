package tool

// CloneToolArgs copies an argument map before an adapter rewrites a call.
func CloneToolArgs(args map[string]any) map[string]any { return cloneToolArgs(args) }

// NumberArg reads the numeric argument representations accepted by builtins.
func NumberArg(v any) (float64, bool) { return numberArg(v) }

// StringArg returns a trimmed string argument.
func StringArg(v any) string { return stringArg(v) }

// BoolArg reads a boolean value or an argument-map key with a fallback.
func BoolArg(v any, rest ...any) bool { return boolArg(v, rest...) }

// FilesystemPolicyFromOptional resolves the legacy variadic policy input.
func FilesystemPolicyFromOptional(policies []FilesystemPolicy) FilesystemPolicy {
	return filesystemPolicyFromOptional(policies)
}

// ValidatePath applies the shared builtin path validation rules.
func ValidatePath(path string) error { return validatePath(path) }

// ExpandSandboxPath expands the path forms understood by filesystem tools.
func ExpandSandboxPath(path string) string { return expandSandboxPath(path) }

// SandboxWorkspaceDir returns the compatible default tool workspace.
func SandboxWorkspaceDir() string { return sandboxWorkspaceDir() }

// ResolveWorkspacePath restricts an adapter's path to the tool workspace.
func ResolveWorkspacePath(path string) (string, error) { return resolveWorkspacePath(path) }

// BoundedIntArg reads an integer argument and clamps it to the supplied bounds.
func BoundedIntArg(args map[string]any, key string, def, minValue, maxValue int) int {
	return boundedIntArg(args, key, def, minValue, maxValue)
}

// PrettyStructuredValue formats structured tool output as indented JSON.
func PrettyStructuredValue(v any) (string, error) { return prettyStructuredValue(v) }

// ToolAction reads the normalized action of a multi-action tool.
func ToolAction(args map[string]any) string { return toolAction(args) }

// MergeToolParams combines schemas while preserving the primary action field.
func MergeToolParams(base map[string]Param, groups ...map[string]Param) map[string]Param {
	return mergedToolParams(base, groups...)
}

// ValidateHTTPMethod enforces the builtin's opt-in for mutating HTTP methods.
func ValidateHTTPMethod(method string, allowMutation bool) error {
	return validateHTTPMethod(method, allowMutation)
}

// ValidateRequestHeader rejects unsafe or protected request headers.
func ValidateRequestHeader(name, value string) error { return validateRequestHeader(name, value) }

// ValidateFetchURL applies the shared fetch URL restrictions.
func ValidateFetchURL(rawURL string) error { return validateFetchURL(rawURL) }

// ParamSchema produces the nested JSON schema used by function calling.
func ParamSchema(p Param) map[string]any { return paramSchema(p) }
