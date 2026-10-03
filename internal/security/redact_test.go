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

func TestRedactSignedURLInTextAndJSON(t *testing.T) {
	message := `download http://127.0.0.1/pkg?expires=123&sig=top-secret failed`
	if got := RedactText(message); strings.Contains(got, "top-secret") || !strings.Contains(got, "sig=***REDACTED***") {
		t.Fatalf("signed URL leaked: %s", got)
	}
	got := RedactJSON(`{"error":"` + message + `"}`)
	if strings.Contains(got, "top-secret") {
		t.Fatalf("nested signed URL leaked: %s", got)
	}
}
