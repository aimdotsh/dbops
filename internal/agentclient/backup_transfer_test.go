package agentclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupTransferRoundTripAndChecksum(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	target := t.TempDir()
	file := filepath.Join(source, "dump.sql.gz")
	if err := os.WriteFile(file, []byte("test-backup"), 0600); err != nil {
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
	chunk := call(source, "read", map[string]any{"offset": 0})
	call(target, "write", map[string]any{"offset": 0, "chunk": chunk["chunk"]})
	if _, err := backupTransfer(ctx, target, "backup.transfer.write", map[string]any{"transfer_id": id, "offset": 0, "chunk": chunk["chunk"]}); err == nil {
		t.Fatal("accepted overwrite")
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
