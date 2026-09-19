package security

import (
	"encoding/json"
	"strings"
)

var sensitiveFragments = []string{
	"password", "passwd", "pwd", "secret", "token", "credential",
	"authorization", "api_key", "apikey", "private_key", "access_key",
	"secret_key", "bootstrap",
}

func IsSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, fragment := range sensitiveFragments {
		if strings.Contains(k, fragment) {
			return true
		}
	}
	return false
}

func RedactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, value := range x {
			if IsSensitiveKey(k) {
				out[k] = "***REDACTED***"
				continue
			}
			out[k] = RedactValue(value)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, value := range x {
			out[i] = RedactValue(value)
		}
		return out
	default:
		return v
	}
}

func RedactJSON(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "{}"
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return "{\"redacted\":\"invalid-json\"}"
	}
	b, err := json.Marshal(RedactValue(value))
	if err != nil {
		return "{\"redacted\":\"marshal-error\"}"
	}
	return string(b)
}

func RedactJSONBytes(raw []byte) []byte {
	return []byte(RedactJSON(string(raw)))
}
