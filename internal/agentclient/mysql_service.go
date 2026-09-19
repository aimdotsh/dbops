package agentclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func mysqlServiceAction(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
	baseDir, _ := params["base_dir"].(string)
	runDir, _ := params["run_dir"].(string)
	configPath, _ := params["config_path"].(string)
	serviceName, _ := params["service_name"].(string)
	serviceMode, _ := params["service_mode"].(string)
	port, err := intParam(params, "port")
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("invalid port")
	}
	if serviceMode == "" {
		serviceMode = "systemd"
	}
	if serviceMode == "systemd" {
		if !validServiceName(serviceName) {
			return nil, errors.New("invalid service_name")
		}
		switch action {
		case "mysql.start":
			out, err := runCommand(ctx, "systemctl", "start", serviceName+".service")
			if err != nil {
				return nil, err
			}
			if err := waitTCP(ctx, port, 30*time.Second); err != nil {
				return nil, err
			}
			return map[string]any{"status": "online", "output": out}, nil
		case "mysql.stop":
			out, err := runCommand(ctx, "systemctl", "stop", serviceName+".service")
			if err != nil {
				return nil, err
			}
			if err := waitPortClosed(ctx, port, 30*time.Second); err != nil {
				return nil, err
			}
			return map[string]any{"status": "offline", "output": out}, nil
		case "mysql.restart":
			out, err := runCommand(ctx, "systemctl", "restart", serviceName+".service")
			if err != nil {
				return nil, err
			}
			if err := waitTCP(ctx, port, 30*time.Second); err != nil {
				return nil, err
			}
			return map[string]any{"status": "online", "output": out}, nil
		}
	}
	if serviceMode != "process" {
		return nil, fmt.Errorf("unsupported service_mode %q", serviceMode)
	}
	if !filepath.IsAbs(baseDir) || !filepath.IsAbs(runDir) || !filepath.IsAbs(configPath) {
		return nil, errors.New("invalid runtime paths")
	}
	pidfile := filepath.Join(runDir, "mysqld.pid")
	switch action {
	case "mysql.start":
		if processAlive(pidfile) {
			return map[string]any{"status": "online", "already_running": true}, nil
		}
		out, err := runCommand(ctx, filepath.Join(baseDir, "bin", "mysqld"), "--defaults-file="+configPath, "--daemonize")
		if err != nil {
			return nil, err
		}
		if err := waitTCP(ctx, port, 30*time.Second); err != nil {
			return nil, err
		}
		return map[string]any{"status": "online", "output": out}, nil
	case "mysql.stop":
		if !processAlive(pidfile) {
			return map[string]any{"status": "offline", "already_stopped": true}, nil
		}
		if err := terminatePIDFile(ctx, pidfile, 30*time.Second); err != nil {
			return nil, err
		}
		if err := waitPortClosed(ctx, port, 30*time.Second); err != nil {
			return nil, err
		}
		return map[string]any{"status": "offline"}, nil
	case "mysql.restart":
		if processAlive(pidfile) {
			if err := terminatePIDFile(ctx, pidfile, 30*time.Second); err != nil {
				return nil, err
			}
		}
		out, err := runCommand(ctx, filepath.Join(baseDir, "bin", "mysqld"), "--defaults-file="+configPath, "--daemonize")
		if err != nil {
			return nil, err
		}
		if err := waitTCP(ctx, port, 30*time.Second); err != nil {
			return nil, err
		}
		return map[string]any{"status": "online", "output": out}, nil
	default:
		return nil, fmt.Errorf("unsupported service action %q", action)
	}
}

func processAlive(pidfile string) bool {
	b, err := os.ReadFile(pidfile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(string(bytesTrimSpace(b)))
	if err != nil || pid <= 1 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func terminatePIDFile(ctx context.Context, pidfile string, timeout time.Duration) error {
	b, err := os.ReadFile(pidfile)
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(string(bytesTrimSpace(b)))
	if err != nil || pid <= 1 {
		return errors.New("invalid pidfile")
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p.Signal(syscall.Signal(0)) != nil {
			_ = os.Remove(pidfile)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("process %d did not stop before timeout", pid)
}

func waitPortClosed(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		d := netDialer()
		conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return nil
		}
		_ = conn.Close()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("MySQL port %d remains open", port)
}

func netDialer() *net.Dialer {
	return &net.Dialer{Timeout: time.Second}
}
