package mysqlinstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/aimdotsh/dbops/internal/actionpolicy"
	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/aimdotsh/dbops/internal/security"
	"github.com/aimdotsh/dbops/internal/software"
)

type AgentDispatcher interface {
	Dispatch(context.Context, int64, agentproto.ActionRequest) (agentproto.ActionResponse, error)
}

type Service struct {
	agents           repository.AgentRepository
	dbs              repository.DatabaseRepository
	packages         repository.SoftwarePackageRepository
	credentials      repository.CredentialRepository
	serverIDs        repository.ServerIDRepository
	tasks            repository.TaskRepository
	software         *software.Service
	cipher           *security.Cipher
	dispatcher       AgentDispatcher
	allowProcessMode bool
}

type InstallRequest struct {
	AgentID               int64  `json:"agent_id"`
	PackageID             int64  `json:"package_id"`
	Name                  string `json:"name"`
	Port                  int    `json:"port"`
	BaseDir               string `json:"base_dir"`
	DataDir               string `json:"data_dir"`
	LogDir                string `json:"log_dir"`
	BinlogDir             string `json:"binlog_dir"`
	RunDir                string `json:"run_dir"`
	ConfigPath            string `json:"config_path"`
	ServiceName           string `json:"service_name"`
	ServiceMode           string `json:"service_mode"`
	MySQLUser             string `json:"mysql_user"`
	ManageOSUser          bool   `json:"manage_os_user"`
	Collation             string `json:"collation"`
	InnoDBBufferPoolBytes int64  `json:"innodb_buffer_pool_bytes"`
	MaxConnections        int    `json:"max_connections"`
	LongQueryTime         string `json:"long_query_time"`
	MinFreeBytes          uint64 `json:"min_free_bytes"`
	MinMemoryBytes        uint64 `json:"min_memory_bytes"`
	MinCPUCores           int    `json:"min_cpu_cores"`
}

func New(
	agents repository.AgentRepository,
	dbs repository.DatabaseRepository,
	packages repository.SoftwarePackageRepository,
	credentials repository.CredentialRepository,
	serverIDs repository.ServerIDRepository,
	tasks repository.TaskRepository,
	softwareService *software.Service,
	cipher *security.Cipher,
	dispatcher AgentDispatcher,
	allowProcessMode bool,
) *Service {
	return &Service{
		agents: agents, dbs: dbs, packages: packages, credentials: credentials,
		serverIDs: serverIDs, tasks: tasks, software: softwareService, cipher: cipher,
		dispatcher: dispatcher, allowProcessMode: allowProcessMode,
	}
}

func (s *Service) CreateTask(ctx context.Context, req InstallRequest) (domain.Task, error) {
	agent, pkg, normalized, err := s.validateRequest(ctx, req)
	if err != nil {
		return domain.Task{}, err
	}
	if agent.HostID == nil {
		return domain.Task{}, errors.New("agent is not bound to a host")
	}

	raw, err := json.Marshal(normalized)
	if err != nil {
		return domain.Task{}, err
	}
	key := fmt.Sprintf("mysql.install:%d:%d", *agent.HostID, normalized.Port)
	task := domain.Task{
		TaskType:       "mysql.install",
		TargetType:     "host",
		TargetID:       agent.HostID,
		AgentID:        &normalized.AgentID,
		ParametersJSON: string(raw),
		IdempotencyKey: &key,
	}
	created, err := s.tasks.Create(ctx, task)
	if err != nil {
		return domain.Task{}, err
	}
	_ = s.tasks.AddEvent(ctx, domain.TaskEvent{
		TaskID: created.ID, EventType: "created", Level: "INFO",
		Message:     "MySQL installation task created",
		PayloadJSON: fmt.Sprintf(`{"package_id":%d,"version":%q,"port":%d}`, pkg.ID, pkg.Version, normalized.Port),
	})
	return created, nil
}

func (s *Service) Handler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		if t.AgentID == nil {
			return nil, errors.New("mysql.install requires agent_id")
		}
		var req InstallRequest
		if err := json.Unmarshal([]byte(t.ParametersJSON), &req); err != nil {
			return nil, err
		}
		agent, pkg, req, err := s.validateRequest(ctx, req)
		if err != nil {
			return nil, err
		}
		if agent.HostID == nil {
			return nil, errors.New("agent is not bound to a host")
		}

		reservation, err := s.serverIDs.Reserve(ctx, t.ID, *agent.HostID, req.Port)
		if err != nil {
			return nil, fmt.Errorf("allocate server_id: %w", err)
		}

		rootPassword, err := security.RandomPassword(32)
		if err != nil {
			_ = s.serverIDs.MarkFailed(ctx, reservation.ID)
			return nil, err
		}
		encrypted, err := s.cipher.EncryptString(rootPassword)
		if err != nil {
			_ = s.serverIDs.MarkFailed(ctx, reservation.ID)
			return nil, err
		}
		credential, err := s.credentials.Create(ctx, domain.Credential{
			Name:            fmt.Sprintf("%s root", req.Name),
			CredentialType:  "mysql_root",
			Username:        "root",
			EncryptedSecret: encrypted,
			MetadataJSON:    fmt.Sprintf(`{"host_id":%d,"port":%d}`, *agent.HostID, req.Port),
		})
		if err != nil {
			_ = s.serverIDs.MarkFailed(ctx, reservation.ID)
			return nil, err
		}

		configText, err := RenderConfig(ConfigValues{
			Port: req.Port, ServerID: reservation.ServerID,
			BaseDir: req.BaseDir, DataDir: req.DataDir, LogDir: req.LogDir,
			BinlogDir: req.BinlogDir, RunDir: req.RunDir,
			InnoDBBufferPoolBytes: req.InnoDBBufferPoolBytes,
			MaxConnections:        req.MaxConnections, LongQueryTime: req.LongQueryTime,
			Collation: req.Collation,
		})
		if err != nil {
			_ = s.serverIDs.MarkFailed(ctx, reservation.ID)
			return nil, err
		}

		policy, _ := actionpolicy.Get("mysql.install")
		actionParams := map[string]any{
			"execute":          true,
			"package_url":      s.software.SignedDownloadURL(pkg.ID, 2*time.Hour),
			"package_sha256":   pkg.SHA256,
			"package_type":     pkg.PackageType,
			"version":          pkg.Version,
			"expected_os":      pkg.OSFamily,
			"expected_arch":    pkg.Architecture,
			"port":             req.Port,
			"server_id":        reservation.ServerID,
			"base_dir":         req.BaseDir,
			"data_dir":         req.DataDir,
			"log_dir":          req.LogDir,
			"binlog_dir":       req.BinlogDir,
			"run_dir":          req.RunDir,
			"config_path":      req.ConfigPath,
			"config_text":      configText,
			"service_name":     req.ServiceName,
			"service_mode":     req.ServiceMode,
			"mysql_user":       req.MySQLUser,
			"manage_os_user":   req.ManageOSUser,
			"root_password":    rootPassword,
			"min_free_bytes":   req.MinFreeBytes,
			"min_memory_bytes": req.MinMemoryBytes,
			"min_cpu_cores":    req.MinCPUCores,
			"credential_id":    credential.ID,
		}

		resp, dispatchErr := s.dispatcher.Dispatch(ctx, *t.AgentID, agentproto.ActionRequest{
			TaskID: t.ID, Action: "mysql.install", Risk: string(policy.Risk),
			ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 3600, Params: actionParams,
		})
		if dispatchErr != nil {
			_ = s.serverIDs.MarkFailed(ctx, reservation.ID)
			return nil, dispatchErr
		}

		metadata, _ := json.Marshal(map[string]any{
			"server_id":      reservation.ServerID,
			"package_id":     pkg.ID,
			"service_name":   req.ServiceName,
			"service_mode":   req.ServiceMode,
			"install_result": resp.Result,
		})
		instance, err := s.dbs.CreateInstalled(ctx, domain.DatabaseInstance{
			Name: req.Name, DBType: "mysql", Version: pkg.Version,
			HostID: *agent.HostID, Port: req.Port, Role: "standalone",
			DataDir: req.DataDir, ConfigPath: req.ConfigPath, CredentialID: &credential.ID,
			Status: "online", ManagedMode: "installed", MetadataJSON: string(metadata),
		})
		if err != nil {
			_ = s.serverIDs.MarkFailed(ctx, reservation.ID)
			return nil, fmt.Errorf("register installed instance: %w", err)
		}
		if err := s.serverIDs.BindInstance(ctx, reservation.ID, instance.ID); err != nil {
			return nil, fmt.Errorf("bind server_id: %w", err)
		}

		instanceOutput, _ := json.Marshal(map[string]any{"instance_id": instance.ID, "host_id": instance.HostID, "port": instance.Port})
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 19, StepCode: "REGISTER_INSTANCE", StepName: "Register instance",
			Status: "success", Progress: 96, OutputJSON: string(instanceOutput), RecoveryPolicy: "verify_before_retry",
		})
		_ = s.tasks.AddEvent(ctx, domain.TaskEvent{
			TaskID: t.ID, EventType: "server_step", StepCode: "REGISTER_INSTANCE", Level: "INFO",
			Message: "database instance registered", PayloadJSON: string(instanceOutput),
		})
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 20, StepCode: "ENABLE_METRICS", StepName: "Enable metrics",
			Status: "success", Progress: 98, OutputJSON: `{"discovery":"database_instances"}`, RecoveryPolicy: "verify_before_retry",
		})
		verified, verifyErr := s.dbs.Get(ctx, instance.ID)
		if verifyErr != nil || verified.Status != "online" {
			if verifyErr != nil {
				return nil, fmt.Errorf("final verify: %w", verifyErr)
			}
			return nil, fmt.Errorf("final verify: instance status is %s", verified.Status)
		}
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 21, StepCode: "FINAL_VERIFY", StepName: "Final verify",
			Status: "success", Progress: 100, OutputJSON: `{"verified":true}`, RecoveryPolicy: "verify_before_retry",
		})

		return map[string]any{
			"instance_id":   instance.ID,
			"name":          instance.Name,
			"version":       instance.Version,
			"host_id":       instance.HostID,
			"port":          instance.Port,
			"server_id":     reservation.ServerID,
			"credential_id": credential.ID,
			"status":        "online",
		}, nil
	}
}

func (s *Service) validateRequest(ctx context.Context, req InstallRequest) (domain.Agent, domain.SoftwarePackage, InstallRequest, error) {
	if req.AgentID <= 0 || req.PackageID <= 0 {
		return domain.Agent{}, domain.SoftwarePackage{}, req, errors.New("agent_id and package_id are required")
	}
	agent, err := s.agents.Get(ctx, req.AgentID)
	if err != nil {
		return domain.Agent{}, domain.SoftwarePackage{}, req, err
	}
	if agent.Status != "online" {
		return agent, domain.SoftwarePackage{}, req, errors.New("agent is offline")
	}
	pkg, err := s.packages.Get(ctx, req.PackageID)
	if err != nil {
		return agent, domain.SoftwarePackage{}, req, err
	}
	if strings.ToLower(pkg.SoftwareName) != "mysql" || pkg.Status != "available" {
		return agent, pkg, req, errors.New("selected software package is not an available MySQL package")
	}
	if pkg.OSFamily != "linux" {
		return agent, pkg, req, fmt.Errorf("unsupported MySQL package OS %q", pkg.OSFamily)
	}
	if pkg.Architecture != "" && agent.Architecture != "" && pkg.Architecture != strings.ToLower(agent.Architecture) {
		return agent, pkg, req, fmt.Errorf("package architecture %s does not match agent %s", pkg.Architecture, agent.Architecture)
	}

	if req.Port == 0 {
		req.Port = 3306
	}
	if req.Port < 1 || req.Port > 65535 {
		return agent, pkg, req, errors.New("invalid MySQL port")
	}
	if req.Name == "" {
		req.Name = fmt.Sprintf("mysql-%d", req.Port)
	}
	if req.BaseDir == "" {
		req.BaseDir = fmt.Sprintf("/opt/dbops/mysql/%s-%d", safeVersion(pkg.Version), req.Port)
	}
	if req.DataDir == "" {
		req.DataDir = fmt.Sprintf("/data/mysql/%d/data", req.Port)
	}
	if req.LogDir == "" {
		req.LogDir = fmt.Sprintf("/data/mysql/%d/log", req.Port)
	}
	if req.BinlogDir == "" {
		req.BinlogDir = fmt.Sprintf("/data/mysql/%d/binlog", req.Port)
	}
	if req.RunDir == "" {
		req.RunDir = fmt.Sprintf("/data/mysql/%d/run", req.Port)
	}
	if req.ConfigPath == "" {
		req.ConfigPath = fmt.Sprintf("/etc/dbops/mysql/%d/my.cnf", req.Port)
	}
	if req.ServiceName == "" {
		req.ServiceName = fmt.Sprintf("dbops-mysql-%d", req.Port)
	}
	if req.ServiceMode == "" {
		req.ServiceMode = "systemd"
	}
	if req.ServiceMode != "systemd" && !(req.ServiceMode == "process" && s.allowProcessMode) {
		return agent, pkg, req, fmt.Errorf("service_mode %q is not allowed", req.ServiceMode)
	}
	if req.MySQLUser == "" {
		req.MySQLUser = "mysql"
	}
	if req.Collation == "" {
		req.Collation = "utf8mb4_0900_ai_ci"
	}
	if req.InnoDBBufferPoolBytes <= 0 {
		req.InnoDBBufferPoolBytes = 1 << 30
	}
	if req.MaxConnections <= 0 {
		req.MaxConnections = 1000
	}
	if req.LongQueryTime == "" {
		req.LongQueryTime = "1"
	}
	if req.MinCPUCores <= 0 {
		req.MinCPUCores = 1
	}
	for name, path := range map[string]string{
		"base_dir": req.BaseDir, "data_dir": req.DataDir, "log_dir": req.LogDir,
		"binlog_dir": req.BinlogDir, "run_dir": req.RunDir, "config_path": req.ConfigPath,
	} {
		if err := validateAbsolutePath(name, path); err != nil {
			return agent, pkg, req, err
		}
	}
	if !validIdentifier(req.ServiceName) || !validIdentifier(req.MySQLUser) {
		return agent, pkg, req, errors.New("invalid service_name or mysql_user")
	}
	return agent, pkg, req, nil
}

func validateAbsolutePath(name, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) == "/" {
		return fmt.Errorf("%s must be an absolute non-root path", name)
	}
	if strings.ContainsAny(path, "\r\n\x00") {
		return fmt.Errorf("%s contains invalid characters", name)
	}
	return nil
}

func validIdentifier(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '@' {
			continue
		}
		return false
	}
	return true
}

func safeVersion(v string) string {
	v = strings.TrimSpace(v)
	var b strings.Builder
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
