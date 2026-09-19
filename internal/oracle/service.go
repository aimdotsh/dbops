package oracle

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
	tasks       repository.TaskRepository
	cipher      *security.Cipher
	dispatcher  AgentDispatcher
}

type OnboardRequest struct {
	AgentID     int64  `json:"agent_id"`
	Name        string `json:"name"`
	Port        int    `json:"port"`
	OracleHome  string `json:"oracle_home"`
	OracleSID   string `json:"oracle_sid"`
	ServiceName string `json:"service_name,omitempty"`
	Username    string `json:"username"`
	Password    string `json:"password"`
}

type AddDatafileRequest struct {
	Tablespace string `json:"tablespace"`
	FilePath   string `json:"file_path"`
	SizeMB     int    `json:"size_mb"`
	Autoextend bool   `json:"autoextend"`
	NextMB     int    `json:"next_mb,omitempty"`
	MaxMB      int    `json:"max_mb,omitempty"`
	Confirmed  bool   `json:"confirmed"`
}

type ResizeDatafileRequest struct {
	FilePath     string `json:"file_path"`
	TargetSizeMB int    `json:"target_size_mb"`
	Confirmed    bool   `json:"confirmed"`
}

type instanceMetadata struct {
	OracleHome  string `json:"oracle_home"`
	OracleSID   string `json:"oracle_sid"`
	ServiceName string `json:"service_name,omitempty"`
}

type runtime struct {
	Instance domain.DatabaseInstance
	Agent    domain.Agent
	Metadata instanceMetadata
	Username string
	Password string
}

func New(
	agents repository.AgentRepository,
	dbs repository.DatabaseRepository,
	credentials repository.CredentialRepository,
	tasks repository.TaskRepository,
	cipher *security.Cipher,
	dispatcher AgentDispatcher,
) *Service {
	return &Service{
		agents: agents, dbs: dbs, credentials: credentials,
		tasks: tasks, cipher: cipher, dispatcher: dispatcher,
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
		req.Name = req.OracleSID
	}
	if req.Port == 0 {
		req.Port = 1521
	}
	if req.Port < 1 || req.Port > 65535 {
		return domain.DatabaseInstance{}, errors.New("invalid Oracle port")
	}
	if !validIdentifier(req.OracleSID) || !validIdentifier(req.Username) {
		return domain.DatabaseInstance{}, errors.New("invalid oracle_sid or username")
	}
	if req.OracleHome == "" || !filepath.IsAbs(req.OracleHome) || filepath.Clean(req.OracleHome) == "/" {
		return domain.DatabaseInstance{}, errors.New("oracle_home must be an absolute non-root path")
	}
	if req.Password == "" || strings.ContainsAny(req.Password, "\"\r\n\x00") {
		return domain.DatabaseInstance{}, errors.New("Oracle password is empty or contains unsupported characters")
	}
	meta := instanceMetadata{OracleHome: req.OracleHome, OracleSID: req.OracleSID, ServiceName: req.ServiceName}
	params := map[string]any{
		"oracle_home": req.OracleHome, "oracle_sid": req.OracleSID,
		"username": req.Username, "password": req.Password,
	}
	policy, _ := actionpolicy.Get("oracle.status")
	resp, err := s.dispatcher.Dispatch(ctx, agent.ID, agentproto.ActionRequest{
		TaskID: 0, Action: "oracle.status", Risk: string(policy.Risk),
		ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 60, Params: params,
	})
	if err != nil {
		return domain.DatabaseInstance{}, fmt.Errorf("Oracle connection validation failed: %w", err)
	}
	status, ok := resp.Result.(map[string]any)
	if !ok {
		return domain.DatabaseInstance{}, errors.New("invalid Oracle status response")
	}
	version, _ := status["version"].(string)
	role, _ := status["database_role"].(string)

	encrypted, err := s.cipher.EncryptString(req.Password)
	if err != nil {
		return domain.DatabaseInstance{}, err
	}
	cred, err := s.credentials.Create(ctx, domain.Credential{
		Name: fmt.Sprintf("%s Oracle admin", req.Name), CredentialType: "oracle_admin",
		Username: req.Username, EncryptedSecret: encrypted,
		MetadataJSON: fmt.Sprintf("{\"host_id\":%d,\"sid\":%q}", *agent.HostID, req.OracleSID),
	})
	if err != nil {
		return domain.DatabaseInstance{}, err
	}
	metaRaw, _ := json.Marshal(meta)
	instance, err := s.dbs.CreateImported(ctx, domain.DatabaseInstance{
		Name: req.Name, DBType: "oracle", Version: version, HostID: *agent.HostID,
		Port: req.Port, Role: role, CredentialID: &cred.ID, Status: "online",
		ManagedMode: "imported", MetadataJSON: string(metaRaw),
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
	return s.dispatchRead(ctx, rt, "oracle.status", nil)
}

func (s *Service) Tablespaces(ctx context.Context, instanceID int64) ([]map[string]any, error) {
	rt, err := s.loadRuntime(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	out, err := s.dispatchRead(ctx, rt, "oracle.tablespace.list", nil)
	if err != nil {
		return nil, err
	}
	items, _ := out["items"].([]map[string]any)
	if items != nil {
		return items, nil
	}
	if raw, ok := out["raw"].([]map[string]any); ok {
		return raw, nil
	}
	return nil, errors.New("invalid tablespace response")
}

func (s *Service) Datafiles(ctx context.Context, instanceID int64, tablespace string) ([]map[string]any, error) {
	if !validIdentifier(tablespace) {
		return nil, errors.New("invalid tablespace")
	}
	rt, err := s.loadRuntime(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	resp, err := s.dispatcher.Dispatch(ctx, rt.Agent.ID, agentproto.ActionRequest{
		TaskID: 0, Action: "oracle.datafile.list", Risk: "R0",
		ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 60,
		Params: s.params(rt, map[string]any{"tablespace": tablespace}),
	})
	if err != nil {
		return nil, err
	}
	switch v := resp.Result.(type) {
	case []map[string]any:
		return v, nil
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out, nil
	default:
		return nil, errors.New("invalid datafile response")
	}
}

func (s *Service) CreateAddTask(ctx context.Context, instanceID int64, req AddDatafileRequest) (domain.Task, error) {
	if _, err := actionpolicy.Validate("oracle.datafile.add", req.Confirmed); err != nil {
		return domain.Task{}, err
	}
	if !validIdentifier(req.Tablespace) {
		return domain.Task{}, errors.New("invalid tablespace")
	}
	if err := validatePath(req.FilePath); err != nil {
		return domain.Task{}, err
	}
	if req.SizeMB < 16 {
		return domain.Task{}, errors.New("size_mb must be at least 16")
	}
	rt, err := s.loadRuntime(ctx, instanceID)
	if err != nil {
		return domain.Task{}, err
	}
	raw, _ := json.Marshal(req)
	key := fmt.Sprintf("oracle.datafile.add:%d:%s", instanceID, strings.ToLower(req.FilePath))
	target := instanceID
	agentID := rt.Agent.ID
	return s.tasks.Create(ctx, domain.Task{
		TaskType: "oracle.datafile.add", TargetType: "database", TargetID: &target,
		AgentID: &agentID, ParametersJSON: string(raw), IdempotencyKey: &key,
	})
}

func (s *Service) CreateResizeTask(ctx context.Context, instanceID int64, req ResizeDatafileRequest) (domain.Task, error) {
	if _, err := actionpolicy.Validate("oracle.datafile.resize", req.Confirmed); err != nil {
		return domain.Task{}, err
	}
	if err := validatePath(req.FilePath); err != nil {
		return domain.Task{}, err
	}
	if req.TargetSizeMB < 16 {
		return domain.Task{}, errors.New("target_size_mb must be at least 16")
	}
	rt, err := s.loadRuntime(ctx, instanceID)
	if err != nil {
		return domain.Task{}, err
	}
	raw, _ := json.Marshal(req)
	key := fmt.Sprintf("oracle.datafile.resize:%d:%s", instanceID, strings.ToLower(req.FilePath))
	target := instanceID
	agentID := rt.Agent.ID
	return s.tasks.Create(ctx, domain.Task{
		TaskType: "oracle.datafile.resize", TargetType: "database", TargetID: &target,
		AgentID: &agentID, ParametersJSON: string(raw), IdempotencyKey: &key,
	})
}

func (s *Service) AddHandler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		var req AddDatafileRequest
		if err := json.Unmarshal([]byte(t.ParametersJSON), &req); err != nil {
			return nil, err
		}
		if t.TargetID == nil {
			return nil, errors.New("Oracle task target is missing")
		}
		rt, err := s.loadRuntime(ctx, *t.TargetID)
		if err != nil {
			return nil, err
		}
		return s.runMutation(ctx, t, rt, "oracle.datafile.add", map[string]any{
			"tablespace": req.Tablespace, "file_path": req.FilePath,
			"size_mb": req.SizeMB, "autoextend": req.Autoextend,
			"next_mb": req.NextMB, "max_mb": req.MaxMB,
		})
	}
}

func (s *Service) ResizeHandler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		var req ResizeDatafileRequest
		if err := json.Unmarshal([]byte(t.ParametersJSON), &req); err != nil {
			return nil, err
		}
		if t.TargetID == nil {
			return nil, errors.New("Oracle task target is missing")
		}
		rt, err := s.loadRuntime(ctx, *t.TargetID)
		if err != nil {
			return nil, err
		}
		return s.runMutation(ctx, t, rt, "oracle.datafile.resize", map[string]any{
			"file_path": req.FilePath, "target_size_mb": req.TargetSizeMB,
		})
	}
}

func (s *Service) runMutation(ctx context.Context, t domain.Task, rt runtime, action string, extra map[string]any) (any, error) {
	_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
		TaskID: t.ID, StepNo: 1, StepCode: "REQUEST_VALIDATE", StepName: "Validate structured request",
		Status: "success", Progress: 15, OutputJSON: "{}", RecoveryPolicy: "verify_before_retry",
	})
	_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
		TaskID: t.ID, StepNo: 2, StepCode: "ORACLE_CHANGE", StepName: action,
		Status: "running", Progress: 40, RecoveryPolicy: "manual_on_unknown",
	})
	policy, _ := actionpolicy.Get(action)
	resp, err := s.dispatcher.Dispatch(ctx, rt.Agent.ID, agentproto.ActionRequest{
		TaskID: 0, Action: action, Risk: string(policy.Risk), Confirmed: true,
		ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 600,
		Params: s.params(rt, extra),
	})
	if err != nil {
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 2, StepCode: "ORACLE_CHANGE", StepName: action,
			Status: "failed", Progress: 70, ErrorMessage: err.Error(), RecoveryPolicy: "manual_on_unknown",
		})
		return nil, err
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, errors.New("invalid Oracle mutation response")
	}
	payload, _ := json.Marshal(result)
	_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
		TaskID: t.ID, StepNo: 2, StepCode: "ORACLE_CHANGE", StepName: action,
		Status: "success", Progress: 85, OutputJSON: security.RedactJSON(string(payload)), RecoveryPolicy: "manual_on_unknown",
	})
	verified, _ := result["verified"].(bool)
	if !verified {
		return nil, errors.New("Oracle change verification did not pass")
	}
	_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
		TaskID: t.ID, StepNo: 3, StepCode: "FINAL_VERIFY", StepName: "Verify Oracle metadata",
		Status: "success", Progress: 100, OutputJSON: security.RedactJSON(string(payload)), RecoveryPolicy: "verify_before_retry",
	})
	return result, nil
}

func (s *Service) dispatchRead(ctx context.Context, rt runtime, action string, extra map[string]any) (map[string]any, error) {
	policy, _ := actionpolicy.Get(action)
	resp, err := s.dispatcher.Dispatch(ctx, rt.Agent.ID, agentproto.ActionRequest{
		TaskID: 0, Action: action, Risk: string(policy.Risk),
		ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 60,
		Params: s.params(rt, extra),
	})
	if err != nil {
		return nil, err
	}
	if m, ok := resp.Result.(map[string]any); ok {
		return m, nil
	}
	if arr, ok := resp.Result.([]map[string]any); ok {
		return map[string]any{"items": arr}, nil
	}
	if arr, ok := resp.Result.([]any); ok {
		items := make([]map[string]any, 0, len(arr))
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				items = append(items, m)
			}
		}
		return map[string]any{"items": items}, nil
	}
	return nil, errors.New("invalid Oracle response")
}

func (s *Service) loadRuntime(ctx context.Context, instanceID int64) (runtime, error) {
	inst, err := s.dbs.Get(ctx, instanceID)
	if err != nil {
		return runtime{}, err
	}
	if inst.DBType != "oracle" || inst.CredentialID == nil {
		return runtime{}, errors.New("instance is not a managed Oracle database")
	}
	agent, err := s.agents.GetByHostID(ctx, inst.HostID)
	if err != nil {
		return runtime{}, err
	}
	if agent.Status != "online" {
		return runtime{}, errors.New("Oracle Agent is offline")
	}
	cred, err := s.credentials.Get(ctx, *inst.CredentialID)
	if err != nil {
		return runtime{}, err
	}
	password, err := s.cipher.DecryptString(cred.EncryptedSecret)
	if err != nil {
		return runtime{}, err
	}
	var meta instanceMetadata
	if err := json.Unmarshal([]byte(inst.MetadataJSON), &meta); err != nil {
		return runtime{}, err
	}
	if meta.OracleHome == "" || meta.OracleSID == "" {
		return runtime{}, errors.New("Oracle runtime metadata is incomplete")
	}
	return runtime{
		Instance: inst, Agent: agent, Metadata: meta,
		Username: cred.Username, Password: password,
	}, nil
}

func (s *Service) params(rt runtime, extra map[string]any) map[string]any {
	out := map[string]any{
		"oracle_home": rt.Metadata.OracleHome, "oracle_sid": rt.Metadata.OracleSID,
		"username": rt.Username, "password": rt.Password,
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func validIdentifier(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '$' || r == '#' {
			continue
		}
		return false
	}
	return true
}

func validatePath(v string) error {
	if v == "" || !filepath.IsAbs(v) || filepath.Clean(v) == "/" {
		return errors.New("datafile path must be an absolute non-root path")
	}
	if strings.ContainsAny(v, "'\"\r\n\x00") {
		return errors.New("datafile path contains forbidden characters")
	}
	return nil
}
