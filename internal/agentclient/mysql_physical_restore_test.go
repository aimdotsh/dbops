package agentclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPhysicalRecoveryStagesCopyWithoutChangingBackup(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"xtrabackup_checkpoints": "backup_type = full-backuped\n", "mysql.ibd": "test-data"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tool := filepath.Join(root, "xtrabackup")
	script := `#!/bin/sh
set -eu
mode= target= data=
for arg in "$@"; do
 case "$arg" in
 --prepare) mode=prepare;;
 --copy-back) mode=copy;;
 --target-dir=*) target=${arg#*=};;
 --datadir=*) data=${arg#*=};;
 esac
done
if [ "$mode" = prepare ]; then
 printf 'backup_type = full-prepared\n' > "$target/xtrabackup_checkpoints"
else
 cp "$target/mysql.ibd" "$data/mysql.ibd"
fi
`
	if err := os.WriteFile(tool, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	_, sum, err := physicalBackupDigest(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"backup_path": source, "sha256": sum, "checksum_scope": "sorted-file-manifest-v1", "xtrabackup_bin": tool}
	result, err := mysqlPhysicalRestore(context.Background(), filepath.Join(root, "work"), params)
	if err != nil {
		t.Fatal(err)
	}
	if result["restored"] != false || result["manual_review_required"] != true {
		t.Fatalf("unexpected activation: %v", result)
	}
	_, after, err := physicalBackupDigest(context.Background(), source)
	if err != nil || after != sum {
		t.Fatal("original backup changed", err)
	}
	if _, err := os.Stat(filepath.Join(result["restored_data_dir"].(string), "mysql.ibd")); err != nil {
		t.Fatal(err)
	}
	params["sha256"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	failureWork := filepath.Join(root, "failure")
	if _, err := mysqlPhysicalRestore(context.Background(), failureWork, params); err == nil {
		t.Fatal("accepted bad checksum")
	}
	entries, err := os.ReadDir(failureWork)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed staging not cleaned", err)
	}
}

func TestPhysicalCopyRejectsLinksAndNestedTarget(t *testing.T) {
	source := t.TempDir()
	if err := copyPhysicalBackup(context.Background(), source, filepath.Join(source, "nested")); err == nil {
		t.Fatal("accepted nested staging")
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}
	if err := copyPhysicalBackup(context.Background(), source, filepath.Join(t.TempDir(), "copy")); err == nil {
		t.Fatal("accepted symlink")
	}
}
