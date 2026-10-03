package agentclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPhysicalBackupDigestDetectsDataChanges(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data.ibd")
	if err := os.WriteFile(data, []byte("aaaa"), 0600); err != nil {
		t.Fatal(err)
	}
	size, before, err := physicalBackupDigest(context.Background(), root)
	if err != nil || size != 4 {
		t.Fatalf("size=%d err=%v", size, err)
	}
	if err := os.WriteFile(data, []byte("bbbb"), 0600); err != nil {
		t.Fatal(err)
	}
	_, after, err := physicalBackupDigest(context.Background(), root)
	if err != nil || before == after {
		t.Fatalf("data corruption undetected: %v", err)
	}
	if err := os.Rename(data, filepath.Join(root, "other.ibd")); err != nil {
		t.Fatal(err)
	}
	_, renamed, err := physicalBackupDigest(context.Background(), root)
	if err != nil || after == renamed {
		t.Fatalf("rename undetected: %v", err)
	}
	if err := os.Symlink("other.ibd", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := physicalBackupDigest(context.Background(), root); err == nil {
		t.Fatal("accepted symlink")
	}
}

func TestPhysicalBackupDigestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := physicalBackupDigest(ctx, t.TempDir()); err == nil {
		t.Fatal("ignored cancellation")
	}
}
