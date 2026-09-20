package agentclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func mysqlReplicationPrecheck(ctx context.Context, params map[string]any) (map[string]any, error) {
	baseDir, runDir, password, err := mysqlRuntimeParams(params)
	if err != nil {
		return nil, err
	}
	query := "SELECT @@version,@@server_uuid,@@server_id,@@gtid_mode,@@log_bin,@@binlog_format,@@read_only,@@super_read_only;"
	out, err := runMySQLQuery(ctx, baseDir, runDir, password, query, false)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSpace(out), "	")
	if len(fields) < 8 {
		return nil, fmt.Errorf("unexpected MySQL precheck output")
	}
	serverID, _ := strconv.ParseInt(fields[2], 10, 64)
	logBin := fields[4] == "1" || strings.EqualFold(fields[4], "ON")
	ok := strings.EqualFold(fields[3], "ON") && logBin && strings.EqualFold(fields[5], "ROW") && serverID > 0
	return map[string]any{
		"ok":              ok,
		"version":         fields[0],
		"server_uuid":     fields[1],
		"server_id":       serverID,
		"gtid_mode":       fields[3],
		"log_bin":         logBin,
		"binlog_format":   fields[5],
		"read_only":       fields[6],
		"super_read_only": fields[7],
	}, nil
}

func mysqlReplicationCreate(ctx context.Context, params map[string]any) (map[string]any, error) {
	baseDir, runDir, rootPassword, err := mysqlRuntimeParams(params)
	if err != nil {
		return nil, err
	}
	mode, _ := params["mode"].(string)
	replUser, _ := params["replication_user"].(string)
	replPassword, _ := params["replication_password"].(string)
	if !validSQLIdentifier(replUser) || replPassword == "" || !safeSQLSecret(replPassword) {
		return nil, errors.New("invalid replication credentials")
	}

	switch mode {
	case "primary_prepare":
		host, _ := params["replication_host"].(string)
		if !safeSQLHost(host) {
			return nil, errors.New("invalid replication host")
		}
		sqlText := fmt.Sprintf(
			"CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'; ALTER USER '%s'@'%s' IDENTIFIED BY '%s'; GRANT REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO '%s'@'%s';",
			replUser, host, replPassword, replUser, host, replPassword, replUser, host,
		)
		if _, err := runMySQLQuery(ctx, baseDir, runDir, rootPassword, sqlText, true); err != nil {
			return nil, err
		}
		return map[string]any{"role": "primary", "replication_user": replUser, "host": host, "prepared": true}, nil

	case "replica_configure":
		sourceHost, _ := params["source_host"].(string)
		sourcePort, err := intParam(params, "source_port")
		if err != nil || sourcePort < 1 || sourcePort > 65535 || !safeSQLHost(sourceHost) {
			return nil, errors.New("invalid source host or port")
		}
		sqlText := fmt.Sprintf(
			"STOP REPLICA; RESET REPLICA ALL; CHANGE REPLICATION SOURCE TO SOURCE_HOST='%s', SOURCE_PORT=%d, SOURCE_USER='%s', SOURCE_PASSWORD='%s', SOURCE_AUTO_POSITION=1, GET_SOURCE_PUBLIC_KEY=1; START REPLICA;",
			sourceHost, sourcePort, replUser, replPassword,
		)
		if _, err := runMySQLQuery(ctx, baseDir, runDir, rootPassword, sqlText, true); err != nil {
			return nil, err
		}
		return map[string]any{"role": "replica", "source_host": sourceHost, "source_port": sourcePort, "configured": true}, nil
	default:
		return nil, fmt.Errorf("unsupported replication create mode %q", mode)
	}
}

func mysqlReplicationStatus(ctx context.Context, params map[string]any) (map[string]any, error) {
	baseDir, runDir, password, err := mysqlRuntimeParams(params)
	if err != nil {
		return nil, err
	}
	out, err := runMySQLQuery(ctx, baseDir, runDir, password, "SHOW REPLICA STATUS\\G", false)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		values[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	if len(values) == 0 {
		return map[string]any{"configured": false, "status": "not_configured"}, nil
	}
	var lag any
	if v := values["Seconds_Behind_Source"]; v != "" && !strings.EqualFold(v, "NULL") {
		parsed, parseErr := strconv.ParseInt(v, 10, 64)
		if parseErr == nil {
			lag = parsed
		}
	}
	ioRunning := values["Replica_IO_Running"]
	sqlRunning := values["Replica_SQL_Running"]
	status := "degraded"
	if strings.EqualFold(ioRunning, "Yes") && strings.EqualFold(sqlRunning, "Yes") {
		status = "healthy"
	}
	return map[string]any{
		"configured":              true,
		"status":                  status,
		"io_thread_status":        ioRunning,
		"sql_thread_status":       sqlRunning,
		"replication_lag_seconds": lag,
		"source_uuid":             values["Source_UUID"],
		"source_host":             values["Source_Host"],
		"last_io_error":           values["Last_IO_Error"],
		"last_sql_error":          values["Last_SQL_Error"],
		"retrieved_gtid_set":      values["Retrieved_Gtid_Set"],
		"executed_gtid_set":       values["Executed_Gtid_Set"],
	}, nil
}

func mysqlRuntimeParams(params map[string]any) (baseDir, runDir, password string, err error) {
	baseDir, _ = params["base_dir"].(string)
	runDir, _ = params["run_dir"].(string)
	password, _ = params["root_password"].(string)
	if !filepath.IsAbs(baseDir) || !filepath.IsAbs(runDir) || password == "" {
		err = errors.New("base_dir, run_dir and root_password are required")
	}
	return
}

func runMySQLQuery(ctx context.Context, baseDir, runDir, password, query string, stdinMode bool) (string, error) {
	work, err := os.MkdirTemp("", "dbops-mysql-client-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	cfg := filepath.Join(work, "client.cnf")
	content := "[client]\nuser=root\npassword=" + mysqlOption(password) + "\nsocket=" + runDir + "/mysql.sock\n"
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		return "", err
	}
	mysqlBin := filepath.Join(baseDir, "bin", "mysql")
	args := []string{"--defaults-extra-file=" + cfg, "--batch", "--skip-column-names"}
	if strings.Contains(query, "SHOW REPLICA STATUS") {
		args = []string{"--defaults-extra-file=" + cfg, "--batch", "--vertical"}
	}
	cmd := exec.CommandContext(ctx, mysqlBin, args...)
	if stdinMode {
		cmd.Stdin = strings.NewReader(query + "\n")
	} else {
		cmd.Args = append(cmd.Args, "-e", query)
	}
	var out bytes.Buffer
	lw := &limitedBuffer{buf: &out, limit: 128 << 10}
	cmd.Stdout = lw
	cmd.Stderr = lw
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("mysql controlled query failed: %w: %s", err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

func validSQLIdentifier(v string) bool {
	if v == "" || len(v) > 32 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func safeSQLSecret(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	return !strings.ContainsAny(v, "'\\\r\n\x00")
}

func safeSQLHost(v string) bool {
	if v == "" || len(v) > 255 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == ':' || r == '%' {
			continue
		}
		return false
	}
	return true
}
