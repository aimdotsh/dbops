package doris

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
	AgentID     int64  `json:"agent_id"`
	Name        string `json:"name"`
	MySQLClient string `json:"mysql_client"`
	Host        string `json:"host"`
	QueryPort   int    `json:"query_port"`
	Database    string `json:"database"`
	Username    string `json:"username"`
	Password    string `json:"password"`
}

type BackupRequest struct {
	Repository string   `json:"repository"`
	Label      string   `json:"label,omitempty"`
	Tables     []string `json:"tables,omitempty"`
}

type metadata struct {
	MySQLClient string `json:"mysql_client"`
	Host        string `json:"host"`
	Database    string `json:"database"`
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
	return &Service{agents: agents, dbs: dbs, credentials: credentials, backups: backups, tasks: tasks, cipher: cipher, dispatcher: dispatcher}
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
	if req.QueryPort == 0 {
		req.QueryPort = 9030
	}
	if req.QueryPort < 1 || req.QueryPort > 65535 {
		return domain.DatabaseInstance{}, errors.New("invalid Doris query_port")
	}
	if req.MySQLClient == "" || !filepath.IsAbs(req.MySQLClient) || filepath.Clean(req.MySQLClient) == "/" {
		return domain.DatabaseInstance{}, errors.New("mysql_client must be an absolute executable path")
	}
	if !validName(req.Host) || !validName(req.Database) || !validName(req.Username) {
		return domain.DatabaseInstance{}, errors.New("invalid Doris connection parameters")
	}
	if req.Password == "" || strings.ContainsAny(req.Password, "\r\n\x00") {
		return domain.DatabaseInstance{}, errors.New("invalid Doris password")
	}

	params := map[string]any{
		"mysql_client": req.MySQLClient, "host": req.Host, "query_port": req.QueryPort,
		"database": req.Database, "username": req.Username, "password": req.Password,
	}
	policy, _ := actionpolicy.Get("doris.cluster.status")
	resp, err := s.dispatcher.Dispatch(ctx, agent.ID, agentproto.ActionRequest{
		TaskID: 0, Action: "doris.cluster.status", Risk: string(policy.Risk),
		ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 60, Params: params,
	})
	if err != nil {
		return domain.DatabaseInstance{}, fmt.Errorf("Doris validation failed: %w", err)
	}
	status, ok := resp.Result.(map[string]any)
	if !ok {
		return domain.DatabaseInstance{}, errors.New("invalid Doris cluster status response")
	}
	version, _ := status["version"].(string)
	if number(status["fe_alive"]) <= 0 || number(status["be_alive"]) <= 0 {
		return domain.DatabaseInstance{}, errors.New("Doris cluster has no alive FE or BE")
	}

	encrypted, err := s.cipher.EncryptString(req.Password)
	if err != nil {
		return domain.DatabaseInstance{}, err
	}
	cred, err := s.credentials.Create(ctx, domain.Credential{
		Name: req.Name + " Doris admin", CredentialType: "doris_admin",
		Username: req.Username, EncryptedSecret: encrypted,
		MetadataJSON: fmt.Sprintf("{\"host_id\":%d,\"database\":%q}", *agent.HostID, req.Database),
	})
	if err != nil {
		return domain.DatabaseInstance{}, err
	}
	raw, _ := json.Marshal(metadata{MySQLClient: req.MySQLClient, Host: req.Host, Database: req.Database})
	instance, err := s.dbs.CreateImported(ctx, domain.DatabaseInstance{
		Name: req.Name, DBType: "doris", Version: version, HostID: *agent.HostID,
		Port: req.QueryPort, Role: "cluster", CredentialID: &cred.ID, Status: "online",
		ManagedMode: "imported", MetadataJSON: string(raw),
	})
	if err != nil {
		_ = s.credentials.Delete(ctx, cred.ID)
		return domain.DatabaseInstance{}, err
	}
	return instance, nil
}

func (s *Service) ClusterStatus(ctx context.Context, instanceID int64) (map[string]any, error) {
	rt, err := s.loadRuntime(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return s.dispatchRead(ctx, rt)
}

func (s *Service) CreateBackupTask(ctx context.Context, instanceID int64, req BackupRequest) (domain.Task, error) {
	if !validName(req.Repository) {
		return domain.Task{}, errors.New("invalid Doris repository")
	}
	if req.Label == "" {
		req.Label = fmt.Sprintf("dbops_%d_%s", instanceID, time.Now().UTC().Format("20060102T150405"))
	}
	if !validName(req.Label) {
		return domain.Task{}, errors.New("invalid Doris backup label")
	}
	for _, table := range req.Tables {
		if !validName(table) {
			return domain.Task{}, fmt.Errorf("invalid Doris table %q", table)
		}
	}
	rt, err := s.loadRuntime(ctx, instanceID)
	if err != nil {
		return domain.Task{}, err
	}
	raw, _ := json.Marshal(req)
	target := instanceID
	agentID := rt.Agent.ID
	key := fmt.Sprintf("doris.backup:%d:%s", instanceID, req.Label)
	return s.tasks.Create(ctx, domain.Task{
		TaskType: "doris.backup", TargetType: "database", TargetID: &target,
		AgentID: &agentID, ParametersJSON: string(raw), IdempotencyKey: &key,
	})
}

func (s *Service) ListBackups(ctx context.Context, instanceID int64) ([]domain.BackupJob, error) {
	return s.backups.List(ctx, instanceID)
}

func (s *Service) BackupHandler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		if t.TargetID == nil {
			return nil, errors.New("Doris backup target is missing")
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
			BackupEngine: "doris_snapshot", BackupType: "snapshot", Status: "pending",
		})
		if err != nil {
			return nil, err
		}
		_ = s.backups.MarkRunning(ctx, job.ID)
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 1, StepCode: "DORIS_BACKUP_SUBMIT", StepName: "Submit Doris snapshot backup",
			Status: "running", Progress: 10, RecoveryPolicy: "verify_before_retry",
		})

		policy, _ := actionpolicy.Get("doris.backup")
		resp, err := s.dispatcher.Dispatch(ctx, rt.Agent.ID, agentproto.ActionRequest{
			TaskID: 0, Action: "doris.backup", Risk: string(policy.Risk),
			ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 86400,
			Params: s.params(rt, map[string]any{
				"repository": req.Repository, "label": req.Label, "tables": req.Tables,
				"poll_seconds": 2, "job_timeout_seconds": 43200,
			}),
		})
		if err != nil {
			_ = s.backups.MarkFailed(ctx, job.ID, err.Error())
			return nil, err
		}
		result, ok := resp.Result.(map[string]any)
		if !ok {
			err := errors.New("invalid Doris backup response")
			_ = s.backups.MarkFailed(ctx, job.ID, err.Error())
			return nil, err
		}
		state, _ := result["state"].(string)
		if state != "FINISHED" {
			err := fmt.Errorf("Doris backup did not finish: %s", state)
			_ = s.backups.MarkFailed(ctx, job.ID, err.Error())
			return nil, err
		}
		payload, _ := json.Marshal(result)
		storagePath := fmt.Sprintf("doris://%s/%s/%s", req.Repository, rt.Meta.Database, req.Label)
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 1, StepCode: "DORIS_BACKUP_SUBMIT", StepName: "Submit Doris snapshot backup",
			Status: "success", Progress: 40, OutputJSON: "{}", RecoveryPolicy: "verify_before_retry",
		})
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 2, StepCode: "DORIS_BACKUP_POLL", StepName: "Wait for Doris backup job",
			Status: "success", Progress: 90, OutputJSON: security.RedactJSON(string(payload)), RecoveryPolicy: "verify_before_retry",
		})
		if err := s.backups.MarkSuccess(ctx, job.ID, 0, storagePath, "", string(payload)); err != nil {
			return nil, err
		}
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 3, StepCode: "DORIS_BACKUP_VERIFY", StepName: "Verify Doris backup state",
			Status: "success", Progress: 100, OutputJSON: fmt.Sprintf("{\"backup_job_id\":%d,\"state\":\"FINISHED\"}", job.ID),
			RecoveryPolicy: "verify_before_retry",
		})
		return map[string]any{
			"backup_job_id": job.ID, "engine": "doris_snapshot", "backup_type": "snapshot",
			"repository": req.Repository, "label": req.Label, "state": "FINISHED",
			"storage_path": storagePath, "status": "success",
		}, nil
	}
}

func (s *Service) dispatchRead(ctx context.Context, rt runtime) (map[string]any, error) {
	policy, _ := actionpolicy.Get("doris.cluster.status")
	resp, err := s.dispatcher.Dispatch(ctx, rt.Agent.ID, agentproto.ActionRequest{
		TaskID: 0, Action: "doris.cluster.status", Risk: string(policy.Risk),
		ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 60,
		Params: s.params(rt, nil),
	})
	if err != nil {
		return nil, err
	}
	out, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, errors.New("invalid Doris cluster status response")
	}
	return out, nil
}

func (s *Service) loadRuntime(ctx context.Context, instanceID int64) (runtime, error) {
	inst, err := s.dbs.Get(ctx, instanceID)
	if err != nil {
		return runtime{}, err
	}
	if inst.DBType != "doris" || inst.CredentialID == nil {
		return runtime{}, errors.New("instance is not a managed Doris cluster")
	}
	agent, err := s.agents.GetByHostID(ctx, inst.HostID)
	if err != nil {
		return runtime{}, err
	}
	if agent.Status != "online" {
		return runtime{}, errors.New("Doris Agent is offline")
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
	if meta.MySQLClient == "" || meta.Host == "" || meta.Database == "" {
		return runtime{}, errors.New("Doris runtime metadata is incomplete")
	}
	return runtime{Instance: inst, Agent: agent, Meta: meta, Username: cred.Username, Password: password}, nil
}

func (s *Service) params(rt runtime, extra map[string]any) map[string]any {
	out := map[string]any{
		"mysql_client": rt.Meta.MySQLClient, "host": rt.Meta.Host, "query_port": rt.Instance.Port,
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
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == ':' {
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
