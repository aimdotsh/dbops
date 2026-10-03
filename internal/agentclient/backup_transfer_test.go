package agentclient

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestBackupTransferRoundTripAndChecksum(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	target := t.TempDir()
	file := filepath.Join(source, "dump.sql.gz")
	payload := make([]byte, 600<<10)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, payload, 0600); err != nil {
		t.Fatal(err)
	}
	sum, err := fileSHA256(file)
	if err != nil {
		t.Fatal(err)
	}
	id := "test-transfer-123456789"
	call := func(work, op string, p map[string]any) map[string]any {
		t.Helper()
		p["transfer_id"] = id
		r, err := backupTransfer(ctx, work, "backup.transfer."+op, p)
		if err != nil {
			t.Fatal(op, err)
		}
		return r
	}
	manifest := call(source, "export", map[string]any{"backup_path": file, "engine": "mysqldump", "sha256": sum})
	var chunks int
	for offset, size := int64(0), manifest["size_bytes"].(int64); offset < size; {
		chunk := call(source, "read", map[string]any{"offset": offset})
		ack := call(target, "write", map[string]any{"offset": offset, "chunk": chunk["chunk"]})
		if ack["bytes"] != chunk["bytes"] {
			t.Fatalf("short write at %d", offset)
		}
		if offset == 0 {
			if _, err := backupTransfer(ctx, target, "backup.transfer.write", map[string]any{"transfer_id": id, "offset": 0, "chunk": chunk["chunk"]}); err == nil {
				t.Fatal("accepted overwrite")
			}
		}
		offset += int64(chunk["bytes"].(int))
		chunks++
	}
	if chunks < 3 {
		t.Fatalf("expected multi-chunk transfer, got %d chunks", chunks)
	}
	if _, err := backupTransfer(ctx, target, "backup.transfer.finish", map[string]any{"transfer_id": id, "sha256": "invalid"}); err == nil {
		t.Fatal("accepted bad checksum")
	}
	result := call(target, "finish", map[string]any{"sha256": manifest["sha256"]})
	actual, err := fileSHA256(filepath.Join(result["path"].(string), "backup.sql.gz"))
	if err != nil || actual != sum {
		t.Fatal("transfer mismatch", err)
	}
	call(target, "cleanup", map[string]any{})
	if _, err := backupTransfer(ctx, target, "backup.transfer.cleanup", map[string]any{"transfer_id": "../../escape"}); err == nil {
		t.Fatal("accepted path traversal")
	}
}

func TestPackBackupClosesFilesDuringWalk(t *testing.T) {
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &original); err != nil {
		t.Skipf("cannot inspect open-file limit: %v", err)
	}
	limited := original
	if limited.Cur > 64 {
		limited.Cur = 64
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limited); err != nil {
			t.Skipf("cannot lower open-file limit: %v", err)
		}
		defer syscall.Setrlimit(syscall.RLIMIT_NOFILE, &original)
	}
	source := t.TempDir()
	for i := 0; i < 128; i++ {
		name := filepath.Join(source, fmt.Sprintf("part-%03d", i))
		if err := os.WriteFile(name, []byte("backup part"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(t.TempDir(), "backup.tgz")
	if err := packBackup(context.Background(), source, archive); err != nil {
		t.Fatalf("pack with many files: %v", err)
	}
	dest := t.TempDir()
	if err := unpackBackup(archive, dest); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		if _, err := os.Stat(filepath.Join(dest, fmt.Sprintf("part-%03d", i))); err != nil {
			t.Fatal(err)
		}
	}
}
