package mysqlinstall

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// NormalizeLayout supplies per-port defaults and preserves explicit overrides.
func NormalizeLayout(req *InstallRequest) error {
	if req.Port == 0 {
		req.Port = 3306
	}
	if req.Port < 1 || req.Port > 65535 {
		return fmt.Errorf("invalid MySQL port")
	}
	if req.InstallRoot == "" {
		req.InstallRoot = "/opt/dbops"
	}
	if err := validateAbsolutePath("install_root", req.InstallRoot); err != nil {
		return err
	}
	req.InstallRoot = filepath.Clean(req.InstallRoot)
	instance := filepath.Join(req.InstallRoot, "mysql", strconv.Itoa(req.Port))
	for field, suffix := range map[*string]string{
		&req.BaseDir: "base", &req.DataDir: "data", &req.LogDir: "log",
		&req.BinlogDir: "binlog", &req.RunDir: "run", &req.ConfigPath: "conf/my.cnf",
	} {
		if *field == "" {
			*field = filepath.Join(instance, suffix)
		}
	}
	for name, value := range map[string]string{"base_dir": req.BaseDir, "data_dir": req.DataDir, "log_dir": req.LogDir, "binlog_dir": req.BinlogDir, "run_dir": req.RunDir, "config_path": req.ConfigPath} {
		if err := validateAbsolutePath(name, value); err != nil {
			return err
		}
	}
	req.ServiceName = strings.TrimSuffix(req.ServiceName, ".service")
	if req.ServiceName == "" {
		req.ServiceName = fmt.Sprintf("dbops-mysql%d", req.Port)
	}
	if !validIdentifier(req.ServiceName) {
		return fmt.Errorf("invalid service_name")
	}
	return nil
}
