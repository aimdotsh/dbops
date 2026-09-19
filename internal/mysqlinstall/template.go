package mysqlinstall

import (
	"fmt"
	"strconv"
	"strings"
)

type ConfigValues struct {
	Port int
	ServerID int
	BaseDir string
	DataDir string
	LogDir string
	BinlogDir string
	RunDir string
	InnoDBBufferPoolBytes int64
	MaxConnections int
	LongQueryTime string
	Collation string
}

const productionTemplate = "[mysqld]\n" +
	"port={{PORT}}\n" +
	"server_id={{SERVER_ID}}\n" +
	"basedir={{BASEDIR}}\n" +
	"datadir={{DATADIR}}\n" +
	"socket={{RUNDIR}}/mysql.sock\n" +
	"pid-file={{RUNDIR}}/mysqld.pid\n" +
	"log_error={{LOGDIR}}/error.log\n" +
	"slow_query_log=ON\n" +
	"slow_query_log_file={{LOGDIR}}/slow.log\n" +
	"long_query_time={{LONG_QUERY_TIME}}\n" +
	"character_set_server=utf8mb4\n" +
	"collation_server={{COLLATION}}\n" +
	"log_bin={{BINLOGDIR}}/mysql-bin\n" +
	"binlog_format=ROW\n" +
	"gtid_mode=ON\n" +
	"enforce_gtid_consistency=ON\n" +
	"sync_binlog=1\n" +
	"innodb_flush_log_at_trx_commit=1\n" +
	"innodb_buffer_pool_size={{INNODB_BUFFER_POOL_SIZE}}\n" +
	"max_connections={{MAX_CONNECTIONS}}\n\n" +
	"[client]\n" +
	"port={{PORT}}\n" +
	"socket={{RUNDIR}}/mysql.sock\n"

func RenderConfig(v ConfigValues) (string, error) {
	if v.Port < 1 || v.Port > 65535 || v.ServerID <= 0 {
		return "", fmt.Errorf("invalid port or server_id")
	}
	if v.InnoDBBufferPoolBytes <= 0 || v.MaxConnections <= 0 {
		return "", fmt.Errorf("invalid memory or connection settings")
	}
	if !safeConfigAtom(v.Collation) || !safeConfigAtom(v.LongQueryTime) {
		return "", fmt.Errorf("invalid collation or long_query_time")
	}
	for _, path := range []string{v.BaseDir, v.DataDir, v.LogDir, v.BinlogDir, v.RunDir} {
		if strings.ContainsAny(path, "\r\n\x00") {
			return "", fmt.Errorf("path contains invalid characters")
		}
	}

	replacements := map[string]string{
		"{{PORT}}": strconv.Itoa(v.Port),
		"{{SERVER_ID}}": strconv.Itoa(v.ServerID),
		"{{BASEDIR}}": v.BaseDir,
		"{{DATADIR}}": v.DataDir,
		"{{LOGDIR}}": v.LogDir,
		"{{BINLOGDIR}}": v.BinlogDir,
		"{{RUNDIR}}": v.RunDir,
		"{{LONG_QUERY_TIME}}": v.LongQueryTime,
		"{{COLLATION}}": v.Collation,
		"{{INNODB_BUFFER_POOL_SIZE}}": strconv.FormatInt(v.InnoDBBufferPoolBytes, 10),
		"{{MAX_CONNECTIONS}}": strconv.Itoa(v.MaxConnections),
	}
	out := productionTemplate
	for key, value := range replacements {
		out = strings.ReplaceAll(out, key, value)
	}
	if strings.Contains(out, "{{") {
		return "", fmt.Errorf("unresolved configuration variable")
	}
	return out, nil
}

func safeConfigAtom(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
