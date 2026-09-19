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
	"strconv"
	"strings"
	"time"
)

func postgresStatus(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	query := "SELECT 'STATUS|'||current_setting('server_version')||'|'||" +
		"CASE WHEN pg_is_in_recovery() THEN 'standby' ELSE 'primary' END||'|'||" +
		"to_char(pg_postmaster_start_time(),'YYYY-MM-DD HH24:MI:SS');"
	out, err := runPostgresQuery(ctx, workDir, params, query)
	if err != nil {
		return nil, err
	}
	for _, line := range nonEmptyLines(out) {
		f := strings.Split(line, "|")
		if len(f) == 4 && f[0] == "STATUS" {
			return map[string]any{
				"version": f[1], "role": f[2], "startup_time": f[3],
			}, nil
		}
	}
	return nil, errors.New("unexpected postgres.status output")
}

func postgresReplicationStatus(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	query := "SELECT 'ROLE|'||CASE WHEN pg_is_in_recovery() THEN 'standby' ELSE 'primary' END;" +
		"SELECT 'PRIMARY|'||application_name||'|'||client_addr||'|'||state||'|'||" +
		"COALESCE(sync_state,'')||'|'||COALESCE(write_lag::text,'')||'|'||" +
		"COALESCE(flush_lag::text,'')||'|'||COALESCE(replay_lag::text,'') " +
		"FROM pg_stat_replication ORDER BY application_name;" +
		"SELECT 'STANDBY|'||status||'|'||COALESCE(sender_host,'')||'|'||" +
		"COALESCE(sender_port::text,'')||'|'||COALESCE(latest_end_lsn::text,'')||'|'||" +
		"COALESCE(latest_end_time::text,'') FROM pg_stat_wal_receiver;"
	out, err := runPostgresQuery(ctx, workDir, params, query)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"role": "", "replicas": []map[string]any{}}
	var replicas []map[string]any
	for _, line := range nonEmptyLines(out) {
		f := strings.Split(line, "|")
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "ROLE":
			result["role"] = f[1]
		case "PRIMARY":
			if len(f) >= 8 {
				replicas = append(replicas, map[string]any{
					"application_name": f[1], "client_addr": f[2],
					"state": f[3], "sync_state": f[4],
					"write_lag": f[5], "flush_lag": f[6], "replay_lag": f[7],
				})
			}
		case "STANDBY":
			if len(f) >= 6 {
				result["receiver"] = map[string]any{
					"status": f[1], "sender_host": f[2], "sender_port": f[3],
					"latest_end_lsn": f[4], "latest_end_time": f[5],
				}
			}
		}
	}
	result["replicas"] = replicas
	if result["role"] == "" {
		return nil, errors.New("PostgreSQL replication role query returned no role")
	}
	return result, nil
}

func postgresBackup(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	cfg, cleanup, err := postgresClientConfig(workDir, params)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	binDir, _ := params["bin_dir"].(string)
	outputDir, _ := params["output_dir"].(string)
	fileName, _ := params["file_name"].(string)
	database, _ := params["database"].(string)
	if outputDir == "" || !filepath.IsAbs(outputDir) || filepath.Clean(outputDir) == "/" {
		return nil, errors.New("output_dir must be an absolute non-root path")
	}
	if strings.ContainsAny(outputDir, "'\"\r\n\x00") {
		return nil, errors.New("output_dir contains forbidden characters")
	}
	if !validPostgresIdentifier(database) {
		return nil, errors.New("invalid PostgreSQL database name")
	}
	if fileName == "" {
		fileName = fmt.Sprintf("%s-%s.dump", database, time.Now().UTC().Format("20060102T150405Z"))
	}
	if filepath.Base(fileName) != fileName || strings.ContainsAny(fileName, "/\\\r\n\x00") {
		return nil, errors.New("invalid backup file_name")
	}
	if !strings.HasSuffix(fileName, ".dump") {
		fileName += ".dump"
	}
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return nil, err
	}
	outputPath := filepath.Join(outputDir, fileName)

	pgDump := filepath.Join(binDir, "pg_dump")
	info, err := os.Stat(pgDump)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return nil, errors.New("pg_dump is not executable")
	}

	host, _ := params["host"].(string)
	port, err := intParamDefault(params, "port", 5432)
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("invalid PostgreSQL port")
	}
	username, _ := params["username"].(string)

	cmd := exec.CommandContext(ctx, pgDump,
		"--host", host,
		"--port", strconv.Itoa(port),
		"--username", username,
		"--format", "custom",
		"--file", outputPath,
		database,
	)
	cmd.Env = append(os.Environ(), "PGPASSFILE="+cfg)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		_ = os.Remove(outputPath)
		return nil, fmt.Errorf("pg_dump failed: %w: %s", err, trimOutput(out.String(), 16384))
	}

	sum, size, err := fileSHA256(outputPath)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"engine": "pg_dump", "backup_type": "logical",
		"path": outputPath, "size_bytes": size, "sha256": sum,
		"completed_at": time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func runPostgresQuery(ctx context.Context, workDir string, params map[string]any, query string) (string, error) {
	cfg, cleanup, err := postgresClientConfig(workDir, params)
	if err != nil {
		return "", err
	}
	defer cleanup()

	binDir, _ := params["bin_dir"].(string)
	psql := filepath.Join(binDir, "psql")
	info, err := os.Stat(psql)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("psql is not executable")
	}
	host, _ := params["host"].(string)
	port, err := intParamDefault(params, "port", 5432)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("invalid PostgreSQL port")
	}
	username, _ := params["username"].(string)
	database, _ := params["database"].(string)

	cmd := exec.CommandContext(ctx, psql,
		"--host", host,
		"--port", strconv.Itoa(port),
		"--username", username,
		"--dbname", database,
		"--no-psqlrc",
		"--tuples-only",
		"--no-align",
		"--set", "ON_ERROR_STOP=1",
		"--command", query,
	)
	cmd.Env = append(os.Environ(), "PGPASSFILE="+cfg)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("psql failed: %w: %s", err, trimOutput(out.String(), 16384))
	}
	return strings.TrimSpace(out.String()), nil
}

func postgresClientConfig(workDir string, params map[string]any) (string, func(), error) {
	binDir, _ := params["bin_dir"].(string)
	host, _ := params["host"].(string)
	username, _ := params["username"].(string)
	password, _ := params["password"].(string)
	database, _ := params["database"].(string)
	port, err := intParamDefault(params, "port", 5432)
	if err != nil || port < 1 || port > 65535 {
		return "", nil, errors.New("invalid PostgreSQL port")
	}
	if binDir == "" || !filepath.IsAbs(binDir) || filepath.Clean(binDir) == "/" {
		return "", nil, errors.New("bin_dir must be an absolute non-root path")
	}
	if !safePostgresHost(host) || !validPostgresIdentifier(username) || !validPostgresIdentifier(database) {
		return "", nil, errors.New("invalid PostgreSQL connection parameters")
	}
	if password == "" || strings.ContainsAny(password, "\r\n\x00") {
		return "", nil, errors.New("invalid PostgreSQL password")
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", nil, err
	}
	f, err := os.CreateTemp(workDir, "postgres-pgpass-*")
	if err != nil {
		return "", nil, err
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		cleanup()
		return "", nil, err
	}
	escape := func(v string) string {
		v = strings.ReplaceAll(v, "\\", "\\\\")
		return strings.ReplaceAll(v, ":", "\\:")
	}
	line := fmt.Sprintf("%s:%d:%s:%s:%s\n", escape(host), port, escape(database), escape(username), escape(password))
	if _, err := io.WriteString(f, line); err != nil {
		f.Close()
		cleanup()
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

func validPostgresIdentifier(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func safePostgresHost(v string) bool {
	if v == "" || len(v) > 255 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == ':' {
			continue
		}
		return false
	}
	return true
}
