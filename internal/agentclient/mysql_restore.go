package agentclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Logical restores are restricted to empty targets. Checksums and gzip integrity
// are verified before any SQL reaches the database.
func mysqlRestore(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	base, run, password, err := mysqlRuntimeParams(params)
	if err != nil {
		return nil, err
	}
	source, _ := params["backup_path"].(string)
	expected, _ := params["sha256"].(string)
	if !filepath.IsAbs(source) || len(expected) != 64 {
		return nil, errors.New("absolute backup path and SHA256 required")
	}
	info, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("backup must be a regular file")
	}
	if err = os.MkdirAll(workDir, 0700); err != nil {
		return nil, err
	}
	// A private verified copy prevents a changed source file from being streamed
	// after verification. The copy is removed after execution, successful or not.
	copyFile, err := os.CreateTemp(workDir, "restore-*.sql.gz")
	if err != nil {
		return nil, err
	}
	copyPath := copyFile.Name()
	defer os.Remove(copyPath)
	input, err := os.Open(source)
	if err != nil {
		copyFile.Close()
		return nil, err
	}
	_, err = io.Copy(copyFile, input)
	input.Close()
	closeErr := copyFile.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sum, err := fileSHA256(copyPath)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(sum, expected) {
		return nil, errors.New("backup checksum mismatch")
	}
	verify, err := os.Open(copyPath)
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(verify)
	if err != nil {
		verify.Close()
		return nil, err
	}
	_, err = io.Copy(io.Discard, gz)
	gz.Close()
	verify.Close()
	if err != nil {
		return nil, fmt.Errorf("invalid compressed backup: %w", err)
	}
	const countSQL = "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name NOT IN ('mysql','sys','performance_schema','information_schema');"
	out, err := runMySQLQuery(ctx, base, run, password, countSQL, false)
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || count != 0 {
		return nil, errors.New("restore target must have no user databases")
	}
	cfg, err := os.CreateTemp(workDir, "restore-client-*.cnf")
	if err != nil {
		return nil, err
	}
	cfgPath := cfg.Name()
	defer os.Remove(cfgPath)
	_, err = fmt.Fprintf(cfg, "[client]\nuser=root\npassword=%s\nsocket=%s/mysql.sock\n", mysqlOption(password), run)
	closeErr = cfg.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	stream, err := os.Open(copyPath)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	gz, err = gzip.NewReader(stream)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	cmd := exec.CommandContext(ctx, filepath.Join(base, "bin", "mysql"), "--defaults-extra-file="+cfgPath, "--binary-mode")
	cmd.Stdin = gz
	var stderr bytes.Buffer
	cmd.Stderr = &limitedBuffer{buf: &stderr, limit: 64 << 10}
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("restore failed; target requires inspection before retry: %w: %s", err, stderr.String())
	}
	out, err = runMySQLQuery(ctx, base, run, password, countSQL, false)
	if err != nil {
		return nil, err
	}
	count, err = strconv.Atoi(strings.TrimSpace(out))
	if err != nil || count == 0 {
		return nil, errors.New("restore completed without user databases")
	}
	return map[string]any{"restored": true, "user_databases": count, "sha256": sum}, nil
}
func mysqlOption(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`).Replace(value) + `"`
}
