package security

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactJSON(t *testing.T) {
	raw := "{\"password\":\"p1\",\"nested\":{\"bootstrap_token\":\"abc\",\"safe\":\"ok\"},\"items\":[{\"api_key\":\"k\"}]}"
	got := RedactJSON(raw)

	if strings.Contains(got, "p1") || strings.Contains(got, "abc") || strings.Contains(got, "\"k\"") {
		t.Fatalf("secret leaked: %s", got)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatal(err)
	}
	if v["password"] != "***REDACTED***" {
		t.Fatalf("password not redacted: %v", v)
	}
}
