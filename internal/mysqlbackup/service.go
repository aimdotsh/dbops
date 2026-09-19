package mysqlbackup

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

type CreateRequest struct {
	InstanceID   int64    `json:"instance_id"`
	AllDatabases bool     `json:"all_databases"`
	Databases    []string `json:"databases,omitempty"`
	OutputDir    string   `json:"output_dir"`
	FileName     string   `json:"file_name,omitempty"`
}

type taskParams struct {
	InstanceID   int64    `json:"instance_id"`
	AllDatabases bool     `json:"all_databases"`
	Databases    []string `json:"databases,omitempty"`
	OutputDir    string   `json:"output_dir"`
	FileName     string   `json:"file_name,omitempty"`
}

type runtime struct {
	Instance domain.DatabaseInstance
	Agent    domain.Agent
	BaseDir  string
	RunDir   string
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
	return &Service{agents: agents, dbs: dbs, credentials: credentials, backups: backups, tasks: tasks, cipher: cipher, dispatcher: dispatcher}
}

func (s *Service) CreateTask(ctx context.Context, req CreateRequest) (domain.Task, error) {
	rt, err := s.loadRuntime(ctx, req.InstanceID)
	if err != nil {
		return domain.Task{}, err
	}
	if rt.Agent.Status != "online" {
		return domain.Task{}, errors.New("agent is offline")
	}
	if !req.AllDatabases && len(req.Databases) == 0 {
		return domain.Task{}, errors.New("databases is required when all_databases=false")
	}
	if req.OutputDir == "" {
		req.OutputDir = fmt.Sprintf("/data/dbops-backup/mysql/%d", req.InstanceID)
	}
	if !filepath.IsAbs(req.OutputDir) || filepath.Clean(req.OutputDir) == "/" || strings.ContainsAny(req.OutputDir, "\r\n\x00") {
		return domain.Task{}, errors.New("output_dir must be an absolute non-root path")
	}
	for _, db := range req.Databases {
		if !validDatabaseName(db) {
			return domain.Task{}, fmt.Errorf("invalid database name %q", db)
		}
	}
	if req.FileName != "" && (filepath.Base(req.FileName) != req.FileName || strings.ContainsAny(req.FileName, "/\\\r\n\x00")) {
		return domain.Task{}, errors.New("invalid file_name")
	}

	raw, _ := json.Marshal(taskParams(req))
	key := fmt.Sprintf("mysql.backup:%d", req.InstanceID)
	target := req.InstanceID
	agentID := rt.Agent.ID
	return s.tasks.Create(ctx, domain.Task{
		TaskType: "mysql.backup", TargetType: "database", TargetID: &target,
		AgentID: &agentID, ParametersJSON: string(raw), IdempotencyKey: &key,
	})
}

func (s *Service) List(ctx context.Context, instanceID int64) ([]domain.BackupJob, error) {
	return s.backups.List(ctx, instanceID)
}

func (s *Service) Handler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		var p taskParams
		if err := json.Unmarshal([]byte(t.ParametersJSON), &p); err != nil {
			return nil, err
		}
		rt, err := s.loadRuntime(ctx, p.InstanceID)
		if err != nil {
			return nil, err
		}

		job, err := s.backups.Create(ctx, domain.BackupJob{
			TaskID: t.ID, DatabaseInstanceID: p.InstanceID,
			BackupEngine: "mysqldump", BackupType: "logical", Status: "pending",
		})
		if err != nil {
			return nil, err
		}
		_ = s.backups.MarkRunning(ctx, job.ID)
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 1, StepCode: "BACKUP_PRECHECK", StepName: "Backup precheck",
			Status: "success", Progress: 10, OutputJSON: `{"engine":"mysqldump"}`, RecoveryPolicy: "verify_before_retry",
		})

		policy, _ := actionpolicy.Get("mysql.backup")
		resp, err := s.dispatcher.Dispatch(ctx, rt.Agent.ID, agentproto.ActionRequest{
			TaskID: 0, Action: "mysql.backup", Risk: string(policy.Risk),
			ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 86400,
			Params: map[string]any{
				"base_dir": rt.BaseDir, "run_dir": rt.RunDir, "root_password": rt.Password,
				"output_dir": p.OutputDir, "file_name": p.FileName,
				"all_databases": p.AllDatabases, "databases": p.Databases,
			},
		})
		if err != nil {
			_ = s.backups.MarkFailed(ctx, job.ID, err.Error())
			_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
				TaskID: t.ID, StepNo: 2, StepCode: "MYSQLDUMP", StepName: "Run mysqldump",
				Status: "failed", Progress: 60, ErrorMessage: err.Error(), RecoveryPolicy: "safe_retry",
			})
			return nil, err
		}

		result, ok := resp.Result.(map[string]any)
		if !ok {
			err := errors.New("invalid backup agent result")
			_ = s.backups.MarkFailed(ctx, job.ID, err.Error())
			return nil, err
		}
		path, _ := result["path"].(string)
		checksum, _ := result["sha256"].(string)
		size := int64Value(result["size_bytes"])
		if path == "" || len(checksum) != 64 || size <= 0 {
			err := fmt.Errorf("backup verification failed: path=%q size=%d checksum_len=%d", path, size, len(checksum))
			_ = s.backups.MarkFailed(ctx, job.ID, err.Error())
			return nil, err
		}
		payload, _ := json.Marshal(result)
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 2, StepCode: "MYSQLDUMP", StepName: "Run mysqldump",
			Status: "success", Progress: 70, OutputJSON: security.RedactJSON(string(payload)), RecoveryPolicy: "safe_retry",
		})
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 3, StepCode: "CHECKSUM_VERIFY", StepName: "Verify checksum metadata",
			Status: "success", Progress: 90, OutputJSON: fmt.Sprintf(`{"sha256":%q,"size_bytes":%d}`, checksum, size), RecoveryPolicy: "verify_before_retry",
		})
		if err := s.backups.MarkSuccess(ctx, job.ID, size, path, checksum, string(payload)); err != nil {
			return nil, err
		}
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 4, StepCode: "REGISTER_BACKUP", StepName: "Register backup metadata",
			Status: "success", Progress: 100, OutputJSON: fmt.Sprintf(`{"backup_job_id":%d}`, job.ID), RecoveryPolicy: "verify_before_retry",
		})
		return map[string]any{
			"backup_job_id": job.ID, "engine": "mysqldump", "backup_type": "logical",
			"path": path, "size_bytes": size, "sha256": checksum, "status": "success",
		}, nil
	}
}

func (s *Service) loadRuntime(ctx context.Context, id int64) (runtime, error) {
	inst, err := s.dbs.Get(ctx, id)
	if err != nil {
		return runtime{}, err
	}
	if inst.DBType != "mysql" || inst.CredentialID == nil {
		return runtime{}, errors.New("instance is not a managed MySQL installation")
	}
	agent, err := s.agents.GetByHostID(ctx, inst.HostID)
	if err != nil {
		return runtime{}, err
	}
	cred, err := s.credentials.Get(ctx, *inst.CredentialID)
	if err != nil {
		return runtime{}, err
	}
	password, err := s.cipher.DecryptString(cred.EncryptedSecret)
	if err != nil {
		return runtime{}, err
	}
	var meta struct {
		InstallResult struct {
			BaseDir string `json:"base_dir"`
			RunDir  string `json:"run_dir"`
		} `json:"install_result"`
	}
	if err := json.Unmarshal([]byte(inst.MetadataJSON), &meta); err != nil {
		return runtime{}, err
	}
	if meta.InstallResult.BaseDir == "" || meta.InstallResult.RunDir == "" {
		return runtime{}, errors.New("instance runtime metadata is incomplete")
	}
	return runtime{Instance: inst, Agent: agent, BaseDir: meta.InstallResult.BaseDir, RunDir: meta.InstallResult.RunDir, Password: password}, nil
}

func validDatabaseName(v string) bool {
	if v == "" || len(v) > 64 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '$' {
			continue
		}
		return false
	}
	return true
}

func int64Value(v any) int64 {
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
