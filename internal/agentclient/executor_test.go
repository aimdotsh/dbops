package agentclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aimdotsh/dbops/internal/agentproto"
)

func TestExecutorAllowlist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "actions.yaml")
	content := []byte("version: 1\nactions:\n  - name: host.port.check\n    timeout: 5\n    risk: read_only\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	executor, err := LoadExecutor(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := executor.Execute(context.Background(), agentproto.ActionRequest{
		Action: "shell.execute",
	}); err == nil {
		t.Fatal("expected non-allowlisted action to fail")
	}

	result, err := executor.Execute(context.Background(), agentproto.ActionRequest{
		Action: "host.port.check",
		Params: map[string]any{"port": float64(0)},
	})
	if err == nil || result != nil {
		t.Fatal("expected invalid port to fail")
	}
}

func TestWebsocketURL(t *testing.T) {
	got, err := websocketURL("https://dbops.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://dbops.example.com/api/v1/agent/ws" {
		t.Fatalf("unexpected url: %s", got)
	}
}
