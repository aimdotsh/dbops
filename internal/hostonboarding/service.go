package hostonboarding

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aimdotsh/dbops/internal/agentclient"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

//go:embed agent-actions.yaml
var agentActions []byte

type Config struct {
	PublicURL      string
	BootstrapToken string
	RequireMTLS    bool
	InstallerRoot  string
}

type Service struct {
	cfg    Config
	agents repository.AgentRepository
}

type Request struct {
	Address              string `json:"address"`
	Port                 int    `json:"port"`
	Username             string `json:"username"`
	AuthType             string `json:"auth_type"`
	Password             string `json:"password"`
	PrivateKey           string `json:"private_key"`
	PrivateKeyPassphrase string `json:"private_key_passphrase"`
	SudoPassword         string `json:"sudo_password"`
	AdvertiseIP          string `json:"advertise_ip"`
	AgentID              string `json:"agent_id"`
	ServerURL            string `json:"server_url"`
	CACertificate        string `json:"ca_certificate"`
	HostKeyFingerprint   string `json:"host_key_fingerprint"`
	Confirmed            bool   `json:"confirmed"`
}

type PrecheckResult struct {
	Address            string `json:"address"`
	Hostname           string `json:"hostname"`
	OperatingSystem    string `json:"operating_system"`
	Architecture       string `json:"architecture"`
	AgentArchitecture  string `json:"agent_architecture"`
	HasSystemd         bool   `json:"has_systemd"`
	CanElevate         bool   `json:"can_elevate"`
	HostKeyFingerprint string `json:"host_key_fingerprint"`
	DefaultServerURL   string `json:"default_server_url"`
}

type OnboardResult struct {
	AgentID          string        `json:"agent_id"`
	Hostname         string        `json:"hostname"`
	Architecture     string        `json:"architecture"`
	ServiceStatus    string        `json:"service_status"`
	Connected        bool          `json:"connected"`
	BootstrapRemoved bool          `json:"bootstrap_removed"`
	Agent            *domain.Agent `json:"agent,omitempty"`
}

type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

func New(cfg Config, agents repository.AgentRepository) *Service {
	if cfg.InstallerRoot == "" {
		cfg.InstallerRoot = "/opt/dbops/installers"
	}
	return &Service{cfg: cfg, agents: agents}
}

func (s *Service) Precheck(ctx context.Context, req Request) (PrecheckResult, error) {
	if err := validateConnection(req); err != nil {
		return PrecheckResult{}, err
	}
	var fingerprint string
	client, err := dial(ctx, req, func(key ssh.PublicKey) error {
		fingerprint = ssh.FingerprintSHA256(key)
		return nil
	})
	if err != nil {
		return PrecheckResult{}, fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer client.Close()
	out, err := run(client, "printf '%s|%s|%s|%s|%s' \"$(uname -s)\" \"$(uname -m)\" \"$(hostname)\" \"$(command -v systemctl || true)\" \"$(if [ \"$(id -u)\" = 0 ]; then echo root; elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then echo passwordless; else echo password; fi)\"", "")
	if err != nil {
		return PrecheckResult{}, fmt.Errorf("读取主机信息失败: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "|")
	if len(lines) < 5 {
		return PrecheckResult{}, errors.New("SSH 主机信息响应不完整")
	}
	arch, err := normalizeArch(lines[1])
	if err != nil {
		return PrecheckResult{}, err
	}
	result := PrecheckResult{
		Address: req.Address, OperatingSystem: strings.TrimSpace(lines[0]), Architecture: strings.TrimSpace(lines[1]),
		Hostname: strings.TrimSpace(lines[2]), HasSystemd: strings.TrimSpace(lines[3]) != "", CanElevate: strings.TrimSpace(lines[4]) != "password" || req.SudoPassword != "" || req.Password != "",
		AgentArchitecture: arch, HostKeyFingerprint: fingerprint, DefaultServerURL: s.cfg.PublicURL,
	}
	if !strings.EqualFold(result.OperatingSystem, "Linux") {
		return PrecheckResult{}, ValidationError{Message: "当前仅支持在线纳管 Linux 主机"}
	}
	if !result.HasSystemd {
		return PrecheckResult{}, ValidationError{Message: "目标主机未检测到 systemd"}
	}
	return result, nil
}

func (s *Service) Onboard(ctx context.Context, req Request) (OnboardResult, error) {
	if err := validateConnection(req); err != nil {
		return OnboardResult{}, err
	}
	if !req.Confirmed || req.HostKeyFingerprint == "" {
		return OnboardResult{}, ValidationError{Message: "必须先完成预检并确认 SSH 主机指纹"}
	}
	if s.cfg.RequireMTLS {
		return OnboardResult{}, ValidationError{Message: "Agent 网关已启用双向 TLS，请先通过离线流程下发客户端证书"}
	}
	if s.cfg.BootstrapToken == "" {
		return OnboardResult{}, errors.New("平台未配置 Agent bootstrap token")
	}
	serverURL := strings.TrimSpace(req.ServerURL)
	if serverURL == "" {
		serverURL = s.cfg.PublicURL
	}
	if err := validateServerURL(serverURL); err != nil {
		return OnboardResult{}, err
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		agentID = uuid.NewString()
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(agentID) {
		return OnboardResult{}, ValidationError{Message: "Agent 标识只能包含字母、数字、点、下划线和短横线"}
	}
	if _, lookupErr := s.agents.GetByUUID(ctx, agentID); lookupErr == nil {
		return OnboardResult{}, ValidationError{Message: "Agent 标识已存在，请留空自动生成或使用新的标识"}
	} else if !errors.Is(lookupErr, sql.ErrNoRows) {
		return OnboardResult{}, fmt.Errorf("检查 Agent 标识失败: %w", lookupErr)
	}
	advertiseIP := strings.TrimSpace(req.AdvertiseIP)
	if advertiseIP == "" {
		advertiseIP = net.ParseIP(req.Address).String()
	}
	if net.ParseIP(advertiseIP) == nil {
		return OnboardResult{}, ValidationError{Message: "请填写目标主机用于注册的有效 IP 地址"}
	}

	var actualFingerprint string
	client, err := dial(ctx, req, func(key ssh.PublicKey) error {
		actualFingerprint = ssh.FingerprintSHA256(key)
		if actualFingerprint != req.HostKeyFingerprint {
			return fmt.Errorf("SSH 主机指纹已变化，期望 %s，实际 %s", req.HostKeyFingerprint, actualFingerprint)
		}
		return nil
	})
	if err != nil {
		return OnboardResult{}, fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer client.Close()

	info, err := s.remoteInfo(client)
	if err != nil {
		return OnboardResult{}, err
	}
	binaryPath := filepath.Join(s.cfg.InstallerRoot, "linux-"+info.arch, "dbops-agent")
	binary, err := os.Open(binaryPath)
	if err != nil {
		return OnboardResult{}, fmt.Errorf("找不到 %s Agent 安装包: %w", info.arch, err)
	}
	defer binary.Close()

	agentCfg := agentclient.Config{}
	agentCfg.Server.URL = strings.TrimRight(serverURL, "/")
	agentCfg.Agent.ID = agentID
	agentCfg.Agent.AdvertiseIP = advertiseIP
	agentCfg.Agent.HeartbeatSeconds = 30
	agentCfg.Agent.WorkDir = "/var/lib/dbops-agent"
	agentCfg.Agent.LogDir = "/var/log/dbops-agent"
	agentCfg.Security.BootstrapTokenFile = "/etc/dbops-agent/bootstrap.token"
	agentCfg.Security.CredentialFile = "/var/lib/dbops-agent/agent.credential"
	agentCfg.Security.VerifyServerTLS = true
	if strings.TrimSpace(req.CACertificate) != "" {
		agentCfg.Security.CAFile = "/etc/dbops-agent/ca.pem"
	}
	agentCfg.Executor.MaxConcurrentTasks = 2
	agentCfg.Executor.AllowedActionsFile = "/etc/dbops-agent/actions.yaml"
	cfgBytes, err := yaml.Marshal(agentCfg)
	if err != nil {
		return OnboardResult{}, err
	}

	tmp := "/tmp/dbops-onboard-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	files := []struct {
		path string
		data io.Reader
	}{
		{tmp + ".bin", binary}, {tmp + ".yaml", bytes.NewReader(cfgBytes)}, {tmp + ".actions", bytes.NewReader(agentActions)},
		{tmp + ".token", strings.NewReader(s.cfg.BootstrapToken + "\n")},
	}
	if strings.TrimSpace(req.CACertificate) != "" {
		files = append(files, struct {
			path string
			data io.Reader
		}{tmp + ".ca", strings.NewReader(req.CACertificate)})
	}
	for _, file := range files {
		if err := upload(client, file.path, file.data); err != nil {
			return OnboardResult{}, fmt.Errorf("上传 Agent 文件失败: %w", err)
		}
	}
	defer func() { _, _ = run(client, "rm -f "+tmp+".*", "") }()

	unit := `[Unit]
Description=DBOps Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/dbops-agent --config /etc/dbops-agent/agent.yaml
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`
	if err := upload(client, tmp+".service", strings.NewReader(unit)); err != nil {
		return OnboardResult{}, fmt.Errorf("上传 systemd 服务失败: %w", err)
	}

	sudoPassword := req.SudoPassword
	if sudoPassword == "" && req.AuthType == "password" {
		sudoPassword = req.Password
	}
	prefix := ""
	stdin := ""
	if req.Username != "root" {
		prefix = "sudo -S -p '' "
		stdin = sudoPassword + "\n"
	}
	install := prefix + "mkdir -p /etc/dbops-agent /var/lib/dbops-agent /var/log/dbops-agent && " +
		prefix + "install -m 0755 " + tmp + ".bin /usr/local/bin/dbops-agent && " +
		prefix + "install -m 0600 " + tmp + ".yaml /etc/dbops-agent/agent.yaml && " +
		prefix + "install -m 0600 " + tmp + ".token /etc/dbops-agent/bootstrap.token && " +
		prefix + "install -m 0644 " + tmp + ".actions /etc/dbops-agent/actions.yaml && "
	if strings.TrimSpace(req.CACertificate) != "" {
		install += prefix + "install -m 0644 " + tmp + ".ca /etc/dbops-agent/ca.pem && "
	}
	install += prefix + "install -m 0644 " + tmp + ".service /etc/systemd/system/dbops-agent.service && " +
		prefix + "systemctl daemon-reload && " + prefix + "systemctl enable --now dbops-agent.service"
	if out, err := run(client, install, stdin); err != nil {
		return OnboardResult{}, fmt.Errorf("安装或启动 Agent 失败: %w (%s)", err, strings.TrimSpace(out))
	}
	status, _ := run(client, prefix+"systemctl is-active dbops-agent.service", stdin)
	result := OnboardResult{AgentID: agentID, Hostname: info.hostname, Architecture: info.arch, ServiceStatus: strings.TrimSpace(status)}

	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		agent, lookupErr := s.agents.GetByUUID(ctx, agentID)
		if lookupErr == nil {
			result.Connected = agent.Status == "online"
			result.Agent = &agent
			break
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if result.Connected {
		_, cleanupErr := run(client, "test -s /var/lib/dbops-agent/agent.credential && "+prefix+"rm -f /etc/dbops-agent/bootstrap.token", stdin)
		result.BootstrapRemoved = cleanupErr == nil
	}
	return result, nil
}

type remoteDetails struct{ hostname, arch string }

func (s *Service) remoteInfo(client *ssh.Client) (remoteDetails, error) {
	out, err := run(client, "printf '%s|%s|%s|%s' \"$(uname -s)\" \"$(uname -m)\" \"$(hostname)\" \"$(command -v systemctl || true)\"", "")
	if err != nil {
		return remoteDetails{}, fmt.Errorf("读取主机信息失败: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "|")
	if len(lines) < 4 || !strings.EqualFold(lines[0], "Linux") || strings.TrimSpace(lines[3]) == "" {
		return remoteDetails{}, ValidationError{Message: "目标主机必须是使用 systemd 的 Linux"}
	}
	arch, err := normalizeArch(lines[1])
	if err != nil {
		return remoteDetails{}, err
	}
	return remoteDetails{hostname: strings.TrimSpace(lines[2]), arch: arch}, nil
}

func validateConnection(req Request) error {
	if strings.TrimSpace(req.Address) == "" || strings.TrimSpace(req.Username) == "" {
		return ValidationError{Message: "主机地址和 SSH 用户名不能为空"}
	}
	if req.Port < 0 || req.Port > 65535 {
		return ValidationError{Message: "SSH 端口无效"}
	}
	if req.AuthType == "" || req.AuthType == "password" {
		if req.Password == "" {
			return ValidationError{Message: "SSH 密码不能为空"}
		}
		return nil
	}
	if req.AuthType == "private_key" && strings.TrimSpace(req.PrivateKey) == "" {
		return ValidationError{Message: "SSH 私钥不能为空"}
	}
	if req.AuthType != "private_key" {
		return ValidationError{Message: "SSH 认证方式无效"}
	}
	return nil
}

func validateServerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ValidationError{Message: "平台访问地址必须是目标主机可访问的 HTTP 或 HTTPS URL"}
	}
	return nil
}

func normalizeArch(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", ValidationError{Message: "暂不支持目标主机架构: " + strings.TrimSpace(value)}
	}
}

func dial(ctx context.Context, req Request, verify func(ssh.PublicKey) error) (*ssh.Client, error) {
	var auth ssh.AuthMethod
	if req.AuthType == "private_key" {
		var signer ssh.Signer
		var err error
		if req.PrivateKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(req.PrivateKey), []byte(req.PrivateKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(req.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("解析 SSH 私钥失败: %w", err)
		}
		auth = ssh.PublicKeys(signer)
	} else {
		auth = ssh.Password(req.Password)
	}
	port := req.Port
	if port == 0 {
		port = 22
	}
	config := &ssh.ClientConfig{User: req.Username, Auth: []ssh.AuthMethod{auth}, Timeout: 10 * time.Second, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error { return verify(key) }}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(strings.TrimSpace(req.Address), fmt.Sprint(port)))
	if err != nil {
		return nil, err
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, net.JoinHostPort(req.Address, fmt.Sprint(port)), config)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return ssh.NewClient(cc, chans, reqs), nil
}

func run(client *ssh.Client, command, stdin string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}
	var output bytes.Buffer
	session.Stdout, session.Stderr = &output, &output
	err = session.Run(command)
	return output.String(), err
}

func upload(client *ssh.Client, path string, content io.Reader) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = content
	var stderr bytes.Buffer
	session.Stderr = &stderr
	if err := session.Run("cat > " + path); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
