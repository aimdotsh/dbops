package agentclient

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/aimdotsh/dbops/internal/agentproto"
)

type mysqlInstallParams struct {
	PackageURL     string
	PackageSHA256  string
	PackageType    string
	Version        string
	ExpectedOS     string
	ExpectedArch   string
	Port           int
	ServerID       int
	BaseDir        string
	DataDir        string
	LogDir         string
	BinlogDir      string
	RunDir         string
	ConfigPath     string
	ConfigText     string
	ServiceName    string
	ServiceMode    string
	MySQLUser      string
	ManageOSUser   bool
	RootPassword   string
	MinFreeBytes   uint64
	MinMemoryBytes uint64
	MinCPUCores    int
}

type installMarker struct {
	TaskID      int64  `json:"task_id"`
	Status      string `json:"status"`
	Step        string `json:"step"`
	Port        int    `json:"port"`
	DataDir     string `json:"data_dir"`
	BaseDir     string `json:"base_dir"`
	ConfigPath  string `json:"config_path"`
	ServiceName string `json:"service_name"`
	UpdatedAt   string `json:"updated_at"`
	Error       string `json:"error,omitempty"`
}

func mysqlInstall(ctx context.Context, workDir string, req agentproto.ActionRequest, report ProgressReporter, client *http.Client) (result map[string]any, retErr error) {
	p, err := parseMySQLInstallParams(req.Params)
	if err != nil {
		return nil, err
	}
	if p.PackageType != "tar.gz" && p.PackageType != "tgz" {
		return nil, fmt.Errorf("unsupported package type %q", p.PackageType)
	}
	if p.ServiceMode != "systemd" && p.ServiceMode != "process" {
		return nil, fmt.Errorf("unsupported service mode %q", p.ServiceMode)
	}

	taskDir := filepath.Join(workDir, "tasks", strconv.FormatInt(req.TaskID, 10))
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return nil, err
	}
	markerPath := filepath.Join(taskDir, "mysql-install.json")
	bootstrapCreated := false
	if _, err := os.Lstat(markerPath); err == nil {
		return nil, errors.New("installation marker already exists; verify previous execution before retry")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	marker := installMarker{TaskID: req.TaskID, Status: "running", Port: p.Port, DataDir: p.DataDir, BaseDir: p.BaseDir, ConfigPath: p.ConfigPath, ServiceName: p.ServiceName, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := writeInstallMarker(markerPath, marker); err != nil {
		return nil, err
	}
	defer func() {
		if bootstrapCreated {
			_ = os.Remove(filepath.Join(filepath.Dir(p.ConfigPath), "dbops-bootstrap.sql"))
		}
		if retErr != nil {
			marker.Status = "failed"
			marker.Error = retErr.Error()
			marker.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			_ = writeInstallMarker(markerPath, marker)
		}
	}()

	runStep := func(no int, code, name string, fn func() (any, error)) error {
		progressStart := (no-1)*90/18 + 2
		if report != nil {
			report(agentproto.ActionResponse{Status: "running", Progress: progressStart, Step: agentproto.Step{No: no, Code: code, Name: name, Status: "running"}, Message: name + " started"})
		}
		marker.Step = code
		marker.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_ = writeInstallMarker(markerPath, marker)
		out, err := fn()
		if err != nil {
			if report != nil {
				report(agentproto.ActionResponse{Status: "running", Progress: progressStart, Step: agentproto.Step{No: no, Code: code, Name: name, Status: "failed"}, Message: name + " failed", Error: err.Error()})
			}
			return fmt.Errorf("%s: %w", code, err)
		}
		progressEnd := no*90/18 + 5
		if progressEnd > 95 {
			progressEnd = 95
		}
		if report != nil {
			report(agentproto.ActionResponse{Status: "running", Progress: progressEnd, Step: agentproto.Step{No: no, Code: code, Name: name, Status: "success"}, Message: name + " completed", Result: out})
		}
		return nil
	}

	var precheck precheckResult
	if err := runStep(1, "CHECK_AGENT", "Check Agent", func() (any, error) {
		return map[string]any{"agent": "online", "goos": runtime.GOOS, "goarch": runtime.GOARCH}, nil
	}); err != nil {
		return nil, err
	}
	if err := runStep(2, "CHECK_OS_ARCH", "Check OS and architecture", func() (any, error) {
		if p.ExpectedOS != "" && p.ExpectedOS != runtime.GOOS {
			return nil, fmt.Errorf("OS mismatch: expected %s got %s", p.ExpectedOS, runtime.GOOS)
		}
		if p.ExpectedArch != "" && p.ExpectedArch != runtime.GOARCH {
			return nil, fmt.Errorf("architecture mismatch: expected %s got %s", p.ExpectedArch, runtime.GOARCH)
		}
		return map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH}, nil
	}); err != nil {
		return nil, err
	}
	if err := runStep(3, "CHECK_PORT", "Check port", func() (any, error) {
		r, err := portCheck(ctx, map[string]any{"port": p.Port})
		if err != nil {
			return nil, err
		}
		if ok, _ := r["available"].(bool); !ok {
			return nil, fmt.Errorf("port %d is already in use", p.Port)
		}
		return r, nil
	}); err != nil {
		return nil, err
	}
	if err := runStep(4, "CHECK_DATADIR", "Check data directory", func() (any, error) {
		s, err := inspectMySQLDataDir(p.DataDir, p.MinFreeBytes)
		if err != nil {
			return nil, err
		}
		if entries, err := os.ReadDir(p.DataDir); err == nil && len(entries) > 0 {
			return nil, errors.New("data_dir must be empty")
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if s.ValidMySQLData {
			return nil, fmt.Errorf("existing MySQL data detected in %s", p.DataDir)
		}
		return s, nil
	}); err != nil {
		return nil, err
	}
	if err := runStep(5, "CHECK_DISK", "Check capacity and host requirements", func() (any, error) {
		precheck, err = mysqlPrecheck(ctx, map[string]any{"port": p.Port, "data_dir": p.DataDir, "min_free_bytes": p.MinFreeBytes, "min_memory_bytes": p.MinMemoryBytes, "min_cpu_cores": p.MinCPUCores, "expected_os": p.ExpectedOS, "expected_arch": p.ExpectedArch, "service_name": p.ServiceName})
		if err != nil {
			return nil, err
		}
		if !precheck.OK {
			return precheck, errors.New("precheck contains blocking conditions")
		}
		return precheck, nil
	}); err != nil {
		return nil, err
	}
	if err := runStep(6, "ALLOCATE_SERVER_ID", "Validate server_id", func() (any, error) {
		if p.ServerID <= 0 {
			return nil, errors.New("server_id is required")
		}
		return map[string]any{"server_id": p.ServerID}, nil
	}); err != nil {
		return nil, err
	}

	packagePath := filepath.Join(taskDir, "mysql-package.tgz")
	if err := runStep(7, "DOWNLOAD_PACKAGE", "Download package", func() (any, error) {
		n, err := downloadFile(ctx, p.PackageURL, packagePath, client)
		return map[string]any{"bytes": n}, err
	}); err != nil {
		return nil, err
	}
	if err := runStep(8, "VERIFY_SHA256", "Verify SHA256", func() (any, error) {
		sum, err := fileSHA256(packagePath)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(sum, p.PackageSHA256) {
			return nil, fmt.Errorf("sha256 mismatch: got %s", sum)
		}
		return map[string]any{"sha256": sum}, nil
	}); err != nil {
		return nil, err
	}

	var mysqlUID, mysqlGID int = -1, -1
	if err := runStep(9, "CREATE_MYSQL_USER", "Prepare MySQL OS user", func() (any, error) {
		uid, gid, err := ensureMySQLUser(ctx, p)
		if err != nil {
			return nil, err
		}
		mysqlUID, mysqlGID = uid, gid
		return map[string]any{"user": p.MySQLUser, "uid": uid, "gid": gid}, nil
	}); err != nil {
		return nil, err
	}
	if err := runStep(10, "CREATE_DIRECTORIES", "Create directories", func() (any, error) {
		for _, d := range []string{p.BaseDir, p.DataDir, p.LogDir, p.BinlogDir, p.RunDir, filepath.Dir(p.ConfigPath)} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return nil, err
			}
		}
		if mysqlUID >= 0 {
			for _, d := range []string{p.DataDir, p.LogDir, p.BinlogDir, p.RunDir} {
				if err := os.Chmod(d, 0750); err != nil {
					return nil, err
				}
				if err := os.Chown(d, mysqlUID, mysqlGID); err != nil {
					return nil, err
				}
			}
		}
		return map[string]any{"created": true}, nil
	}); err != nil {
		return nil, err
	}
	if err := runStep(11, "INSTALL_PACKAGE", "Extract package", func() (any, error) {
		if err := ensureEmptyOrAbsentBaseDir(p.BaseDir); err != nil {
			return nil, err
		}
		if err := extractTarGzStripOne(packagePath, p.BaseDir); err != nil {
			return nil, err
		}
		if _, err := os.Stat(filepath.Join(p.BaseDir, "bin", "mysqld")); err != nil {
			return nil, fmt.Errorf("package does not contain bin/mysqld after extraction")
		}

		return map[string]any{"base_dir": p.BaseDir}, nil
	}); err != nil {
		return nil, err
	}
	if err := runStep(12, "RENDER_CONFIG", "Write configuration", func() (any, error) {
		if strings.TrimSpace(p.ConfigText) == "" {
			return nil, errors.New("config_text is empty")
		}
		// init_file runs account hardening before MySQL accepts connections.
		bootstrapSQL := filepath.Join(filepath.Dir(p.ConfigPath), "dbops-bootstrap.sql")
		sqlText := "SET SESSION sql_log_bin=0;\nALTER USER 'root'@'localhost' IDENTIFIED BY '" + p.RootPassword + "';\n"
		if err := writeNewFile(bootstrapSQL, []byte(sqlText), 0600); err != nil {
			return nil, err
		}
		bootstrapCreated = true
		if mysqlUID >= 0 {
			if err := os.Chown(bootstrapSQL, mysqlUID, mysqlGID); err != nil {
				return nil, err
			}
		}
		config := strings.Replace(p.ConfigText, "[mysqld]", "[mysqld]\ninit_file="+bootstrapSQL, 1)
		if err := writeNewFile(p.ConfigPath, []byte(config), 0o640); err != nil {
			return nil, err
		}
		if mysqlUID >= 0 {
			_ = os.Chown(p.ConfigPath, mysqlUID, mysqlGID)
		}
		return map[string]any{"config_path": p.ConfigPath}, nil
	}); err != nil {
		return nil, err
	}
	if err := runStep(13, "VALIDATE_CONFIG", "Validate configuration", func() (any, error) {
		out, err := runCommand(ctx, filepath.Join(p.BaseDir, "bin", "mysqld"), "--defaults-file="+p.ConfigPath, "--validate-config")
		return map[string]any{"output": out}, err
	}); err != nil {
		return nil, err
	}
	if err := runStep(14, "INITIALIZE_DATABASE", "Initialize MySQL", func() (any, error) {
		args := []string{"--defaults-file=" + p.ConfigPath, "--initialize-insecure"}
		if p.ManageOSUser && os.Geteuid() == 0 {
			args = append(args, "--user="+p.MySQLUser)
		}
		out, err := runCommand(ctx, filepath.Join(p.BaseDir, "bin", "mysqld"), args...)
		return map[string]any{"output": out}, err
	}); err != nil {
		return nil, err
	}
	if err := runStep(15, "INSTALL_SYSTEMD", "Install service definition", func() (any, error) {
		if p.ServiceMode == "process" {
			return map[string]any{"mode": "process"}, nil
		}
		if os.Geteuid() != 0 {
			return nil, errors.New("systemd installation requires root privileges")
		}
		unitPath, err := writeSystemdUnit(p)
		if err != nil {
			return nil, err
		}
		out, err := runCommand(ctx, "systemctl", "daemon-reload")
		return map[string]any{"unit_path": unitPath, "output": out}, err
	}); err != nil {
		return nil, err
	}
	if err := runStep(16, "START_DATABASE", "Start MySQL", func() (any, error) {
		if p.ServiceMode == "systemd" {
			out, err := runCommand(ctx, "systemctl", "enable", "--now", p.ServiceName+".service")
			return map[string]any{"output": out}, err
		}
		args := []string{"--defaults-file=" + p.ConfigPath, "--daemonize"}
		if p.ManageOSUser && os.Geteuid() == 0 {
			args = append(args, "--user="+p.MySQLUser)
		}
		out, err := runCommand(ctx, filepath.Join(p.BaseDir, "bin", "mysqld"), args...)
		return map[string]any{"output": out}, err
	}); err != nil {
		return nil, err
	}
	if err := runStep(17, "VERIFY_DATABASE", "Verify MySQL is listening", func() (any, error) {
		if err := waitTCP(ctx, p.Port, 30*time.Second); err != nil {
			return nil, err
		}
		return map[string]any{"port": p.Port, "listening": true}, nil
	}); err != nil {
		return nil, err
	}
	if err := runStep(18, "INITIALIZE_ACCOUNTS", "Initialize root account", func() (any, error) {
		if err := initializeRootAccount(ctx, p, taskDir); err != nil {
			return nil, err
		}
		return map[string]any{"root_account": "secured"}, nil
	}); err != nil {
		return nil, err
	}

	marker.Status = "agent_complete"
	marker.Step = "INITIALIZE_ACCOUNTS"
	marker.Error = ""
	marker.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_ = writeInstallMarker(markerPath, marker)
	_ = os.Remove(packagePath)
	return map[string]any{"agent_install_complete": true, "port": p.Port, "server_id": p.ServerID, "base_dir": p.BaseDir, "data_dir": p.DataDir, "log_dir": p.LogDir, "binlog_dir": p.BinlogDir, "run_dir": p.RunDir, "config_path": p.ConfigPath, "service_name": p.ServiceName, "service_mode": p.ServiceMode, "marker": markerPath}, nil
}

func parseMySQLInstallParams(m map[string]any) (mysqlInstallParams, error) {
	var p mysqlInstallParams
	var err error
	p.PackageURL, _ = m["package_url"].(string)
	p.PackageSHA256, _ = m["package_sha256"].(string)
	p.PackageType, _ = m["package_type"].(string)
	p.Version, _ = m["version"].(string)
	p.ExpectedOS, _ = m["expected_os"].(string)
	p.ExpectedArch, _ = m["expected_arch"].(string)
	p.Port, err = intParam(m, "port")
	if err != nil {
		return p, err
	}
	p.ServerID, err = intParam(m, "server_id")
	if err != nil {
		return p, err
	}
	p.BaseDir, _ = m["base_dir"].(string)
	p.DataDir, _ = m["data_dir"].(string)
	p.LogDir, _ = m["log_dir"].(string)
	p.BinlogDir, _ = m["binlog_dir"].(string)
	p.RunDir, _ = m["run_dir"].(string)
	p.ConfigPath, _ = m["config_path"].(string)
	p.ConfigText, _ = m["config_text"].(string)
	p.ServiceName, _ = m["service_name"].(string)
	p.ServiceMode, _ = m["service_mode"].(string)
	p.MySQLUser, _ = m["mysql_user"].(string)
	p.ManageOSUser, _ = m["manage_os_user"].(bool)
	p.RootPassword, _ = m["root_password"].(string)
	p.MinFreeBytes, _ = uint64Param(m, "min_free_bytes")
	p.MinMemoryBytes, _ = uint64Param(m, "min_memory_bytes")
	p.MinCPUCores, _ = intParamDefault(m, "min_cpu_cores", 1)
	if p.PackageURL == "" || len(p.PackageSHA256) != 64 || p.RootPassword == "" || p.ConfigText == "" {
		return p, errors.New("missing package, password, or configuration parameters")
	}
	if p.Port < 1 || p.Port > 65535 || p.ServerID <= 0 || !validServiceName(p.ServiceName) || !validServiceName(p.MySQLUser) || !safeSQLSecret(p.RootPassword) {
		return p, errors.New("invalid installation identity or credentials")
	}
	for _, v := range []string{p.BaseDir, p.DataDir, p.LogDir, p.BinlogDir, p.RunDir, p.ConfigPath} {
		if !filepath.IsAbs(v) || filepath.Clean(v) == "/" || strings.ContainsAny(v, " \t\r\n\x00%\\\"'") {
			return p, errors.New("invalid installation path")
		}
	}
	return p, nil
}

func downloadFile(ctx context.Context, url, path string, client *http.Client) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download HTTP status %s", resp.Status)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, 8<<30))
	closeErr := f.Close()
	if copyErr != nil {
		return n, copyErr
	}
	return n, closeErr
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ensureMySQLUser(ctx context.Context, p mysqlInstallParams) (int, int, error) {
	u, err := user.Lookup(p.MySQLUser)
	if err == nil {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		return uid, gid, nil
	}
	if !p.ManageOSUser {
		if p.ServiceMode == "process" {
			return os.Geteuid(), os.Getegid(), nil
		}
		return -1, -1, fmt.Errorf("OS user %s does not exist", p.MySQLUser)
	}
	if os.Geteuid() != 0 {
		return -1, -1, errors.New("creating MySQL OS user requires root")
	}
	if _, err := runCommand(ctx, "useradd", "--system", "--no-create-home", "--shell", "/sbin/nologin", p.MySQLUser); err != nil {
		return -1, -1, err
	}
	u, err = user.Lookup(p.MySQLUser)
	if err != nil {
		return -1, -1, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return uid, gid, nil
}

func ensureEmptyOrAbsentBaseDir(path string) error {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return os.MkdirAll(path, 0o755)
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("base_dir %s is not empty", path)
	}
	return nil
}

func extractTarGzStripOne(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	cleanDest := filepath.Clean(dest) + string(os.PathSeparator)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(h.Name)
		parts := strings.Split(name, string(os.PathSeparator))
		if len(parts) < 2 {
			continue
		}
		rel := filepath.Join(parts[1:]...)
		if rel == "." || rel == "" {
			continue
		}
		target := filepath.Join(dest, rel)
		cleanTarget := filepath.Clean(target)
		if !strings.HasPrefix(cleanTarget, cleanDest) {
			return fmt.Errorf("unsafe archive path %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleanTarget, os.FileMode(h.Mode)&0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o755)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(h.Linkname) || strings.Contains(filepath.Clean(h.Linkname), "..") {
				return fmt.Errorf("unsafe symlink %q", h.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, cleanTarget); err != nil {
				return err
			}
		default:
		}
	}
	return nil
}

func chownTree(root string, uid, gid int) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(path, uid, gid)
	})
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	lw := &limitedBuffer{buf: &buf, limit: 64 << 10}
	cmd.Stdout = lw
	cmd.Stderr = lw
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err != nil {
		return out, fmt.Errorf("%s %v failed: %w: %s", name, args, err, out)
	}
	return out, nil
}

type limitedBuffer struct {
	buf   *bytes.Buffer
	limit int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := l.limit - l.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = l.buf.Write(p[:remaining])
		} else {
			_, _ = l.buf.Write(p)
		}
	}
	return n, nil
}

func writeSystemdUnit(p mysqlInstallParams) (string, error) {
	unitPath := filepath.Join("/etc/systemd/system", p.ServiceName+".service")
	unit := "[Unit]\nDescription=DBOps MySQL " + strconv.Itoa(p.Port) + "\nAfter=network.target\n\n[Service]\nType=simple\nUser=" + p.MySQLUser + "\nGroup=" + p.MySQLUser + "\nExecStart=" + filepath.Join(p.BaseDir, "bin", "mysqld") + " --defaults-file=" + p.ConfigPath + "\nRestart=on-failure\nLimitNOFILE=65535\n\n[Install]\nWantedBy=multi-user.target\n"
	if err := writeNewFile(unitPath, []byte(unit), 0o644); err != nil {
		return "", err
	}
	return unitPath, nil
}

func waitTCP(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		d := net.Dialer{Timeout: time.Second}
		conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("MySQL port %d did not become ready", port)
}

func initializeRootAccount(ctx context.Context, p mysqlInstallParams, taskDir string) error {
	verifyCfg := filepath.Join(taskDir, "root-verify.cnf")
	verify := "[client]\nuser=root\npassword=" + mysqlOption(p.RootPassword) + "\nsocket=" + p.RunDir + "/mysql.sock\n"
	if err := os.WriteFile(verifyCfg, []byte(verify), 0o600); err != nil {
		return err
	}
	defer os.Remove(verifyCfg)
	// SELECT verifies authentication; mysqladmin ping can succeed on access denied.
	_, err := runCommand(ctx, filepath.Join(p.BaseDir, "bin", "mysql"), "--defaults-extra-file="+verifyCfg, "--batch", "--skip-column-names", "-e", "SELECT 1")
	if err != nil {
		return err
	}
	// Persist the normal config before removing the bootstrap file, so restarts
	// do not depend on a deleted init_file. Never return or log the password.
	if err = os.WriteFile(p.ConfigPath, []byte(p.ConfigText), 0640); err != nil {
		return err
	}
	return os.Remove(filepath.Join(filepath.Dir(p.ConfigPath), "dbops-bootstrap.sql"))
}

func writeInstallMarker(path string, m installMarker) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeNewFile(path string, contents []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = f.Write(contents); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
