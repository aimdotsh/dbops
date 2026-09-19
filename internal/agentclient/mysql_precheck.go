package agentclient

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type precheckResult struct {
	OK     bool             `json:"ok"`
	Checks []map[string]any `json:"checks"`
}

func mysqlPrecheck(ctx context.Context, params map[string]any) (precheckResult, error) {
	port, err := intParamDefault(params, "port", 3306)
	if err != nil || port < 1 || port > 65535 {
		return precheckResult{}, fmt.Errorf("invalid port")
	}
	dataDir, _ := params["data_dir"].(string)
	if dataDir == "" || !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) == "/" {
		return precheckResult{}, fmt.Errorf("data_dir must be an absolute non-root path")
	}
	minFree, err := uint64Param(params, "min_free_bytes")
	if err != nil {
		return precheckResult{}, err
	}
	minMemory, err := uint64Param(params, "min_memory_bytes")
	if err != nil {
		return precheckResult{}, err
	}
	minCPU, err := intParamDefault(params, "min_cpu_cores", 1)
	if err != nil || minCPU < 1 {
		return precheckResult{}, fmt.Errorf("invalid min_cpu_cores")
	}

	result := precheckResult{OK: true}
	add := func(name, status, message string, detail any) {
		result.Checks = append(result.Checks, map[string]any{
			"name": name, "status": status, "message": message, "detail": detail,
		})
		if status == "block" {
			result.OK = false
		}
	}

	expectedOS, _ := params["expected_os"].(string)
	expectedArch, _ := params["expected_arch"].(string)
	if expectedOS != "" && expectedOS != runtime.GOOS {
		add("os", "block", "operating system does not match package compatibility", map[string]any{"actual": runtime.GOOS, "expected": expectedOS})
	} else {
		add("os", "pass", "operating system compatible", runtime.GOOS)
	}
	if expectedArch != "" && expectedArch != runtime.GOARCH {
		add("arch", "block", "architecture does not match package compatibility", map[string]any{"actual": runtime.GOARCH, "expected": expectedArch})
	} else {
		add("arch", "pass", "architecture compatible", runtime.GOARCH)
	}

	info, _ := hostInfo()
	cpu, _ := info["cpu_cores"].(int)
	mem, _ := info["memory_bytes"].(uint64)
	if cpu < minCPU {
		add("cpu", "block", "cpu cores below minimum", map[string]any{"actual": cpu, "minimum": minCPU})
	} else {
		add("cpu", "pass", "cpu requirement satisfied", map[string]any{"actual": cpu, "minimum": minCPU})
	}
	if minMemory > 0 && mem < minMemory {
		add("memory", "block", "memory below minimum", map[string]any{"actual": mem, "minimum": minMemory})
	} else {
		add("memory", "pass", "memory requirement satisfied", map[string]any{"actual": mem, "minimum": minMemory})
	}

	portResult, err := portCheck(ctx, map[string]any{"port": port})
	if err != nil {
		return precheckResult{}, err
	}
	if available, _ := portResult["available"].(bool); !available {
		add("port", "block", "target port is already in use", portResult)
	} else {
		add("port", "pass", "target port is available", portResult)
	}

	dataStatus, err := inspectMySQLDataDir(dataDir, minFree)
	if err != nil {
		return precheckResult{}, err
	}
	if dataStatus.ValidMySQLData {
		add("data_dir", "block", "existing MySQL data directory detected", dataStatus)
	} else if dataStatus.FreeBytes < minFree {
		add("disk", "block", "insufficient free disk space", dataStatus)
	} else {
		add("data_dir", "pass", "data directory has no existing MySQL markers", dataStatus)
		add("disk", "pass", "disk space requirement satisfied", dataStatus)
	}

	if conflict, detail := mysqlProcessConflict(port, dataDir); conflict {
		add("mysqld", "block", "conflicting mysqld process detected for target port or data directory", detail)
	} else {
		add("mysqld", "pass", "no conflicting mysqld process detected", detail)
	}

	serviceName := fmt.Sprintf("dbops-mysql-%d", port)
	if v, _ := params["service_name"].(string); v != "" {
		if !validServiceName(v) {
			return precheckResult{}, fmt.Errorf("invalid service_name")
		}
		serviceName = v
	}
	if path := existingSystemdUnit(serviceName); path != "" {
		add("systemd_unit", "block", "systemd unit already exists", path)
	} else {
		add("systemd_unit", "pass", "systemd unit name is available", serviceName)
	}

	add("time_sync", "warn", "time synchronization verification is not yet authoritative in this agent version", nil)
	return result, nil
}

type dataDirStatus struct {
	Path           string `json:"path"`
	Exists         bool   `json:"exists"`
	ValidMySQLData bool   `json:"valid_mysql_data"`
	FreeBytes      uint64 `json:"free_bytes"`
}

func inspectMySQLDataDir(path string, minFree uint64) (dataDirStatus, error) {
	status := dataDirStatus{Path: path}
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return status, fmt.Errorf("data_dir is not a directory")
		}
		status.Exists = true
		for _, marker := range []string{"auto.cnf", "mysql.ibd", "ibdata1"} {
			if _, err := os.Stat(filepath.Join(path, marker)); err == nil {
				status.ValidMySQLData = true
				break
			}
		}
	} else if !os.IsNotExist(err) {
		return status, err
	}

	probe := path
	for {
		if _, err := os.Stat(probe); err == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return status, fmt.Errorf("cannot locate existing parent for data_dir")
		}
		probe = parent
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(probe, &st); err != nil {
		return status, err
	}
	status.FreeBytes = uint64(st.Bavail) * uint64(st.Bsize)
	_ = minFree
	return status, nil
}

func mysqlProcessConflict(port int, dataDir string) (bool, []map[string]any) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, nil
	}
	targetPortA := fmt.Sprintf("--port=%d", port)
	targetPortB := fmt.Sprintf("--port %d", port)
	targetDataA := "--datadir=" + filepath.Clean(dataDir)
	targetDataB := "--datadir " + filepath.Clean(dataDir)
	var matches []map[string]any
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(comm))
		if name != "mysqld" && name != "mysqld_safe" {
			continue
		}
		cmdlineBytes, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		cmdline := strings.ReplaceAll(string(cmdlineBytes), "\x00", " ")
		conflict := strings.Contains(cmdline, targetPortA) ||
			strings.Contains(cmdline, targetPortB) ||
			strings.Contains(cmdline, targetDataA) ||
			strings.Contains(cmdline, targetDataB)
		if conflict {
			matches = append(matches, map[string]any{"pid": pid, "command": name})
		}
	}
	return len(matches) > 0, matches
}

func existingSystemdUnit(name string) string {
	for _, dir := range []string{"/etc/systemd/system", "/usr/lib/systemd/system", "/lib/systemd/system"} {
		path := filepath.Join(dir, name+".service")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func validServiceName(v string) bool {
	if len(v) == 0 || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '@' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func intParamDefault(params map[string]any, key string, def int) (int, error) {
	if _, ok := params[key]; !ok {
		return def, nil
	}
	return intParam(params, key)
}
