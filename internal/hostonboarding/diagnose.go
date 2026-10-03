package hostonboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
	"golang.org/x/crypto/ssh"
)

type InstanceDiagnosis struct {
	InstanceID    int64  `json:"instance_id"`
	Name          string `json:"name"`
	Engine        string `json:"engine"`
	Port          int    `json:"port"`
	ServiceName   string `json:"service_name,omitempty"`
	ServiceActive *bool  `json:"service_active,omitempty"`
	ProcessSeen   *bool  `json:"process_seen,omitempty"`
	PortListening *bool  `json:"port_listening,omitempty"`
	Conclusion    string `json:"conclusion"`
}

type DiagnosisResult struct {
	HostID     int64               `json:"host_id"`
	Hostname   string              `json:"hostname"`
	Address    string              `json:"address"`
	CheckedAt  time.Time           `json:"checked_at"`
	Instances  []InstanceDiagnosis `json:"instances"`
	Limitation string              `json:"limitation"`
}

var safeUnitName = regexp.MustCompile(`^[A-Za-z0-9_.@-]+$`)

// Diagnose performs fixed, read-only process and listener checks. The supplied
// SSH credential is used for this request only and is never stored.
func (s *Service) Diagnose(ctx context.Context, req Request, host domain.Host, instances []domain.DatabaseInstance) (DiagnosisResult, error) {
	if err := validateConnection(req); err != nil {
		return DiagnosisResult{}, err
	}
	if !req.Confirmed || req.HostKeyFingerprint == "" {
		return DiagnosisResult{}, ValidationError{Message: "必须先核对并确认 SSH 主机指纹"}
	}
	work, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := dial(work, req, func(key ssh.PublicKey) error {
		actual := ssh.FingerprintSHA256(key)
		if actual != req.HostKeyFingerprint {
			return ValidationError{Message: "SSH 主机指纹已变化，请重新核对"}
		}
		return nil
	})
	if err != nil {
		return DiagnosisResult{}, fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer client.Close()
	hostname, err := runBounded(work, client, "hostname")
	if err != nil {
		return DiagnosisResult{}, fmt.Errorf("读取主机名失败: %w", err)
	}
	hostname = strings.TrimSpace(hostname)
	if hostname != host.Hostname {
		return DiagnosisResult{}, ValidationError{Message: "SSH 目标主机名与纳管主机不一致，请核对连接地址"}
	}
	processes, processErr := runBounded(work, client, "ps -eo comm=")
	listeners, listenerErr := runBounded(work, client, "ss -ltnH")
	result := DiagnosisResult{HostID: host.ID, Hostname: hostname, Address: req.Address, CheckedAt: time.Now().UTC(), Instances: make([]InstanceDiagnosis, 0, len(instances)), Limitation: "SSH 仅检查进程、systemd 和监听端口；不能代替数据库登录、SQL 查询或复制健康检查。"}
	for _, inst := range instances {
		item := InstanceDiagnosis{InstanceID: inst.ID, Name: inst.Name, Engine: inst.DBType, Port: inst.Port, Conclusion: "indeterminate"}
		meta := map[string]any{}
		_ = json.Unmarshal([]byte(inst.MetadataJSON), &meta)
		if listenerErr == nil {
			seen := listenerHasPort(listeners, inst.Port)
			item.PortListening = &seen
		}
		if inst.DBType == "mysql" {
			service, _ := meta["service_name"].(string)
			if service == "" && inst.ManagedMode == "installed" {
				service = fmt.Sprintf("dbops-mysql%d.service", inst.Port)
			}
			if safeUnitName.MatchString(service) {
				item.ServiceName = service
				output, runErr := runBounded(work, client, "systemctl is-active -- "+service)
				if runErr == nil || strings.TrimSpace(output) == "inactive" || strings.TrimSpace(output) == "failed" {
					active := strings.TrimSpace(output) == "active"
					item.ServiceActive = &active
				}
			}
		} else if inst.DBType == "oracle" && processErr == nil {
			sid, _ := meta["oracle_sid"].(string)
			if sid != "" {
				seen := processHasName(processes, "ora_pmon_"+sid)
				item.ProcessSeen = &seen
			}
		}
		item.Conclusion = diagnosisConclusion(item)
		result.Instances = append(result.Instances, item)
	}
	return result, nil
}

func runBounded(ctx context.Context, client *ssh.Client, command string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	type commandResult struct {
		output []byte
		err    error
	}
	done := make(chan commandResult, 1)
	go func() {
		output, err := session.CombinedOutput(command)
		done <- commandResult{output: output, err: err}
	}()
	select {
	case result := <-done:
		return string(result.output), result.err
	case <-ctx.Done():
		_ = session.Close()
		return "", ctx.Err()
	}
}

func listenerHasPort(output string, port int) bool {
	if port < 1 || port > 65535 {
		return false
	}
	needle := fmt.Sprintf(":%d", port)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.HasSuffix(fields[3], needle) {
			return true
		}
	}
	return false
}

func processHasName(output, name string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

func diagnosisConclusion(item InstanceDiagnosis) string {
	if item.ServiceActive != nil && *item.ServiceActive || item.ProcessSeen != nil && *item.ProcessSeen {
		return "process_running"
	}
	if item.PortListening != nil && *item.PortListening {
		return "port_listening"
	}
	if item.ServiceActive != nil && !*item.ServiceActive || item.ProcessSeen != nil && !*item.ProcessSeen {
		return "process_not_observed"
	}
	return "indeterminate"
}
