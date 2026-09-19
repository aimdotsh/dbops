package agentclient

import (
	"bytes"
	"compress/gzip"
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

func mysqlBackup(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	baseDir, runDir, password, err := mysqlRuntimeParams(params)
	if err != nil {
		return nil, err
	}
	outputDir, _ := params["output_dir"].(string)
	if outputDir == "" || !filepath.IsAbs(outputDir) || filepath.Clean(outputDir) == "/" {
		return nil, errors.New("output_dir must be an absolute non-root path")
	}
	if strings.ContainsAny(outputDir, "\r\n\x00") {
		return nil, errors.New("output_dir contains invalid characters")
	}
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return nil, err
	}

	dumpBin := filepath.Join(baseDir, "bin", "mysqldump")
	if _, err := os.Stat(dumpBin); err != nil {
		return nil, fmt.Errorf("mysqldump not found: %w", err)
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, err
	}
	secretFile, err := os.CreateTemp(workDir, "mysql-backup-*.cnf")
	if err != nil {
		return nil, err
	}
	secretPath := secretFile.Name()
	defer os.Remove(secretPath)
	if err := secretFile.Chmod(0o600); err != nil {
		secretFile.Close()
		return nil, err
	}
	escaped := mysqlOption(password)
	if _, err := fmt.Fprintf(secretFile, "[client]\nuser=root\npassword=%s\nsocket=%s/mysql.sock\n", escaped, runDir); err != nil {
		secretFile.Close()
		return nil, err
	}
	if err := secretFile.Close(); err != nil {
		return nil, err
	}

	timestamp := time.Now().UTC().Format("20060102T150405Z")
	fileName := fmt.Sprintf("mysql-backup-%s.sql.gz", timestamp)
	if requested, _ := params["file_name"].(string); requested != "" {
		if filepath.Base(requested) != requested || strings.ContainsAny(requested, "/\\\r\n\x00") {
			return nil, errors.New("invalid backup file_name")
		}
		fileName = requested
		if !strings.HasSuffix(fileName, ".gz") {
			fileName += ".gz"
		}
	}
	outputPath := filepath.Join(outputDir, fileName)

	outFile, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o640)
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		_ = outFile.Close()
		if cleanup {
			_ = os.Remove(outputPath)
		}
	}()

	hash := sha256.New()
	multi := io.MultiWriter(outFile, hash)
	gz := gzip.NewWriter(multi)

	args := []string{
		"--defaults-extra-file=" + secretPath,
		"--single-transaction",
		"--routines",
		"--events",
		"--triggers",
		"--hex-blob",
		"--set-gtid-purged=OFF",
	}
	if all, _ := params["all_databases"].(bool); all {
		args = append(args, "--all-databases")
	} else {
		dbs := stringSliceParam(params["databases"])
		if len(dbs) == 0 {
			return nil, errors.New("databases is required when all_databases=false")
		}
		for _, db := range dbs {
			if !validDatabaseName(db) {
				return nil, fmt.Errorf("invalid database name %q", db)
			}
		}
		args = append(args, "--databases")
		args = append(args, dbs...)
	}

	cmd := exec.CommandContext(ctx, dumpBin, args...)
	cmd.Stdout = gz
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = gz.Close()
		return nil, fmt.Errorf("mysqldump failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if err := outFile.Sync(); err != nil {
		return nil, err
	}
	if err := outFile.Close(); err != nil {
		return nil, err
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return nil, err
	}
	cleanup = false

	return map[string]any{
		"engine":       "mysqldump",
		"backup_type":  "logical",
		"path":         outputPath,
		"size_bytes":   info.Size(),
		"sha256":       hex.EncodeToString(hash.Sum(nil)),
		"compressed":   true,
		"completed_at": time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func stringSliceParam(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func validDatabaseName(v string) bool {
	if v == "" || len(v) > 64 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '$' {
			continue
		}
		return false
	}
	return true
}
