package platformbackup

import (
	"context"
	"github.com/aimdotsh/dbops/internal/storage"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotRestoreAndTamper(t *testing.T) {
	root := t.TempDir()
	stores, err := storage.Open(filepath.Join(root, "live", "dbops.db"), filepath.Join(root, "live", "metrics.db"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	if err = storage.Migrate(stores.Metadata, stores.Metrics); err != nil {
		t.Fatal(err)
	}
	if _, err = stores.Metadata.Exec("INSERT INTO operation_audit(actor_id,method,path,status_code,created_at) VALUES(1,'POST','/test',202,'2026-01-01')"); err != nil {
		t.Fatal(err)
	}
	s := Service{Metadata: stores.Metadata, Metrics: stores.Metrics, Directory: filepath.Join(root, "backups")}
	m, err := s.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(s.Directory, m.ID)
	dest := filepath.Join(root, "restored")
	if err = Restore(context.Background(), src, dest); err != nil {
		t.Fatal(err)
	}
	if err = Restore(context.Background(), src, dest); err == nil {
		t.Fatal("overwrote existing destination")
	}
	restored, err := storage.Open(filepath.Join(dest, "dbops.db"), filepath.Join(dest, "metrics.db"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var n int
	if err = restored.Metadata.QueryRow("SELECT count(*) FROM operation_audit").Scan(&n); err != nil || n != 1 {
		t.Fatalf("lost WAL-backed data: count=%d err=%v", n, err)
	}
	if err = os.WriteFile(filepath.Join(src, "metrics.db"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Restore(context.Background(), src, filepath.Join(root, "corrupt-restore")); err == nil {
		t.Fatal("accepted corrupt snapshot")
	}
}
