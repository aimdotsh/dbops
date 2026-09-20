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

func mysqlArchivePrecheck(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	baseDir, runDir, password, err := mysqlRuntimeParams(params)
	if err != nil {
		return nil, err
	}
	sourceDB, sourceTable, destDB, destTable, where, toolPath, err := archiveParams(params)
	if err != nil {
		return nil, err
	}

	pkSQL := fmt.Sprintf("SHOW INDEX FROM %s.%s WHERE Key_name='PRIMARY';",
		quoteIdentifier(sourceDB), quoteIdentifier(sourceTable))
	pkOut, err := runMySQLQuery(ctx, baseDir, runDir, password, pkSQL, false)
	if err != nil {
		return nil, fmt.Errorf("check primary key: %w", err)
	}
	if strings.TrimSpace(pkOut) == "" {
		return map[string]any{"ok": false, "reason": "source table has no primary key"}, nil
	}

	sourceDefaults, sourceCleanup, err := archiveDefaultsFile(workDir, "src", runDir, password)
	if err != nil {
		return nil, err
	}
	defer sourceCleanup()

	destDefaults := sourceDefaults
	destCleanup := func() {}
	if destPassword, _ := params["destination_password"].(string); destPassword != "" {
		destRunDir, _ := params["destination_run_dir"].(string)
		if destRunDir == "" {
			return nil, errors.New("destination_run_dir is required for a separate destination instance")
		}
		destDefaults, destCleanup, err = archiveDefaultsFile(workDir, "dst", destRunDir, destPassword)
		if err != nil {
			return nil, err
		}
	}
	defer destCleanup()

	args := archiveBaseArgs(sourceDefaults, destDefaults, sourceDB, sourceTable, destDB, destTable, where)
	args = append(args, "--dry-run", "--no-delete")
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, toolPath, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pt-archiver dry-run failed: %w: %s", err, trimOutput(out.String(), 8192))
	}

	return map[string]any{
		"ok":                   true,
		"primary_key":          true,
		"source_database":      sourceDB,
		"source_table":         sourceTable,
		"destination_database": destDB,
		"destination_table":    destTable,
		"dry_run_output":       trimOutput(out.String(), 8192),
	}, nil
}

func mysqlArchiveStart(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	baseDir, runDir, password, err := mysqlRuntimeParams(params)
	if err != nil {
		return nil, err
	}
	sourceDB, sourceTable, destDB, destTable, where, toolPath, err := archiveParams(params)
	if err != nil {
		return nil, err
	}
	jobID, err := intParam(params, "archive_job_id")
	if err != nil || jobID <= 0 {
		return nil, errors.New("archive_job_id is required")
	}
	limit, err := intParamDefault(params, "batch_size", 5000)
	if err != nil || limit < 1 || limit > 100000 {
		return nil, errors.New("invalid batch_size")
	}
	txnSize, err := intParamDefault(params, "txn_size", limit)
	if err != nil || txnSize < 1 || txnSize > 100000 {
		return nil, errors.New("invalid txn_size")
	}
	sleepMS, err := intParamDefault(params, "sleep_ms", 200)
	if err != nil || sleepMS < 0 || sleepMS > 60000 {
		return nil, errors.New("invalid sleep_ms")
	}
	deleteSource, _ := params["delete_source"].(bool)

	sourceDefaults, sourceCleanup, err := archiveDefaultsFile(workDir, "src", runDir, password)
	if err != nil {
		return nil, err
	}
	defer sourceCleanup()

	destDefaults := sourceDefaults
	destCleanup := func() {}
	if destPassword, _ := params["destination_password"].(string); destPassword != "" {
		destRunDir, _ := params["destination_run_dir"].(string)
		if destRunDir == "" {
			return nil, errors.New("destination_run_dir is required for a separate destination instance")
		}
		destDefaults, destCleanup, err = archiveDefaultsFile(workDir, "dst", destRunDir, destPassword)
		if err != nil {
			return nil, err
		}
	}
	defer destCleanup()

	sentinel, err := archiveSentinelPath(workDir, jobID)
	if err != nil {
		return nil, err
	}
	_ = os.Remove(sentinel)

	args := archiveBaseArgs(sourceDefaults, destDefaults, sourceDB, sourceTable, destDB, destTable, where)
	// pt-archiver 3.2 accepts integer seconds for --sleep. Round a positive
	// millisecond request upward so throttling never becomes weaker.
	sleepSeconds := (sleepMS + 999) / 1000
	args = append(args,
		"--limit", strconv.Itoa(limit),
		"--txn-size", strconv.Itoa(txnSize),
		"--sleep", strconv.Itoa(sleepSeconds),
		"--statistics",
		"--progress", strconv.Itoa(limit),
		"--sentinel", sentinel,
	)
	maxThreads, err := intParamDefault(params, "max_threads_running", 0)
	if err != nil || maxThreads < 0 {
		return nil, errors.New("invalid max_threads_running")
	}
	if !deleteSource {
		args = append(args, "--no-delete")
	}

	var out bytes.Buffer
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	loadFailures := make(chan error, 1)
	if maxThreads > 0 {
		if err := checkArchiveThreads(ctx, baseDir, runDir, password, maxThreads); err != nil {
			return nil, err
		}
		go watchArchiveThreads(runCtx, cancel, baseDir, runDir, password, maxThreads, loadFailures)
	}
	cmd := exec.CommandContext(runCtx, toolPath, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	cancel()
	select {
	case loadErr := <-loadFailures:
		return nil, loadErr
	default:
	}
	if err != nil {
		return nil, fmt.Errorf("pt-archiver failed: %w: %s", err, trimOutput(out.String(), 16384))
	}

	state := "completed"
	pauseReason := ""
	if b, readErr := os.ReadFile(sentinel); readErr == nil {
		mode := strings.TrimSpace(string(b))
		switch mode {
		case "pause":
			state = "paused"
			pauseReason = "operator requested pause"
		case "stop":
			state = "stopped"
			pauseReason = "operator requested stop"
		}
	}
	scanned, archived, deleted, statsOK := parseArchiveStats(out.String())
	if !statsOK {
		return nil, errors.New("pt-archiver completed without parseable row statistics; inspect data before retry")
	}
	return map[string]any{
		"status":        state,
		"scanned_rows":  scanned,
		"archived_rows": archived,
		"deleted_rows":  deleted,
		"failed_rows":   int64(0),
		"delete_source": deleteSource,
		"statistics":    trimOutput(out.String(), 16384),
		"verification":  "command_completed",
		"pause_reason":  pauseReason,
		"sentinel_path": sentinel,
	}, nil
}

func mysqlArchiveControl(workDir, action string, params map[string]any) (map[string]any, error) {
	jobID, err := intParam(params, "archive_job_id")
	if err != nil || jobID <= 0 {
		return nil, errors.New("archive_job_id is required")
	}
	path, err := archiveSentinelPath(workDir, jobID)
	if err != nil {
		return nil, err
	}
	switch action {
	case "mysql.archive.pause":
		if err := os.WriteFile(path, []byte("pause\n"), 0o600); err != nil {
			return nil, err
		}
		return map[string]any{"archive_job_id": jobID, "state": "pause_requested"}, nil
	case "mysql.archive.stop":
		if err := os.WriteFile(path, []byte("stop\n"), 0o600); err != nil {
			return nil, err
		}
		return map[string]any{"archive_job_id": jobID, "state": "stop_requested"}, nil
	case "mysql.archive.resume":
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		return map[string]any{"archive_job_id": jobID, "state": "resume_ready"}, nil
	default:
		return nil, fmt.Errorf("unsupported archive control %q", action)
	}
}

func archiveSentinelPath(workDir string, jobID int) (string, error) {
	dir := filepath.Join(workDir, "archive-control")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("archive-%d.stop", jobID)), nil
}

func archiveParams(params map[string]any) (sourceDB, sourceTable, destDB, destTable, where, toolPath string, err error) {
	sourceDB, _ = params["source_database"].(string)
	sourceTable, _ = params["source_table"].(string)
	destDB, _ = params["destination_database"].(string)
	destTable, _ = params["destination_table"].(string)
	where, _ = params["where"].(string)
	toolPath, _ = params["pt_archiver_path"].(string)

	for name, value := range map[string]string{
		"source_database": sourceDB,
		"source_table":    sourceTable,
	} {
		if !validSQLIdentifier(value) {
			return "", "", "", "", "", "", fmt.Errorf("invalid %s", name)
		}
	}
	if destDB != "" || destTable != "" {
		if !validSQLIdentifier(destDB) || !validSQLIdentifier(destTable) {
			return "", "", "", "", "", "", errors.New("destination_database and destination_table must both be valid identifiers")
		}
	}
	if !safeArchiveWhere(where) {
		return "", "", "", "", "", "", errors.New("where expression is empty or contains forbidden SQL tokens")
	}
	if toolPath == "" || !filepath.IsAbs(toolPath) || filepath.Clean(toolPath) == "/" {
		return "", "", "", "", "", "", errors.New("pt_archiver_path must be an absolute path")
	}
	info, statErr := os.Stat(toolPath)
	if statErr != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return "", "", "", "", "", "", errors.New("pt_archiver_path is not an executable file")
	}
	return sourceDB, sourceTable, destDB, destTable, where, toolPath, nil
}

func archiveBaseArgs(sourceDefaults, destDefaults, sourceDB, sourceTable, destDB, destTable, where string) []string {
	args := []string{
		"--source", fmt.Sprintf("F=%s,D=%s,t=%s", sourceDefaults, sourceDB, sourceTable),
		"--where", where,
	}
	if destDB != "" && destTable != "" {
		args = append(args, "--dest", fmt.Sprintf("F=%s,D=%s,t=%s", destDefaults, destDB, destTable))
	}
	return args
}

func archiveDefaultsFile(workDir, prefix, runDir, password string) (string, func(), error) {
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", nil, err
	}
	f, err := os.CreateTemp(workDir, prefix+"-pt-archiver-*.cnf")
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
	password = strings.ReplaceAll(password, "\n", "")
	if _, err := fmt.Fprintf(f, "[client]\nuser=root\npassword=%s\nsocket=%s/mysql.sock\n", mysqlOption(password), runDir); err != nil {
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

func safeArchiveWhere(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 2048 {
		return false
	}
	lower := strings.ToLower(v)
	for _, forbidden := range []string{";", "--", "/*", "*/", "\x00", "\n", "\r"} {
		if strings.Contains(lower, forbidden) {
			return false
		}
	}
	return true
}

func quoteIdentifier(v string) string {
	return "`" + strings.ReplaceAll(v, "`", "``") + "`"
}

func trimOutput(v string, max int) string {
	v = strings.TrimSpace(v)
	if len(v) <= max {
		return v
	}
	return v[:max]
}

func parseArchiveStats(output string) (scanned, archived, deleted int64, ok bool) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "DBOPS_ROWS ") {
			ok = true
			for _, part := range strings.Fields(strings.TrimPrefix(line, "DBOPS_ROWS ")) {
				pair := strings.SplitN(part, "=", 2)
				if len(pair) != 2 {
					continue
				}
				n, _ := strconv.ParseInt(pair[1], 10, 64)
				switch pair[0] {
				case "scanned":
					scanned = n
				case "archived":
					archived = n
				case "deleted":
					deleted = n
				}
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		n, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || n < 0 {
			continue
		}
		switch fields[0] {
		case "SELECT":
			scanned = n
			ok = true
		case "INSERT":
			archived = n
			ok = true
		case "DELETE":
			deleted = n
			ok = true
		}
	}
	return scanned, archived, deleted, ok
}
