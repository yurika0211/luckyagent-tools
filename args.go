package tool

import (
	"encoding/json"
	"strings"
)

func cloneToolArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

func numberArg(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		parsed, err := n.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func stringArg(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func boolArg(v any, rest ...any) bool {
	if len(rest) >= 2 {
		args, _ := v.(map[string]any)
		key, _ := rest[0].(string)
		def, _ := rest[1].(bool)
		if args == nil || key == "" {
			return def
		}
		b, ok := args[key].(bool)
		if !ok {
			return def
		}
		return b
	}
	b, _ := v.(bool)
	return b
}
