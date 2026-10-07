package agentclient

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const archiveInspectLimit = 50000

// mysqlArchiveInspect is read-only. The bounded key digest makes a retry
// conditional on the same set of matching primary keys still existing across
// source and destination. Larger or unsupported tables require manual review.
func mysqlArchiveInspect(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	baseDir, runDir, password, err := mysqlRuntimeParams(params)
	if err != nil {
		return nil, err
	}
	sourceDB, sourceTable, destDB, destTable, where, _, err := archiveParams(params)
	if err != nil {
		return nil, err
	}
	destRunDir, destPassword := runDir, password
	if v, _ := params["destination_password"].(string); v != "" {
		destPassword = v
		destRunDir, _ = params["destination_run_dir"].(string)
		if destRunDir == "" {
			return nil, errors.New("destination_run_dir is required")
		}
	}
	sourceCount, err := archiveMatchingCount(ctx, baseDir, runDir, password, sourceDB, sourceTable, where)
	if err != nil {
		return nil, err
	}
	destCount, err := archiveMatchingCount(ctx, baseDir, destRunDir, destPassword, destDB, destTable, where)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"source_matching": sourceCount, "destination_matching": destCount, "bounded": false, "overlap": int64(-1), "process_running": false, "process_check_ok": false}
	if jobID, err := intParamDefault(params, "archive_job_id", 0); err == nil && jobID > 0 {
		active, checked := archiveProcessActive(workDir, jobID)
		result["process_running"], result["process_check_ok"] = active, checked
	} else {
		result["process_check_ok"] = true // baseline inspection precedes process start
	}
	if sourceCount+destCount > archiveInspectLimit {
		result["reason"] = "matching row count exceeds bounded verification limit"
		return result, nil
	}
	sourcePK, sourceType, err := archiveIntegerPK(ctx, baseDir, runDir, password, sourceDB, sourceTable)
	if err != nil {
		result["reason"] = err.Error()
		return result, nil
	}
	destPK, destType, err := archiveIntegerPK(ctx, baseDir, destRunDir, destPassword, destDB, destTable)
	if err != nil {
		result["reason"] = err.Error()
		return result, nil
	}
	if sourcePK != destPK || sourceType != destType {
		result["reason"] = "source and destination primary keys differ"
		return result, nil
	}
	sourceKeys, err := archiveMatchingKeys(ctx, workDir, baseDir, runDir, password, sourceDB, sourceTable, sourcePK, where)
	if err != nil {
		return nil, err
	}
	destKeys, err := archiveMatchingKeys(ctx, workDir, baseDir, destRunDir, destPassword, destDB, destTable, destPK, where)
	if err != nil {
		return nil, err
	}
	if int64(len(sourceKeys)) != sourceCount || int64(len(destKeys)) != destCount {
		return nil, errors.New("archive table changed during inspection; retry inspection")
	}
	set := make(map[string]struct{}, len(sourceKeys)+len(destKeys))
	for _, key := range sourceKeys {
		set[key] = struct{}{}
	}
	var overlap int64
	for _, key := range destKeys {
		if _, found := set[key]; found {
			overlap++
		}
		set[key] = struct{}{}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, key := range keys {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(key)))
		_, _ = h.Write(n[:])
		_, _ = h.Write([]byte(key))
	}
	result["bounded"], result["overlap"], result["union_key_digest"] = true, overlap, hex.EncodeToString(h.Sum(nil))
	result["primary_key"] = sourcePK
	return result, nil
}

func archiveMatchingCount(ctx context.Context, baseDir, runDir, password, db, table, where string) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s WHERE (%s)", quoteIdentifier(db), quoteIdentifier(table), where)
	out, err := runMySQLQuery(ctx, baseDir, runDir, password, query, false)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(out), 10, 64)
}

func archiveIntegerPK(ctx context.Context, baseDir, runDir, password, db, table string) (string, string, error) {
	query := fmt.Sprintf("SHOW INDEX FROM %s.%s WHERE Key_name='PRIMARY'", quoteIdentifier(db), quoteIdentifier(table))
	out, err := runMySQLQuery(ctx, baseDir, runDir, password, query, false)
	if err != nil {
		return "", "", err
	}
	rows := nonEmptyLines(out)
	if len(rows) != 1 {
		return "", "", errors.New("bounded retry requires a single-column primary key")
	}
	fields := strings.Split(rows[0], "\t")
	if len(fields) < 5 || !validArchiveIdentifier(fields[4]) {
		return "", "", errors.New("invalid primary key metadata")
	}
	pk := fields[4]
	query = fmt.Sprintf("SHOW COLUMNS FROM %s.%s WHERE Field='%s'", quoteIdentifier(db), quoteIdentifier(table), pk)
	out, err = runMySQLQuery(ctx, baseDir, runDir, password, query, false)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(strings.TrimSpace(out), "\t")
	if len(parts) < 2 {
		return "", "", errors.New("missing primary key column")
	}
	typ := strings.ToLower(parts[1])
	ok := false
	for _, prefix := range []string{"tinyint", "smallint", "mediumint", "int(", "int ", "bigint"} {
		if strings.HasPrefix(typ, prefix) {
			ok = true
			break
		}
	}
	if typ == "int" {
		ok = true
	}
	if !ok {
		return "", "", errors.New("bounded retry requires an integer primary key")
	}
	return pk, typ, nil
}

func archiveMatchingKeys(ctx context.Context, workDir, baseDir, runDir, password, db, table, pk, where string) ([]string, error) {
	defaults, cleanup, err := archiveDefaultsFile(workDir, "inspect", runDir, password)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	query := fmt.Sprintf("SELECT CAST(%s AS CHAR) FROM %s.%s WHERE (%s) ORDER BY %s LIMIT %d", quoteIdentifier(pk), quoteIdentifier(db), quoteIdentifier(table), where, quoteIdentifier(pk), archiveInspectLimit+1)
	cmd := exec.CommandContext(ctx, filepath.Join(baseDir, "bin", "mysql"), "--defaults-extra-file="+defaults, "--batch", "--skip-column-names", "-e", query)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("archive key inspection failed: %w", err)
	}
	keys := nonEmptyLines(string(out))
	if len(keys) > archiveInspectLimit {
		return nil, errors.New("archive inspection exceeds row limit")
	}
	for _, key := range keys {
		if _, err := strconv.ParseUint(strings.TrimPrefix(key, "-"), 10, 64); err != nil {
			return nil, errors.New("invalid integer primary key in archive inspection")
		}
	}
	return keys, nil
}

func archiveProcessActive(workDir string, jobID int) (bool, bool) {
	sentinel, err := archiveSentinelPath(workDir, jobID)
	if err != nil {
		return false, false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, false
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err == nil && strings.Contains(string(cmdline), sentinel) {
			return true, true
		}
	}
	return false, true
}
