package agentclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateOracleDatafilePath(t *testing.T) {
	for _, bad := range []string{"", "/", "relative/a.dbf", "/tmp/a.dbf';drop"} {
		if err := validateOracleDatafilePath(bad); err == nil {
			t.Fatalf("expected invalid path: %q", bad)
		}
	}
	if err := validateOracleDatafilePath("/u01/oradata/TEST/users02.dbf"); err != nil {
		t.Fatal(err)
	}
}

func TestOracleIdentifier(t *testing.T) {
	for _, good := range []string{"ORCL", "USERS", "APP_DATA_01", "SYS$USERS"} {
		if !validOracleIdentifier(good) {
			t.Fatalf("expected valid identifier: %q", good)
		}
	}
	for _, bad := range []string{"", "USERS;DROP", "A B", "A-B"} {
		if validOracleIdentifier(bad) {
			t.Fatalf("expected invalid identifier: %q", bad)
		}
	}
}

func TestPrepareOracleBackupDir(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "bin", "oracle"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "backup", "rman")
	if err := prepareOracleBackupDir(home, output); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(root, "backup"), output} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o750 {
			t.Fatalf("%s mode = %o, want 750", dir, info.Mode().Perm())
		}
	}
	if err := os.Chmod(output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := prepareOracleBackupDir(home, output); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("existing directory permissions changed: %o", info.Mode().Perm())
	}
	if err := os.Symlink(output, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := prepareOracleBackupDir(home, filepath.Join(root, "link", "nested")); err == nil {
		t.Fatal("expected symlink path to be rejected")
	}
}
