package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aimdotsh/dbops/internal/actionpolicy"
	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/aimdotsh/dbops/internal/security"
)

type AgentDispatcher interface {
	Dispatch(context.Context, int64, agentproto.ActionRequest) (agentproto.ActionResponse, error)
}

type Service struct {
	agents      repository.AgentRepository
	dbs         repository.DatabaseRepository
	credentials repository.CredentialRepository
	backups     repository.BackupJobRepository
	tasks       repository.TaskRepository
	cipher      *security.Cipher
	dispatcher  AgentDispatcher
}

type OnboardRequest struct {
	AgentID  int64  `json:"agent_id"`
	Name     string `json:"name"`
	BinDir   string `json:"bin_dir"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type BackupRequest struct {
	OutputDir string `json:"output_dir"`
	FileName  string `json:"file_name,omitempty"`
}

type metadata struct {
	BinDir   string `json:"bin_dir"`
	Host     string `json:"host"`
	Database string `json:"database"`
}

type runtime struct {
	Instance domain.DatabaseInstance
	Agent    domain.Agent
	Meta     metadata
	Username string
	Password string
}

func New(
	agents repository.AgentRepository,
	dbs repository.DatabaseRepository,
	credentials repository.CredentialRepository,
	backups repository.BackupJobRepository,
	tasks repository.TaskRepository,
	cipher *security.Cipher,
	dispatcher AgentDispatcher,
) *Service {
	return &Service{
		agents: agents, dbs: dbs, credentials: credentials,
		backups: backups, tasks: tasks, cipher: cipher, dispatcher: dispatcher,
	}
}

func (s *Service) Onboard(ctx context.Context, req OnboardRequest) (domain.DatabaseInstance, error) {
	if req.AgentID <= 0 {
		return domain.DatabaseInstance{}, errors.New("agent_id is required")
	}
	agent, err := s.agents.Get(ctx, req.AgentID)
	if err != nil {
		return domain.DatabaseInstance{}, err
	}
	if agent.Status != "online" || agent.HostID == nil {
		return domain.DatabaseInstance{}, errors.New("agent must be online and bound to a host")
	}
	if req.Name == "" {
		req.Name = req.Database
	}
	if req.Port == 0 {
		req.Port = 5432
	}
	if req.Port < 1 || req.Port > 65535 {
		return domain.DatabaseInstance{}, errors.New("invalid PostgreSQL port")
	}
	if req.BinDir == "" || !filepath.IsAbs(req.BinDir) || filepath.Clean(req.BinDir) == "/" {
		return domain.DatabaseInstance{}, errors.New("bin_dir must be an absolute non-root path")
	}
	if !safeHost(req.Host) || !validName(req.Database) || !validName(req.Username) {
		return domain.DatabaseInstance{}, errors.New("invalid PostgreSQL connection parameters")
	}
	if req.Password == "" || strings.ContainsAny(req.Password, "\r\n\x00") {
		return domain.DatabaseInstance{}, errors.New("invalid PostgreSQL password")
	}
	params := map[string]any{
		"bin_dir": req.BinDir, "host": req.Host, "port": req.Port,
		"database": req.Database, "username": req.Username, "password": req.Password,
	}
	policy, _ := actionpolicy.Get("postgres.status")
	resp, err := s.dispatcher.Dispatch(ctx, agent.ID, agentproto.ActionRequest{
		TaskID: 0, Action: "postgres.status", Risk: string(policy.Risk),
		ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 60, Params: params,
	})
	if err != nil {
		return domain.DatabaseInstance{}, fmt.Errorf("PostgreSQL validation failed: %w", err)
	}
	status, ok := resp.Result.(map[string]any)
	if !ok {
		return domain.DatabaseInstance{}, errors.New("invalid PostgreSQL status response")
	}
	version, _ := status["version"].(string)
	role, _ := status["role"].(string)

	encrypted, err := s.cipher.EncryptString(req.Password)
	if err != nil {
		return domain.DatabaseInstance{}, err
	}
	cred, err := s.credentials.Create(ctx, domain.Credential{
		Name: req.Name + " PostgreSQL admin", CredentialType: "postgres_admin",
		Username: req.Username, EncryptedSecret: encrypted,
		MetadataJSON: fmt.Sprintf("{\"host_id\":%d,\"database\":%q}", *agent.HostID, req.Database),
	})
	if err != nil {
		return domain.DatabaseInstance{}, err
	}
	raw, _ := json.Marshal(metadata{BinDir: req.BinDir, Host: req.Host, Database: req.Database})
	instance, err := s.dbs.CreateImported(ctx, domain.DatabaseInstance{
		Name: req.Name, DBType: "postgresql", Version: version, HostID: *agent.HostID,
		Port: req.Port, Role: role, CredentialID: &cred.ID, Status: "online",
		ManagedMode: "imported", MetadataJSON: string(raw),
	})
	if err != nil {
		_ = s.credentials.Delete(ctx, cred.ID)
		return domain.DatabaseInstance{}, err
	}
	return instance, nil
}

func (s *Service) Status(ctx context.Context, instanceID int64) (map[string]any, error) {
	rt, err := s.loadRuntime(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return s.dispatchRead(ctx, rt, "postgres.status")
}

func (s *Service) ReplicationStatus(ctx context.Context, instanceID int64) (map[string]any, error) {
	rt, err := s.loadRuntime(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return s.dispatchRead(ctx, rt, "postgres.replication.status")
}

func (s *Service) CreateBackupTask(ctx context.Context, instanceID int64, req BackupRequest) (domain.Task, error) {
	if req.OutputDir == "" || !filepath.IsAbs(req.OutputDir) || filepath.Clean(req.OutputDir) == "/" {
		return domain.Task{}, errors.New("output_dir must be an absolute non-root path")
	}
	if strings.ContainsAny(req.OutputDir, "'\"\r\n\x00") {
		return domain.Task{}, errors.New("output_dir contains forbidden characters")
	}
	if req.FileName != "" && (filepath.Base(req.FileName) != req.FileName || strings.ContainsAny(req.FileName, "/\\\r\n\x00")) {
		return domain.Task{}, errors.New("invalid file_name")
	}
	rt, err := s.loadRuntime(ctx, instanceID)
	if err != nil {
		return domain.Task{}, err
	}
	raw, _ := json.Marshal(req)
	target := instanceID
	agentID := rt.Agent.ID
	key := fmt.Sprintf("postgres.backup:%d", instanceID)
	return s.tasks.Create(ctx, domain.Task{
		TaskType: "postgres.backup", TargetType: "database", TargetID: &target,
		AgentID: &agentID, ParametersJSON: string(raw), IdempotencyKey: &key,
	})
}

func (s *Service) ListBackups(ctx context.Context, instanceID int64) ([]domain.BackupJob, error) {
	return s.backups.List(ctx, instanceID)
}

func (s *Service) BackupHandler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		if t.TargetID == nil {
			return nil, errors.New("PostgreSQL backup target is missing")
		}
		var req BackupRequest
		if err := json.Unmarshal([]byte(t.ParametersJSON), &req); err != nil {
			return nil, err
		}
		rt, err := s.loadRuntime(ctx, *t.TargetID)
		if err != nil {
			return nil, err
		}
		job, err := s.backups.Create(ctx, domain.BackupJob{
			TaskID: t.ID, DatabaseInstanceID: *t.TargetID,
			BackupEngine: "pg_dump", BackupType: "logical", Status: "pending",
		})
		if err != nil {
			return nil, err
		}
		_ = s.backups.MarkRunning(ctx, job.ID)
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 1, StepCode: "PG_BACKUP_PRECHECK", StepName: "Validate PostgreSQL backup",
			Status: "success", Progress: 10, OutputJSON: "{}", RecoveryPolicy: "verify_before_retry",
		})

		policy, _ := actionpolicy.Get("postgres.backup")
		resp, err := s.dispatcher.Dispatch(ctx, rt.Agent.ID, agentproto.ActionRequest{
			TaskID: 0, Action: "postgres.backup", Risk: string(policy.Risk),
			ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 86400,
			Params: s.params(rt, map[string]any{
				"output_dir": req.OutputDir, "file_name": req.FileName,
			}),
		})
		if err != nil {
			_ = s.backups.MarkFailed(ctx, job.ID, err.Error())
			return nil, err
		}
		result, ok := resp.Result.(map[string]any)
		if !ok {
			err := errors.New("invalid PostgreSQL backup response")
			_ = s.backups.MarkFailed(ctx, job.ID, err.Error())
			return nil, err
		}
		path, _ := result["path"].(string)
		checksum, _ := result["sha256"].(string)
		size := number(result["size_bytes"])
		if path == "" || size <= 0 || len(checksum) != 64 {
			err := errors.New("PostgreSQL backup verification failed")
			_ = s.backups.MarkFailed(ctx, job.ID, err.Error())
			return nil, err
		}
		payload, _ := json.Marshal(result)
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 2, StepCode: "PG_DUMP", StepName: "Run pg_dump",
			Status: "success", Progress: 85, OutputJSON: security.RedactJSON(string(payload)),
			RecoveryPolicy: "safe_retry",
		})
		if err := s.backups.MarkSuccess(ctx, job.ID, size, path, checksum, string(payload)); err != nil {
			return nil, err
		}
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 3, StepCode: "PG_BACKUP_VERIFY", StepName: "Verify PostgreSQL backup",
			Status: "success", Progress: 100,
			OutputJSON:     fmt.Sprintf("{\"backup_job_id\":%d,\"sha256\":%q,\"size_bytes\":%d}", job.ID, checksum, size),
			RecoveryPolicy: "verify_before_retry",
		})
		return map[string]any{
			"backup_job_id": job.ID, "engine": "pg_dump", "backup_type": "logical",
			"path": path, "size_bytes": size, "sha256": checksum, "status": "success",
		}, nil
	}
}

func (s *Service) dispatchRead(ctx context.Context, rt runtime, action string) (map[string]any, error) {
	policy, _ := actionpolicy.Get(action)
	resp, err := s.dispatcher.Dispatch(ctx, rt.Agent.ID, agentproto.ActionRequest{
		TaskID: 0, Action: action, Risk: string(policy.Risk),
		ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 60,
		Params: s.params(rt, nil),
	})
	if err != nil {
		return nil, err
	}
	out, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, errors.New("invalid PostgreSQL response")
	}
	return out, nil
}

func (s *Service) loadRuntime(ctx context.Context, instanceID int64) (runtime, error) {
	inst, err := s.dbs.Get(ctx, instanceID)
	if err != nil {
		return runtime{}, err
	}
	if inst.DBType != "postgresql" || inst.CredentialID == nil {
		return runtime{}, errors.New("instance is not a managed PostgreSQL database")
	}
	agent, err := s.agents.GetByHostID(ctx, inst.HostID)
	if err != nil {
		return runtime{}, err
	}
	if agent.Status != "online" {
		return runtime{}, errors.New("PostgreSQL Agent is offline")
	}
	cred, err := s.credentials.Get(ctx, *inst.CredentialID)
	if err != nil {
		return runtime{}, err
	}
	password, err := s.cipher.DecryptString(cred.EncryptedSecret)
	if err != nil {
		return runtime{}, err
	}
	var meta metadata
	if err := json.Unmarshal([]byte(inst.MetadataJSON), &meta); err != nil {
		return runtime{}, err
	}
	if meta.BinDir == "" || meta.Host == "" || meta.Database == "" {
		return runtime{}, errors.New("PostgreSQL runtime metadata is incomplete")
	}
	return runtime{
		Instance: inst, Agent: agent, Meta: meta,
		Username: cred.Username, Password: password,
	}, nil
}

func (s *Service) params(rt runtime, extra map[string]any) map[string]any {
	out := map[string]any{
		"bin_dir": rt.Meta.BinDir, "host": rt.Meta.Host, "port": rt.Instance.Port,
		"database": rt.Meta.Database, "username": rt.Username, "password": rt.Password,
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func validName(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func safeHost(v string) bool {
	if v == "" || len(v) > 255 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == ':' {
			continue
		}
		return false
	}
	return true
}

func number(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	case json.Number:
		n, _ := x.Int64()
		return n
	}
	return 0
}
