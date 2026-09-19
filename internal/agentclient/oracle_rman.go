package agentclient

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func oracleRMANBackup(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	home, sid, username, password, err := oracleRuntimeParams(params)
	if err != nil {
		return nil, err
	}

	backupType, _ := params["backup_type"].(string)
	switch backupType {
	case "full", "level0", "level1", "archivelog":
	default:
		return nil, errors.New("backup_type must be full, level0, level1 or archivelog")
	}

	outputDir, _ := params["output_dir"].(string)
	if outputDir == "" || !filepath.IsAbs(outputDir) || filepath.Clean(outputDir) == "/" {
		return nil, errors.New("output_dir must be an absolute non-root path")
	}
	if strings.ContainsAny(outputDir, "'\"\r\n\x00") {
		return nil, errors.New("output_dir contains forbidden characters")
	}
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return nil, err
	}

	rman := filepath.Join(home, "bin", "rman")
	info, err := os.Stat(rman)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return nil, errors.New("rman is not executable")
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, err
	}

	runID := fmt.Sprintf("%s-%s", strings.ToLower(sid), time.Now().UTC().Format("20060102T150405Z"))
	prefix := "dbops_" + runID + "_"
	dbFormat := filepath.Join(outputDir, prefix+"%T_%U.bkp")
	archFormat := filepath.Join(outputDir, prefix+"arch_%T_%U.bkp")

	f, err := os.CreateTemp(workDir, "oracle-rman-*.rcv")
	if err != nil {
		return nil, err
	}
	scriptPath := f.Name()
	defer os.Remove(scriptPath)

	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, err
	}
	if strings.ContainsAny(password, "\"\r\n\x00") {
		f.Close()
		return nil, errors.New("Oracle password contains unsupported characters")
	}
	if strings.ContainsAny(username, "\"\r\n\x00") {
		f.Close()
		return nil, errors.New("Oracle username contains unsupported characters")
	}

	var script strings.Builder
	fmt.Fprintf(&script, "CONNECT TARGET %s/\"%s\"\n", username, password)
	script.WriteString("RUN {\n")
	switch backupType {
	case "full":
		fmt.Fprintf(&script, "  BACKUP AS COMPRESSED BACKUPSET DATABASE FORMAT '%s';\n", dbFormat)
		fmt.Fprintf(&script, "  BACKUP AS COMPRESSED BACKUPSET ARCHIVELOG ALL NOT BACKED UP 1 TIMES FORMAT '%s';\n", archFormat)
	case "level0":
		fmt.Fprintf(&script, "  BACKUP AS COMPRESSED BACKUPSET INCREMENTAL LEVEL 0 DATABASE FORMAT '%s';\n", dbFormat)
	case "level1":
		fmt.Fprintf(&script, "  BACKUP AS COMPRESSED BACKUPSET INCREMENTAL LEVEL 1 DATABASE FORMAT '%s';\n", dbFormat)
	case "archivelog":
		fmt.Fprintf(&script, "  BACKUP AS COMPRESSED BACKUPSET ARCHIVELOG ALL NOT BACKED UP 1 TIMES FORMAT '%s';\n", archFormat)
	}
	script.WriteString("}\n")
	script.WriteString("EXIT;\n")

	if _, err := f.WriteString(script.String()); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}

	logPath := filepath.Join(outputDir, prefix+"rman.log")
	cmd := exec.CommandContext(ctx, rman, "cmdfile="+scriptPath, "log="+logPath)
	cmd.Env = append(os.Environ(),
		"ORACLE_HOME="+home,
		"ORACLE_SID="+sid,
		"PATH="+filepath.Join(home, "bin")+":"+os.Getenv("PATH"),
	)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("RMAN backup failed: %w: %s", err, tailFile(logPath, 16384))
	}

	files, err := filepath.Glob(filepath.Join(outputDir, prefix+"*.bkp"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, errors.New("RMAN completed but no backup pieces were found")
	}

	outFiles := make([]map[string]any, 0, len(files))
	manifestHash := sha256.New()
	var total int64
	for _, path := range files {
		sum, size, err := fileSHA256WithSize(path)
		if err != nil {
			return nil, err
		}
		total += size
		outFiles = append(outFiles, map[string]any{
			"path": path, "size_bytes": size, "sha256": sum,
		})
		_, _ = io.WriteString(manifestHash, filepath.Base(path)+"|"+sum+"\n")
	}

	return map[string]any{
		"engine":          "rman",
		"backup_type":     backupType,
		"output_dir":      outputDir,
		"size_bytes":      total,
		"piece_count":     len(outFiles),
		"manifest_sha256": hex.EncodeToString(manifestHash.Sum(nil)),
		"files":           outFiles,
		"log_path":        logPath,
		"completed_at":    time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func tailFile(path string, max int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	joined := strings.Join(lines, "\n")
	if len(joined) > max {
		return joined[len(joined)-max:]
	}
	return joined
}
