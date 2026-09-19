package agentclient

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aimdotsh/dbops/internal/actionpolicy"
	"github.com/aimdotsh/dbops/internal/agentproto"
	"gopkg.in/yaml.v3"
)

type ProgressReporter func(agentproto.ActionResponse)

type Executor struct {
	specs   map[string]agentproto.ActionSpec
	workDir string
}

func LoadExecutor(path string) (*Executor, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file agentproto.ActionFile
	if err := yaml.Unmarshal(b, &file); err != nil {
		return nil, err
	}
	specs := make(map[string]agentproto.ActionSpec, len(file.Actions))
	for _, spec := range file.Actions {
		specs[spec.Name] = spec
	}
	return &Executor{specs: specs, workDir: "/var/lib/dbops-agent"}, nil
}

func (e *Executor) SetWorkDir(path string) {
	if path != "" {
		e.workDir = path
	}
}

func (e *Executor) Capabilities() []string {
	out := make([]string, 0, len(e.specs))
	for name := range e.specs {
		out = append(out, name)
	}
	return out
}

func (e *Executor) Execute(parent context.Context, req agentproto.ActionRequest) (any, error) {
	return e.ExecuteWithReporter(parent, req, nil)
}

func (e *Executor) ExecuteWithReporter(parent context.Context, req agentproto.ActionRequest, report ProgressReporter) (any, error) {
	spec, ok := e.specs[req.Action]
	if !ok {
		return nil, fmt.Errorf("action %q is not allowed", req.Action)
	}
	policy, err := actionpolicy.Validate(req.Action, req.Confirmed)
	if err != nil {
		return nil, err
	}
	if req.Risk != "" && req.Risk != string(policy.Risk) {
		return nil, fmt.Errorf("action risk mismatch: request=%s policy=%s", req.Risk, policy.Risk)
	}

	timeout := req.TimeoutSeconds
	if timeout <= 0 || (spec.Timeout > 0 && timeout > spec.Timeout) {
		timeout = spec.Timeout
	}
	if timeout <= 0 {
		timeout = 30
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Second)
	defer cancel()

	switch req.Action {
	case "host.info":
		return hostInfo()
	case "host.disk.list":
		return diskList(ctx)
	case "host.port.check":
		return portCheck(ctx, req.Params)
	case "host.directory.check":
		return directoryCheck(ctx, req.Params)
	case "mysql.precheck":
		return mysqlPrecheck(ctx, req.Params)
	case "mysql.install":
		if execute, _ := req.Params["execute"].(bool); execute {
			return mysqlInstall(ctx, e.workDir, req, report)
		}
		return mysqlInstallPlan(req.Params)
	default:
		return nil, fmt.Errorf("action %q is allowed but not implemented by this agent version", req.Action)
	}
}

func hostInfo() (map[string]any, error) {
	hostname, _ := os.Hostname()
	var memTotal uint64
	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		s := bufio.NewScanner(f)
		for s.Scan() {
			fields := strings.Fields(s.Text())
			if len(fields) >= 2 && fields[0] == "MemTotal:" {
				kb, _ := strconv.ParseUint(fields[1], 10, 64)
				memTotal = kb * 1024
				break
			}
		}
	}
	return map[string]any{
		"hostname":     hostname,
		"os":           runtime.GOOS,
		"architecture": runtime.GOARCH,
		"cpu_cores":    runtime.NumCPU(),
		"memory_bytes": memTotal,
	}, nil
}

func diskList(ctx context.Context) ([]map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := map[string]bool{}
	var out []map[string]any
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 3 {
			continue
		}
		mount := strings.ReplaceAll(fields[1], "\\040", " ")
		if seen[mount] {
			continue
		}
		seen[mount] = true

		var st syscall.Statfs_t
		if err := syscall.Statfs(mount, &st); err != nil {
			continue
		}
		total := uint64(st.Blocks) * uint64(st.Bsize)
		free := uint64(st.Bavail) * uint64(st.Bsize)
		out = append(out, map[string]any{
			"mount":       mount,
			"filesystem":  fields[0],
			"fs_type":     fields[2],
			"total_bytes": total,
			"free_bytes":  free,
			"used_bytes":  total - free,
		})
	}
	return out, s.Err()
}

func portCheck(ctx context.Context, params map[string]any) (map[string]any, error) {
	port, err := intParam(params, "port")
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port")
	}

	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return map[string]any{"port": port, "available": false, "reason": err.Error()}, nil
	}
	_ = ln.Close()
	return map[string]any{"port": port, "available": true}, nil
}

func directoryCheck(ctx context.Context, params map[string]any) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	path, _ := params["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	requireEmpty, _ := params["require_empty"].(bool)
	minFree, _ := uint64Param(params, "min_free_bytes")

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return map[string]any{"path": path, "exists": false, "free_bytes": uint64(0)}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return nil, err
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	empty := len(entries) == 0
	return map[string]any{
		"path":              path,
		"exists":            true,
		"empty":             empty,
		"require_empty":     requireEmpty,
		"empty_requirement": !requireEmpty || empty,
		"free_bytes":        free,
		"min_free_bytes":    minFree,
		"space_requirement": free >= minFree,
	}, nil
}

func intParam(params map[string]any, key string) (int, error) {
	v, ok := params[key]
	if !ok {
		return 0, fmt.Errorf("%s is required", key)
	}
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case string:
		return strconv.Atoi(n)
	default:
		return 0, fmt.Errorf("%s must be a number", key)
	}
}

func uint64Param(params map[string]any, key string) (uint64, error) {
	v, ok := params[key]
	if !ok {
		return 0, nil
	}
	switch n := v.(type) {
	case float64:
		if n < 0 {
			return 0, fmt.Errorf("%s must be non-negative", key)
		}
		return uint64(n), nil
	case float32:
		if n < 0 {
			return 0, fmt.Errorf("%s must be non-negative", key)
		}
		return uint64(n), nil
	case uint64:
		return n, nil
	case uint:
		return uint64(n), nil
	case uint32:
		return uint64(n), nil
	case int64:
		if n < 0 {
			return 0, fmt.Errorf("%s must be non-negative", key)
		}
		return uint64(n), nil
	case int:
		if n < 0 {
			return 0, fmt.Errorf("%s must be non-negative", key)
		}
		return uint64(n), nil
	case int32:
		if n < 0 {
			return 0, fmt.Errorf("%s must be non-negative", key)
		}
		return uint64(n), nil
	case string:
		return strconv.ParseUint(n, 10, 64)
	default:
		return 0, fmt.Errorf("%s must be a number", key)
	}
}

func mysqlInstallPlan(params map[string]any) (map[string]any, error) {
	execute, _ := params["execute"].(bool)
	if execute {
		return nil, fmt.Errorf("mysql.install execution is not enabled until software package repository, SHA256 verification, systemd installation and rollback markers are configured")
	}
	port, err := intParamDefault(params, "port", 3306)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port")
	}
	dataDir, _ := params["data_dir"].(string)
	if dataDir == "" {
		return nil, fmt.Errorf("data_dir is required")
	}
	return map[string]any{
		"mode":     "plan_only",
		"risk":     "R2",
		"port":     port,
		"data_dir": dataDir,
		"steps": []string{
			"CHECK_AGENT",
			"CHECK_OS_ARCH",
			"CHECK_PORT",
			"CHECK_DATADIR",
			"CHECK_DISK",
			"ALLOCATE_SERVER_ID",
			"DOWNLOAD_PACKAGE",
			"VERIFY_SHA256",
			"CREATE_MYSQL_USER",
			"CREATE_DIRECTORIES",
			"INSTALL_PACKAGE",
			"RENDER_CONFIG",
			"VALIDATE_CONFIG",
			"INITIALIZE_DATABASE",
			"INSTALL_SYSTEMD",
			"START_DATABASE",
			"VERIFY_DATABASE",
			"INITIALIZE_ACCOUNTS",
			"REGISTER_INSTANCE",
			"ENABLE_METRICS",
			"FINAL_VERIFY",
		},
	}, nil
}
