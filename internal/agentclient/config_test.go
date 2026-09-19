package agentclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialPersistenceAndBootstrapFallback(t *testing.T) {
	dir := t.TempDir()
	bootstrap := filepath.Join(dir, "bootstrap.token")
	if err := os.WriteFile(bootstrap, []byte("bootstrap-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	cfg.Agent.WorkDir = dir
	cfg.Security.BootstrapTokenFile = bootstrap
	cfg.Security.CredentialFile = filepath.Join(dir, "agent.credential")

	credential, token, err := LoadAuth(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if credential != "" || token != "bootstrap-value" {
		t.Fatalf("unexpected initial auth: credential=%q token=%q", credential, token)
	}

	if err := os.WriteFile(cfg.Security.CredentialFile, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredential(cfg, "persistent-value"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(cfg.Security.CredentialFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode = %o", info.Mode().Perm())
	}

	credential, token, err = LoadAuth(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if credential != "persistent-value" || token != "" {
		t.Fatalf("persistent credential not preferred: credential=%q token=%q", credential, token)
	}
}
