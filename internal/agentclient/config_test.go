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

func TestExplicitAdvertiseIP(t *testing.T) {
	var cfg Config
	cfg.Agent.AdvertiseIP = "10.10.1.25"
	client := &Client{cfg: cfg}
	if got := client.advertiseIP(); got != "10.10.1.25" {
		t.Fatalf("advertised wrong interface: %s", got)
	}
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte("server:\n  url: https://example.test\nagent:\n  advertise_ip: bad-host-name\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("invalid advertise IP accepted")
	}
}
