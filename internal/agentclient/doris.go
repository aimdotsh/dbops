package agentclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func dorisClusterStatus(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	version, err := runDorisSQL(ctx, workDir, params, "SELECT VERSION()", false)
	if err != nil {
		return nil, err
	}
	frontends, err := runDorisTable(ctx, workDir, params, "SHOW FRONTENDS")
	if err != nil {
		return nil, err
	}
	backends, err := runDorisTable(ctx, workDir, params, "SHOW BACKENDS")
	if err != nil {
		return nil, err
	}
	tabletHealth, _ := runDorisTable(ctx, workDir, params, "SHOW PROC '/cluster_health/tablet_health'")
	loadJobs, _ := runDorisTable(ctx, workDir, params, "SHOW LOAD")

	return map[string]any{
		"version": strings.TrimSpace(version),
		"frontends": frontends,
		"backends": backends,
		"tablet_health": tabletHealth,
		"load_jobs": loadJobs,
		"fe_total": len(frontends),
		"fe_alive": countAlive(frontends),
		"be_total": len(backends),
		"be_alive": countAlive(backends),
	}, nil
}

func dorisBackup(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	database, _ := params["database"].(string)
	repository, _ := params["repository"].(string)
	label, _ := params["label"].(string)
	if !validDorisIdentifier(database) || !validDorisIdentifier(repository) || !validDorisIdentifier(label) {
		return nil, errors.New("invalid Doris database, repository or label")
	}
	tables := stringSliceParam(params["tables"])
	for _, table := range tables {
		if !validDorisIdentifier(table) {
			return nil, fmt.Errorf("invalid Doris table %q", table)
		}
	}

	var target string
	if len(tables) > 0 {
		quoted := make([]string, 0, len(tables))
		for _, table := range tables {
			quoted = append(quoted, "`"+table+"`")
		}
		target = " ON (" + strings.Join(quoted, ",") + ")"
	}
	sqlText := fmt.Sprintf("BACKUP SNAPSHOT `%s`.`%s` TO `%s`%s PROPERTIES(\"type\"=\"full\")",
		database, label, repository, target)
	if _, err := runDorisSQL(ctx, workDir, params, sqlText, false); err != nil {
		return nil, fmt.Errorf("submit Doris backup: %w", err)
	}

	pollSeconds, err := intParamDefault(params, "poll_seconds", 2)
	if err != nil || pollSeconds < 1 || pollSeconds > 60 {
		return nil, errors.New("invalid poll_seconds")
	}
	timeoutSeconds, err := intParamDefault(params, "job_timeout_seconds", 3600)
	if err != nil || timeoutSeconds < 1 || timeoutSeconds > 86400 {
		return nil, errors.New("invalid job_timeout_seconds")
	}

	deadline := time.NewTimer(time.Duration(timeoutSeconds) * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Duration(pollSeconds) * time.Second)
	defer ticker.Stop()

	var last []map[string]string
	for {
		rows, err := runDorisTable(ctx, workDir, params, fmt.Sprintf("SHOW BACKUP FROM `%s`", database))
		if err != nil {
			return nil, err
		}
		last = filterDorisBackup(rows, label)
		if len(last) > 0 {
			state := dorisField(last[0], "State", "state")
			switch strings.ToUpper(state) {
			case "FINISHED":
				return map[string]any{
					"engine": "doris_snapshot",
					"backup_type": "snapshot",
					"database": database,
					"repository": repository,
					"label": label,
					"state": "FINISHED",
					"job": last[0],
					"completed_at": time.Now().UTC().Format(time.RFC3339),
				}, nil
			case "CANCELLED":
				msg := dorisField(last[0], "Status", "status")
				if msg == "" {
					msg = "Doris backup job cancelled"
				}
				return nil, errors.New(msg)
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("Doris backup timed out; last=%v", last)
		case <-ticker.C:
		}
	}
}

func runDorisTable(ctx context.Context, workDir string, params map[string]any, sqlText string) ([]map[string]string, error) {
	out, err := runDorisSQL(ctx, workDir, params, sqlText, true)
	if err != nil {
		return nil, err
	}
	return parseTabularWithHeader(out), nil
}

func runDorisSQL(ctx context.Context, workDir string, params map[string]any, sqlText string, header bool) (string, error) {
	clientPath, host, port, username, password, database, err := dorisRuntimeParams(params)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(workDir, "doris-client-*.cnf")
	if err != nil {
		return "", err
	}
	secretPath := f.Name()
	defer os.Remove(secretPath)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return "", err
	}
	password = strings.ReplaceAll(password, "\n", "")
	if _, err := fmt.Fprintf(f, "[client]\nuser=%s\npassword=%s\nhost=%s\nport=%d\n", username, password, host, port); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	args := []string{"--defaults-extra-file=" + secretPath, "--batch", "--raw"}
	if !header {
		args = append(args, "--skip-column-names")
	}
	if database != "" {
		args = append(args, "--database", database)
	}
	args = append(args, "--execute", sqlText)

	cmd := exec.CommandContext(ctx, clientPath, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("Doris mysql client failed: %w: %s", err, trimOutput(out.String(), 16384))
	}
	return strings.TrimSpace(out.String()), nil
}

func dorisRuntimeParams(params map[string]any) (clientPath, host string, port int, username, password, database string, err error) {
	clientPath, _ = params["mysql_client"].(string)
	host, _ = params["host"].(string)
	username, _ = params["username"].(string)
	password, _ = params["password"].(string)
	database, _ = params["database"].(string)
	port, err = intParamDefault(params, "query_port", 9030)
	if err != nil || port < 1 || port > 65535 {
		return "", "", 0, "", "", "", errors.New("invalid Doris query_port")
	}
	if clientPath == "" || !filepath.IsAbs(clientPath) || filepath.Clean(clientPath) == "/" {
		return "", "", 0, "", "", "", errors.New("mysql_client must be an absolute path")
	}
	info, statErr := os.Stat(clientPath)
	if statErr != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return "", "", 0, "", "", "", errors.New("mysql_client is not executable")
	}
	if !safeDorisHost(host) || !validDorisIdentifier(username) {
		return "", "", 0, "", "", "", errors.New("invalid Doris host or username")
	}
	if database != "" && !validDorisIdentifier(database) {
		return "", "", 0, "", "", "", errors.New("invalid Doris database")
	}
	if password == "" || strings.ContainsAny(password, "\r\n\x00") {
		return "", "", 0, "", "", "", errors.New("invalid Doris password")
	}
	return clientPath, host, port, username, password, database, nil
}

func parseTabularWithHeader(raw string) []map[string]string {
	lines := nonEmptyLines(raw)
	if len(lines) < 2 {
		return nil
	}
	headers := strings.Split(lines[0], "\t")
	out := make([]map[string]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		values := strings.Split(line, "\t")
		row := make(map[string]string, len(headers))
		for i, h := range headers {
			if i < len(values) {
				row[h] = values[i]
			} else {
				row[h] = ""
			}
		}
		out = append(out, row)
	}
	return out
}

func countAlive(rows []map[string]string) int {
	count := 0
	for _, row := range rows {
		v := dorisField(row, "Alive", "alive", "IsAlive", "isAlive")
		if strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") || v == "1" {
			count++
		}
	}
	return count
}

func filterDorisBackup(rows []map[string]string, label string) []map[string]string {
	var out []map[string]string
	for _, row := range rows {
		v := dorisField(row, "SnapshotName", "snapshotName", "Snapshot", "Label")
		if v == label {
			out = append(out, row)
		}
	}
	return out
}

func dorisField(row map[string]string, names ...string) string {
	for _, name := range names {
		if v, ok := row[name]; ok {
			return v
		}
	}
	return ""
}

func validDorisIdentifier(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func safeDorisHost(v string) bool {
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
