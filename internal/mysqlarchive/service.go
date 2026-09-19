package mysqlarchive

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
	archives    repository.ArchiveJobRepository
	tasks       repository.TaskRepository
	cipher      *security.Cipher
	dispatcher  AgentDispatcher
}

type Request struct {
	InstanceID           int64  `json:"instance_id"`
	SourceDatabase       string `json:"source_database"`
	SourceTable          string `json:"source_table"`
	DestinationDatabase  string `json:"destination_database,omitempty"`
	DestinationTable     string `json:"destination_table,omitempty"`
	Where                string `json:"where"`
	PTArchiverPath       string `json:"pt_archiver_path"`
	BatchSize            int    `json:"batch_size"`
	TxnSize              int    `json:"txn_size"`
	SleepMS              int    `json:"sleep_ms"`
	DeleteSource         bool   `json:"delete_source"`
	Confirmed            bool   `json:"confirmed"`
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
	archives repository.ArchiveJobRepository,
	tasks repository.TaskRepository,
	cipher *security.Cipher,
	dispatcher AgentDispatcher,
) *Service {
	return &Service{agents: agents, dbs: dbs, credentials: credentials, archives: archives, tasks: tasks, cipher: cipher, dispatcher: dispatcher}
}

func (s *Service) Precheck(ctx context.Context, req Request) (map[string]any, error) {
	rt, req, err := s.validate(ctx, req, false)
	if err != nil {
		return nil, err
	}
	policy, _ := actionpolicy.Get("mysql.archive.precheck")
	resp, err := s.dispatcher.Dispatch(ctx, rt.Agent.ID, agentproto.ActionRequest{
		TaskID: 0, Action: "mysql.archive.precheck", Risk: string(policy.Risk),
		ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 600,
		Params: s.actionParams(req, rt),
	})
	if err != nil {
		return nil, err
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, errors.New("invalid archive precheck response")
	}
	return result, nil
}

func (s *Service) CreateTask(ctx context.Context, req Request) (domain.Task, error) {
	rt, req, err := s.validate(ctx, req, true)
	if err != nil {
		return domain.Task{}, err
	}
	raw, _ := json.Marshal(req)
	key := fmt.Sprintf("mysql.archive:%d:%s:%s", req.InstanceID, req.SourceDatabase, req.SourceTable)
	target := req.InstanceID
	agentID := rt.Agent.ID
	return s.tasks.Create(ctx, domain.Task{
		TaskType: "mysql.archive", TargetType: "database", TargetID: &target,
		AgentID: &agentID, ParametersJSON: string(raw), IdempotencyKey: &key,
	})
}

func (s *Service) List(ctx context.Context, instanceID int64) ([]domain.ArchiveJob, error) {
	return s.archives.List(ctx, instanceID)
}

func (s *Service) Handler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		var req Request
		if err := json.Unmarshal([]byte(t.ParametersJSON), &req); err != nil {
			return nil, err
		}
		rt, req, err := s.validate(ctx, req, true)
		if err != nil {
			return nil, err
		}
		job, err := s.archives.Create(ctx, domain.ArchiveJob{
			TaskID: t.ID, SourceInstanceID: req.InstanceID,
			SourceDatabase: req.SourceDatabase, SourceTable: req.SourceTable,
			DestinationDatabase: req.DestinationDatabase, DestinationTable: req.DestinationTable,
		})
		if err != nil {
			return nil, err
		}
		_ = s.archives.MarkRunning(ctx, job.ID)

		precheck, err := s.Precheck(ctx, req)
		if err != nil {
			_ = s.archives.MarkFailed(ctx, job.ID, err.Error())
			return nil, err
		}
		if ok, _ := precheck["ok"].(bool); !ok {
			err := fmt.Errorf("archive precheck blocked: %v", precheck)
			_ = s.archives.MarkFailed(ctx, job.ID, err.Error())
			return nil, err
		}
		prePayload, _ := json.Marshal(precheck)
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 1, StepCode: "ARCHIVE_PRECHECK", StepName: "Archive precheck and dry-run",
			Status: "success", Progress: 20, OutputJSON: security.RedactJSON(string(prePayload)), RecoveryPolicy: "verify_before_retry",
		})

		policy, _ := actionpolicy.Get("mysql.archive.start")
		resp, err := s.dispatcher.Dispatch(ctx, rt.Agent.ID, agentproto.ActionRequest{
			TaskID: 0, Action: "mysql.archive.start", Risk: string(policy.Risk), Confirmed: true,
			ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 86400,
			Params: s.actionParams(req, rt),
		})
		if err != nil {
			_ = s.archives.MarkFailed(ctx, job.ID, err.Error())
			_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
				TaskID: t.ID, StepNo: 2, StepCode: "PT_ARCHIVER", StepName: "Run pt-archiver",
				Status: "failed", Progress: 70, ErrorMessage: err.Error(), RecoveryPolicy: "manual_on_unknown",
			})
			return nil, err
		}
		result, ok := resp.Result.(map[string]any)
		if !ok {
			err := errors.New("invalid archive execution response")
			_ = s.archives.MarkFailed(ctx, job.ID, err.Error())
			return nil, err
		}
		payload, _ := json.Marshal(result)
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 2, StepCode: "PT_ARCHIVER", StepName: "Run pt-archiver",
			Status: "success", Progress: 80, OutputJSON: security.RedactJSON(string(payload)), RecoveryPolicy: "manual_on_unknown",
		})

		scanned := number(result["scanned_rows"])
		archived := number(result["archived_rows"])
		deleted := number(result["deleted_rows"])
		failed := number(result["failed_rows"])
		verification, _ := result["verification"].(string)
		if verification == "" {
			verification = "command_completed"
		}
		if err := s.archives.MarkSuccess(ctx, job.ID, scanned, archived, deleted, failed, verification); err != nil {
			return nil, err
		}
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 3, StepCode: "ARCHIVE_VERIFY", StepName: "Verify archive result",
			Status: "success", Progress: 100,
			OutputJSON: fmt.Sprintf(`{"archive_job_id":%d,"scanned_rows":%d,"archived_rows":%d,"deleted_rows":%d}`, job.ID, scanned, archived, deleted),
			RecoveryPolicy: "verify_before_retry",
		})
		return map[string]any{
			"archive_job_id": job.ID, "status": "success", "scanned_rows": scanned,
			"archived_rows": archived, "deleted_rows": deleted, "failed_rows": failed,
			"verification": verification,
		}, nil
	}
}

func (s *Service) validate(ctx context.Context, req Request, requireConfirmation bool) (runtime, Request, error) {
	if req.InstanceID <= 0 {
		return runtime{}, req, errors.New("instance_id is required")
	}
	if requireConfirmation && !req.Confirmed {
		return runtime{}, req, errors.New("mysql.archive.start is R3 and requires confirmed=true")
	}
	if !validIdentifier(req.SourceDatabase) || !validIdentifier(req.SourceTable) {
		return runtime{}, req, errors.New("invalid source database or table")
	}
	if req.DestinationDatabase != "" || req.DestinationTable != "" {
		if !validIdentifier(req.DestinationDatabase) || !validIdentifier(req.DestinationTable) {
			return runtime{}, req, errors.New("invalid destination database or table")
		}
	}
	if !safeWhere(req.Where) {
		return runtime{}, req, errors.New("where expression is empty or contains forbidden SQL tokens")
	}
	if req.PTArchiverPath == "" || !filepath.IsAbs(req.PTArchiverPath) || filepath.Clean(req.PTArchiverPath) == "/" {
		return runtime{}, req, errors.New("pt_archiver_path must be an absolute path")
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 5000
	}
	if req.TxnSize <= 0 {
		req.TxnSize = req.BatchSize
	}
	if req.SleepMS < 0 {
		return runtime{}, req, errors.New("sleep_ms cannot be negative")
	}
	rt, err := s.loadRuntime(ctx, req.InstanceID)
	if err != nil {
		return runtime{}, req, err
	}
	if rt.Agent.Status != "online" {
		return runtime{}, req, errors.New("agent is offline")
	}
	return rt, req, nil
}

func (s *Service) actionParams(req Request, rt runtime) map[string]any {
	return map[string]any{
		"base_dir": rt.BaseDir, "run_dir": rt.RunDir, "root_password": rt.Password,
		"source_database": req.SourceDatabase, "source_table": req.SourceTable,
		"destination_database": req.DestinationDatabase, "destination_table": req.DestinationTable,
		"where": req.Where, "pt_archiver_path": req.PTArchiverPath,
		"batch_size": req.BatchSize, "txn_size": req.TxnSize, "sleep_ms": req.SleepMS,
		"delete_source": req.DeleteSource,
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

func validIdentifier(v string) bool {
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

func safeWhere(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 2048 {
		return false
	}
	lower := strings.ToLower(v)
	for _, x := range []string{";", "--", "/*", "*/", "\x00", "\n", "\r"} {
		if strings.Contains(lower, x) {
			return false
		}
	}
	return true
}

func number(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	}
	return 0
}
