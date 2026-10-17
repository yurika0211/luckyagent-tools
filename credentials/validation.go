package credentials

import (
	"fmt"
	"strings"
)

const DefaultScope = "default"
const defaultScope = DefaultScope

// ValidateID checks a credential reference before it is shown to a client.
func ValidateID(id string) (string, error) {
	return validatePart("credential id", id)
}

// ValidateKind checks a credential type label.
func ValidateKind(kind string) (string, error) {
	return validatePart("credential kind", kind)
}

// NormalizeScope applies the default scope and validates the result.
func NormalizeScope(scope string) (string, error) {
	return normalizeScope(scope)
}

func validatePart(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("%s contains an invalid control character", name)
	}
	if name == "credential id" {
		if len(value) > 64 {
			return "", fmt.Errorf("%s is too long", name)
		}
		for _, r := range value {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			default:
				return "", fmt.Errorf("%s may contain only letters, numbers, dot, underscore, and hyphen", name)
			}
		}
	}
	return value, nil
}

func normalizeScope(scope string) (string, error) {
	if strings.TrimSpace(scope) == "" {
		scope = defaultScope
	}
	return validatePart("credential scope", scope)
}
