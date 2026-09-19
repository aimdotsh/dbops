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
	"syscall"
)

func oracleStatus(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	out, err := runOracleSQL(ctx, workDir, params, `
SELECT i.version||'|'||d.open_mode||'|'||d.database_role||'|'||
       TO_CHAR(i.startup_time,'YYYY-MM-DD HH24:MI:SS')
FROM v$instance i CROSS JOIN v$database d;`)
	if err != nil {
		return nil, err
	}
	fields := splitOracleLine(out, 4)
	if len(fields) != 4 {
		return nil, fmt.Errorf("unexpected oracle.status output")
	}
	return map[string]any{
		"version": fields[0], "open_mode": fields[1],
		"database_role": fields[2], "startup_time": fields[3],
	}, nil
}

func oracleTablespaceList(ctx context.Context, workDir string, params map[string]any) ([]map[string]any, error) {
	out, err := runOracleSQL(ctx, workDir, params, `
SELECT t.tablespace_name||'|'||t.status||'|'||t.contents||'|'||
       NVL(df.bytes,0)||'|'||NVL(fs.free_bytes,0)||'|'||NVL(df.maxbytes,0)
FROM dba_tablespaces t
LEFT JOIN (
  SELECT tablespace_name,SUM(bytes) bytes,SUM(CASE WHEN maxbytes=0 THEN bytes ELSE maxbytes END) maxbytes
  FROM dba_data_files GROUP BY tablespace_name
) df ON df.tablespace_name=t.tablespace_name
LEFT JOIN (
  SELECT tablespace_name,SUM(bytes) free_bytes
  FROM dba_free_space GROUP BY tablespace_name
) fs ON fs.tablespace_name=t.tablespace_name
ORDER BY t.tablespace_name;`)
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	for _, line := range nonEmptyLines(out) {
		f := strings.Split(line, "|")
		if len(f) != 6 {
			continue
		}
		total := parseInt64(f[3])
		free := parseInt64(f[4])
		maxBytes := parseInt64(f[5])
		used := total - free
		pct := float64(0)
		if total > 0 {
			pct = float64(used) * 100 / float64(total)
		}
		items = append(items, map[string]any{
			"tablespace": f[0], "status": f[1], "contents": f[2],
			"total_bytes": total, "used_bytes": used, "free_bytes": free,
			"max_bytes": maxBytes, "used_pct": pct,
		})
	}
	return items, nil
}

func oracleDatafileList(ctx context.Context, workDir string, params map[string]any) ([]map[string]any, error) {
	tablespace, _ := params["tablespace"].(string)
	if !validOracleIdentifier(tablespace) {
		return nil, errors.New("invalid tablespace")
	}
	query := fmt.Sprintf(`
SELECT file_id||'|'||file_name||'|'||bytes||'|'||autoextensible||'|'||maxbytes||'|'||increment_by
FROM dba_data_files
WHERE tablespace_name=UPPER('%s')
ORDER BY file_id;`, tablespace)
	out, err := runOracleSQL(ctx, workDir, params, query)
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	for _, line := range nonEmptyLines(out) {
		f := strings.Split(line, "|")
		if len(f) != 6 {
			continue
		}
		items = append(items, map[string]any{
			"file_id": parseInt64(f[0]), "file_name": f[1],
			"bytes": parseInt64(f[2]), "autoextensible": strings.EqualFold(f[3], "YES"),
			"max_bytes": parseInt64(f[4]), "increment_by_blocks": parseInt64(f[5]),
		})
	}
	return items, nil
}

func oracleDatafileAdd(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	tablespace, _ := params["tablespace"].(string)
	path, _ := params["file_path"].(string)
	if !validOracleIdentifier(tablespace) {
		return nil, errors.New("invalid tablespace")
	}
	if err := validateOracleDatafilePath(path); err != nil {
		return nil, err
	}
	sizeMB, err := intParam(params, "size_mb")
	if err != nil || sizeMB < 16 {
		return nil, errors.New("size_mb must be at least 16")
	}
	autoextend, _ := params["autoextend"].(bool)
	nextMB, _ := intParamDefault(params, "next_mb", 64)
	maxMB, _ := intParamDefault(params, "max_mb", 0)
	if autoextend && (nextMB < 1 || (maxMB > 0 && maxMB < sizeMB)) {
		return nil, errors.New("invalid autoextend next/max size")
	}

	exists, err := oracleScalarInt(ctx, workDir, params,
		fmt.Sprintf("SELECT COUNT(*) FROM dba_tablespaces WHERE tablespace_name=UPPER('%s');", tablespace))
	if err != nil {
		return nil, err
	}
	if exists != 1 {
		return nil, errors.New("tablespace does not exist")
	}
	dup, err := oracleScalarInt(ctx, workDir, params,
		fmt.Sprintf("SELECT COUNT(*) FROM dba_data_files WHERE file_name='%s';", oracleLiteral(path)))
	if err != nil {
		return nil, err
	}
	if dup != 0 {
		return nil, errors.New("datafile already exists in Oracle metadata")
	}
	free, err := oraclePathFreeBytes(path)
	if err != nil {
		return nil, err
	}
	required := int64(sizeMB) * 1024 * 1024
	if free < required {
		return nil, fmt.Errorf("insufficient filesystem space: free=%d required=%d", free, required)
	}

	clause := "AUTOEXTEND OFF"
	if autoextend {
		clause = fmt.Sprintf("AUTOEXTEND ON NEXT %dM", nextMB)
		if maxMB > 0 {
			clause += fmt.Sprintf(" MAXSIZE %dM", maxMB)
		} else {
			clause += " MAXSIZE UNLIMITED"
		}
	}
	sqlText := fmt.Sprintf(
		"ALTER TABLESPACE \"%s\" ADD DATAFILE '%s' SIZE %dM %s;",
		tablespace, oracleLiteral(path), sizeMB, clause,
	)
	if _, err := runOracleSQL(ctx, workDir, params, sqlText); err != nil {
		return nil, err
	}
	bytes, err := oracleScalarInt(ctx, workDir, params,
		fmt.Sprintf("SELECT bytes FROM dba_data_files WHERE file_name='%s';", oracleLiteral(path)))
	if err != nil {
		return nil, err
	}
	if bytes < required {
		return nil, fmt.Errorf("datafile verification failed: bytes=%d expected>=%d", bytes, required)
	}
	return map[string]any{
		"verified": true, "tablespace": strings.ToUpper(tablespace), "file_path": path,
		"size_bytes": bytes, "autoextend": autoextend,
	}, nil
}

func oracleDatafileResize(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	path, _ := params["file_path"].(string)
	if err := validateOracleDatafilePath(path); err != nil {
		return nil, err
	}
	targetMB, err := intParam(params, "target_size_mb")
	if err != nil || targetMB < 16 {
		return nil, errors.New("target_size_mb must be at least 16")
	}
	current, err := oracleScalarInt(ctx, workDir, params,
		fmt.Sprintf("SELECT bytes FROM dba_data_files WHERE file_name='%s';", oracleLiteral(path)))
	if err != nil {
		return nil, err
	}
	if current <= 0 {
		return nil, errors.New("datafile not found")
	}
	target := int64(targetMB) * 1024 * 1024
	if target <= current {
		return nil, fmt.Errorf("V1 only permits datafile growth: current=%d target=%d", current, target)
	}
	free, err := oraclePathFreeBytes(path)
	if err != nil {
		return nil, err
	}
	delta := target - current
	if free < delta {
		return nil, fmt.Errorf("insufficient filesystem space: free=%d growth=%d", free, delta)
	}
	if _, err := runOracleSQL(ctx, workDir, params,
		fmt.Sprintf("ALTER DATABASE DATAFILE '%s' RESIZE %dM;", oracleLiteral(path), targetMB)); err != nil {
		return nil, err
	}
	after, err := oracleScalarInt(ctx, workDir, params,
		fmt.Sprintf("SELECT bytes FROM dba_data_files WHERE file_name='%s';", oracleLiteral(path)))
	if err != nil {
		return nil, err
	}
	if after < target {
		return nil, fmt.Errorf("resize verification failed: bytes=%d target=%d", after, target)
	}
	return map[string]any{
		"verified": true, "file_path": path,
		"before_bytes": current, "after_bytes": after,
	}, nil
}

func runOracleSQL(ctx context.Context, workDir string, params map[string]any, sqlText string) (string, error) {
	home, sid, username, password, err := oracleRuntimeParams(params)
	if err != nil {
		return "", err
	}
	sqlplus := filepath.Join(home, "bin", "sqlplus")
	info, err := os.Stat(sqlplus)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("sqlplus is not executable")
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(workDir, "oracle-sqlplus-*.sql")
	if err != nil {
		return "", err
	}
	path := f.Name()
	defer os.Remove(path)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return "", err
	}
	if strings.ContainsAny(password, "\"\r\n\x00") {
		f.Close()
		return "", errors.New("Oracle password contains unsupported control or quote characters")
	}
	script := "whenever sqlerror exit sql.sqlcode\n" +
		"set pagesize 0 feedback off heading off echo off verify off trimspool on linesize 32767\n" +
		fmt.Sprintf("connect %s/\"%s\"\n", username, password) +
		sqlText + "\nexit\n"
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, sqlplus, "-s", "/nolog", "@"+path)
	cmd.Env = append(os.Environ(),
		"ORACLE_HOME="+home,
		"ORACLE_SID="+sid,
		"PATH="+filepath.Join(home, "bin")+":"+os.Getenv("PATH"),
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("sqlplus failed: %w: %s", err, trimOutput(out.String(), 8192))
	}
	return strings.TrimSpace(out.String()), nil
}

func oracleRuntimeParams(params map[string]any) (home, sid, username, password string, err error) {
	home, _ = params["oracle_home"].(string)
	sid, _ = params["oracle_sid"].(string)
	username, _ = params["username"].(string)
	password, _ = params["password"].(string)
	if home == "" || !filepath.IsAbs(home) || filepath.Clean(home) == "/" {
		return "", "", "", "", errors.New("oracle_home must be an absolute non-root path")
	}
	if !validOracleIdentifier(sid) || !validOracleIdentifier(username) {
		return "", "", "", "", errors.New("invalid oracle_sid or username")
	}
	if password == "" {
		return "", "", "", "", errors.New("Oracle password is required")
	}
	return home, sid, username, password, nil
}

func oracleScalarInt(ctx context.Context, workDir string, params map[string]any, query string) (int64, error) {
	out, err := runOracleSQL(ctx, workDir, params, query)
	if err != nil {
		return 0, err
	}
	lines := nonEmptyLines(out)
	if len(lines) == 0 {
		return 0, errors.New("empty Oracle scalar result")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(lines[len(lines)-1]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid Oracle scalar result %q", lines[len(lines)-1])
	}
	return n, nil
}

func oraclePathFreeBytes(filePath string) (int64, error) {
	dir := filepath.Dir(filePath)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return 0, errors.New("datafile directory does not exist")
	}
	probe, err := os.CreateTemp(dir, ".dbops-oracle-write-*")
	if err != nil {
		return 0, errors.New("datafile directory is not writable by Agent execution context")
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

func validateOracleDatafilePath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) == "/" {
		return errors.New("file_path must be an absolute non-root path")
	}
	if strings.ContainsAny(path, "'\"\r\n\x00") {
		return errors.New("file_path contains forbidden characters")
	}
	return nil
}

func validOracleIdentifier(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '$' || r == '#' {
			continue
		}
		return false
	}
	return true
}

func oracleLiteral(v string) string { return strings.ReplaceAll(v, "'", "''") }

func splitOracleLine(v string, min int) []string {
	lines := nonEmptyLines(v)
	if len(lines) == 0 {
		return nil
	}
	f := strings.Split(lines[len(lines)-1], "|")
	if len(f) < min {
		return nil
	}
	return f
}

func nonEmptyLines(v string) []string {
	var out []string
	for _, line := range strings.Split(v, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func parseInt64(v string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n
}
