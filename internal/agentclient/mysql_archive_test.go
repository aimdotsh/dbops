package agentclient

import (
	"os"
	"strings"
	"testing"
)

func TestArchiveControlSentinelLifecycle(t *testing.T) {
	dir := t.TempDir()
	params := map[string]any{"archive_job_id": 42}

	if _, err := mysqlArchiveControl(dir, "mysql.archive.pause", params); err != nil {
		t.Fatal(err)
	}
	path, err := archiveSentinelPath(dir, 42)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != "pause" {
		t.Fatalf("unexpected pause sentinel: %q", string(b))
	}

	if _, err := mysqlArchiveControl(dir, "mysql.archive.resume", params); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("sentinel should be removed on resume: %v", err)
	}

	if _, err := mysqlArchiveControl(dir, "mysql.archive.stop", params); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != "stop" {
		t.Fatalf("unexpected stop sentinel: %q", string(b))
	}
}

func TestSafeArchiveWhere(t *testing.T) {
	for _, bad := range []string{"", "id > 1; delete", "id > 1 -- comment", "id > 1\nOR 1=1"} {
		if safeArchiveWhere(bad) {
			t.Fatalf("expected unsafe where to be rejected: %q", bad)
		}
	}
	if !safeArchiveWhere("create_time < '2026-01-01 00:00:00' AND id > 0") {
		t.Fatal("expected controlled where to be accepted")
	}
}


func TestArchiveControlsUseReservedLane(t *testing.T) {
	for _, action := range []string{"mysql.archive.pause", "mysql.archive.resume", "mysql.archive.stop"} {
		if !isControlAction(action) {
			t.Fatalf("%s should use reserved control lane", action)
		}
	}
	if isControlAction("mysql.archive.start") {
		t.Fatal("archive start must remain subject to normal concurrency limits")
	}
}
