package agentclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallNeverOverwritesConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "my.cnf")
	if err := os.WriteFile(p, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeNewFile(p, []byte("changed"), 0600); err == nil {
		t.Fatal("overwrote existing config")
	}
	got, _ := os.ReadFile(p)
	if string(got) != "existing" {
		t.Fatal("existing config changed")
	}
}
func TestClientTLSRejectsMissingCA(t *testing.T) {
	var cfg Config
	cfg.Security.VerifyServerTLS = true
	cfg.Security.CAFile = filepath.Join(t.TempDir(), "missing.pem")
	if _, err := clientTLSConfig(cfg); err == nil {
		t.Fatal("accepted invalid CA")
	}
}
