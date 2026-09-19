package agentclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectMySQLDataDirDetectsExistingData(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "auto.cnf"), []byte("[auto]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := inspectMySQLDataDir(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !status.ValidMySQLData {
		t.Fatal("expected existing MySQL data marker")
	}
}

func TestMySQLPrecheckRejectsRelativeDataDir(t *testing.T) {
	_, err := mysqlPrecheck(context.Background(), map[string]any{
		"port":     float64(3306),
		"data_dir": "relative/path",
	})
	if err == nil {
		t.Fatal("expected relative data_dir to be rejected")
	}
}
