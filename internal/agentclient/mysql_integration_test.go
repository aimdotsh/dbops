//go:build integration

package agentclient

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/mysqlinstall"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDisposableMySQLBackupRestore(t *testing.T) {
	if os.Getenv("DBOPS_DISPOSABLE_MYSQL") != "1" {
		t.Skip("requires scripts/test-real-mysql.sh disposable container")
	}
	ctx := context.Background()
	work := t.TempDir()
	password := "Dbops-disposable-test-123"
	query := func(sql string) string {
		t.Helper()
		out, err := runMySQLQuery(ctx, "/usr", "/var/run/mysqld", password, sql, true)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	query("CREATE DATABASE dbops_fixture; CREATE TABLE dbops_fixture.items(id INT PRIMARY KEY, name VARCHAR(80)); INSERT INTO dbops_fixture.items VALUES(1,'first'),(2,'second');")
	params := map[string]any{"base_dir": "/usr", "run_dir": "/var/run/mysqld", "root_password": password, "output_dir": filepath.Join(work, "backups"), "all_databases": false, "databases": []string{"dbops_fixture"}}
	backup, err := mysqlBackup(ctx, work, params)
	if err != nil {
		t.Fatal(err)
	}
	restore := map[string]any{"base_dir": "/usr", "run_dir": "/var/run/mysqld", "root_password": password, "backup_path": backup["path"], "sha256": backup["sha256"]}
	if _, err = mysqlRestore(ctx, work, restore); err == nil {
		t.Fatal("restore accepted nonempty target")
	}
	query("DROP DATABASE dbops_fixture;")
	original := restore["sha256"]
	restore["sha256"] = strings.Repeat("0", 64)
	if _, err = mysqlRestore(ctx, work, restore); err == nil {
		t.Fatal("restore accepted checksum mismatch")
	}
	restore["sha256"] = original
	result, err := mysqlRestore(ctx, work, restore)
	if err != nil {
		t.Fatal(err)
	}
	if got := query("SELECT GROUP_CONCAT(CONCAT(id,':',name) ORDER BY id) FROM dbops_fixture.items;"); got != "1:first,2:second" {
		t.Fatalf("restored data mismatch: %q", got)
	}
	t.Logf("real MySQL backup + checksum + empty-target protection + restored rows verified: %v", result)
}

func TestDisposableMySQLInstall(t *testing.T) {
	if os.Getenv("DBOPS_DISPOSABLE_MYSQL") != "1" {
		t.Skip("requires disposable container")
	}
	root, err := os.MkdirTemp("/tmp", "dbops-real-install-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err = os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "mysql.tgz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, target := range map[string]string{"mysqld": "/usr/sbin/mysqld", "mysql": "/usr/bin/mysql", "mysqladmin": "/usr/bin/mysqladmin"} {
		body := []byte("#!/bin/sh\nexec " + target + " \"$@\"\n")
		if err = tw.WriteHeader(&tar.Header{Name: "mysql/bin/" + name, Mode: 0755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err = tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	digest, err := fileSHA256(archive)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, archive) }))
	defer server.Close()
	base, data, logDir, binlog, run, config := filepath.Join(root, "base"), filepath.Join(root, "data"), filepath.Join(root, "log"), filepath.Join(root, "binlog"), filepath.Join(root, "run"), filepath.Join(root, "etc", "my.cnf")
	configText, err := mysqlinstall.RenderConfig(mysqlinstall.ConfigValues{Port: 13307, ServerID: 13307, BaseDir: base, DataDir: data, LogDir: logDir, BinlogDir: binlog, RunDir: run, InnoDBBufferPoolBytes: 128 << 20, MaxConnections: 50, LongQueryTime: "1", Collation: "utf8mb4_0900_ai_ci"})
	if err != nil {
		t.Fatal(err)
	}
	configText += "\n[mysqld]\nlc_messages_dir=/usr/share/mysql-8.0\nmysqlx=0\n"
	password := "Fresh install # password-123"
	params := map[string]any{"package_url": server.URL, "package_sha256": digest, "package_type": "tar.gz", "expected_os": "linux", "expected_arch": "amd64", "port": 13307, "server_id": 13307, "base_dir": base, "data_dir": data, "log_dir": logDir, "binlog_dir": binlog, "run_dir": run, "config_path": config, "config_text": configText, "service_name": "dbops-test-real", "service_mode": "process", "mysql_user": "mysql", "manage_os_user": true, "root_password": password, "min_free_bytes": 1, "min_memory_bytes": 1, "min_cpu_cores": 1}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	defer func() {
		_ = exec.Command("/usr/bin/mysqladmin", "--socket="+run+"/mysql.sock", "--user=root", "--password="+password, "shutdown").Run()
	}()
	_, err = mysqlInstall(ctx, filepath.Join(root, "agent"), agentproto.ActionRequest{TaskID: 99, Params: params}, nil, server.Client())
	if err != nil {
		if b, e := os.ReadFile(filepath.Join(logDir, "error.log")); e == nil {
			t.Log(string(b))
		}
		t.Fatal(err)
	}
	if out, err := runMySQLQuery(ctx, base, run, password, "SELECT 1", false); err != nil || strings.TrimSpace(out) != "1" {
		t.Fatalf("secured root cannot authenticate: %s %v", out, err)
	}
	if _, err := runMySQLQuery(ctx, base, run, "", "SELECT 1", false); err == nil {
		t.Fatal("passwordless root accepted")
	}
	if _, err = os.Stat(filepath.Join(root, "etc", "dbops-bootstrap.sql")); !os.IsNotExist(err) {
		t.Fatal("bootstrap secret file retained")
	}
	if out, err := runMySQLQuery(ctx, base, run, password, "SELECT @@GLOBAL.gtid_executed", false); err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("bootstrap credentials entered replication log: %q %v", out, err)
	}
	t.Log("real mysqld installation, initialization, account hardening and cleanup verified")
}
