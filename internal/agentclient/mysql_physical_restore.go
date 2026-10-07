package agentclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Physical recovery is staged outside the live datadir. Activation needs a
// separate operator review of accounts, UUID, replication and rollback paths.
func mysqlPhysicalRestore(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	source, _ := params["backup_path"].(string)
	expected, _ := params["sha256"].(string)
	scope, _ := params["checksum_scope"].(string)
	tool, _ := params["xtrabackup_bin"].(string)
	if !filepath.IsAbs(source) || len(expected) != 64 || scope != "sorted-file-manifest-v1" {
		return nil, errors.New("physical recovery requires an absolute backup path and full-file manifest checksum")
	}
	if tool == "" {
		tool = "/usr/bin/xtrabackup"
	}
	if !filepath.IsAbs(tool) {
		return nil, errors.New("xtrabackup_bin must be absolute")
	}
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(workDir, "physical-recovery-")
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		}
	}()
	prepared := filepath.Join(stage, "prepared")
	if err := copyPhysicalBackup(ctx, source, prepared); err != nil {
		return nil, err
	}
	_, sum, err := physicalBackupDigest(ctx, prepared)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(sum, expected) {
		return nil, errors.New("physical backup checksum mismatch")
	}
	checkpoint, err := os.ReadFile(filepath.Join(prepared, "xtrabackup_checkpoints"))
	if err != nil {
		return nil, err
	}
	if !strings.Contains(string(checkpoint), "full-backuped") {
		return nil, errors.New("only unprepared full backups can be staged")
	}
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, tool, args...)
		var output bytes.Buffer
		limited := &limitedBuffer{buf: &output, limit: 64 << 10}
		cmd.Stdout, cmd.Stderr = limited, limited
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("physical recovery failed: %w: %s", err, output.String())
		}
		return nil
	}
	if err := run("--no-defaults", "--prepare", "--target-dir="+prepared); err != nil {
		return nil, err
	}
	checkpoint, err = os.ReadFile(filepath.Join(prepared, "xtrabackup_checkpoints"))
	if err != nil || !strings.Contains(string(checkpoint), "full-prepared") {
		return nil, errors.New("prepare did not produce a full-prepared checkpoint")
	}
	data := filepath.Join(stage, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		return nil, err
	}
	if err := run("--no-defaults", "--copy-back", "--target-dir="+prepared, "--datadir="+data); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(data, "mysql.ibd")); err != nil {
		return nil, fmt.Errorf("restored MySQL system tablespace missing: %w", err)
	}
	keep = true
	return map[string]any{"stage": "awaiting_manual_activation", "restored": false, "prepared": true, "manual_review_required": true, "staging_dir": stage, "prepared_dir": prepared, "restored_data_dir": data, "source_sha256": sum, "checksum_scope": scope}, nil
}

func copyPhysicalBackup(ctx context.Context, source, target string) error {
	root, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !root.IsDir() {
		return errors.New("backup source must be a real directory")
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return err
	}
	absoluteTarget := filepath.Join(parent, filepath.Base(target))
	if absoluteTarget == resolved || strings.HasPrefix(absoluteTarget, resolved+string(os.PathSeparator)) {
		return errors.New("staging directory must be outside backup")
	}
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if info.IsDir() {
			return os.Mkdir(dest, 0700)
		}
		if !info.Mode().IsRegular() {
			return errors.New("backup contains a non-regular file")
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
