package agentclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func mysqlXtraBackup(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	_, runDir, password, err := mysqlRuntimeParams(params)
	if err != nil {
		return nil, err
	}
	dataDir, _ := params["data_dir"].(string)
	outputDir, _ := params["output_dir"].(string)
	tool, _ := params["xtrabackup_bin"].(string)
	if dataDir == "" || !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) == "/" {
		return nil, errors.New("data_dir must be an absolute non-root path")
	}
	if outputDir == "" || !filepath.IsAbs(outputDir) || filepath.Clean(outputDir) == "/" || strings.ContainsAny(outputDir, "\r\n\x00") {
		return nil, errors.New("output_dir must be an absolute non-root path")
	}
	if tool == "" {
		tool = "/usr/bin/xtrabackup"
	}
	if !filepath.IsAbs(tool) || strings.ContainsAny(tool, "\r\n\x00") {
		return nil, errors.New("xtrabackup_bin must be an absolute path")
	}
	if _, err := os.Stat(tool); err != nil {
		return nil, fmt.Errorf("xtrabackup not found: %w", err)
	}
	if _, err := os.Stat(dataDir); err != nil {
		return nil, fmt.Errorf("data_dir not found: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, err
	}
	target := filepath.Join(outputDir, "xtrabackup-"+time.Now().UTC().Format("20060102T150405Z"))
	if requested, _ := params["file_name"].(string); requested != "" {
		if requested == "." || requested == ".." || filepath.Base(requested) != requested || strings.ContainsAny(requested, "/\\\r\n\x00") {
			return nil, errors.New("invalid physical backup directory name")
		}
		target = filepath.Join(outputDir, requested)
	}
	if err := os.Mkdir(target, 0o750); err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(target)
		}
	}()
	secret, err := os.CreateTemp(workDir, "mysql-xtrabackup-*.cnf")
	if err != nil {
		return nil, err
	}
	secretPath := secret.Name()
	defer os.Remove(secretPath)
	if err := secret.Chmod(0o600); err != nil {
		secret.Close()
		return nil, err
	}
	if _, err := fmt.Fprintf(secret, "[client]\nuser=root\npassword=%s\nsocket=%s\n", mysqlOption(password), mysqlOption(filepath.Join(runDir, "mysql.sock"))); err != nil {
		secret.Close()
		return nil, err
	}
	if err := secret.Close(); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, tool, "--defaults-file="+secretPath, "--backup", "--datadir="+dataDir, "--target-dir="+target)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("xtrabackup failed: %w: %s", err, strings.TrimSpace(output.String()))
	}
	_, err = os.ReadFile(filepath.Join(target, "xtrabackup_checkpoints"))
	if err != nil {
		return nil, fmt.Errorf("xtrabackup checkpoint missing: %w", err)
	}
	size, checksum, err := physicalBackupDigest(ctx, target)
	if err != nil {
		return nil, err
	}
	cleanup = false
	return map[string]any{"engine": "xtrabackup", "backup_type": "physical", "path": target, "size_bytes": size, "sha256": checksum, "checksum_scope": "sorted-file-manifest-v1", "prepared": false, "completed_at": time.Now().UTC().Format(time.RFC3339)}, nil
}

// Hash a deterministic manifest of relative names, sizes and full file digests.
// prepare changes backup files; verify this digest before preparing a copy.
func physicalBackupDigest(ctx context.Context, root string) (int64, string, error) {
	var total int64
	manifest := sha256.New()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular backup file: %s", path)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		n, copyErr := io.Copy(hash, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(manifest, "%q %d %x\n", filepath.ToSlash(rel), n, hash.Sum(nil))
		total += n
		return nil
	})
	return total, hex.EncodeToString(manifest.Sum(nil)), err
}
