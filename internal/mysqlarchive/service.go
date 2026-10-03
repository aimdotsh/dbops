package mysqlarchive

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
	agents       repository.AgentRepository
	dbs          repository.DatabaseRepository
	credentials  repository.CredentialRepository
	policies     repository.ArchivePolicyRepository
	jobs         repository.ArchiveJobRepository
	tasks        repository.TaskRepository
	cipher       *security.Cipher
	dispatcher   AgentDispatcher
	replications repository.MySQLReplicationRepository
}

func (s *Service) SetReplications(repo repository.MySQLReplicationRepository) { s.replications = repo }

type PolicyRequest struct {
	Name                  string `json:"name"`
	SourceInstanceID      int64  `json:"source_instance_id"`
	SourceDatabase        string `json:"source_database"`
	SourceTable           string `json:"source_table"`
	ArchiveColumn         string `json:"archive_column,omitempty"`
	WhereTemplate         string `json:"where_template"`
	RetentionDays         int    `json:"retention_days,omitempty"`
	DestinationType       string `json:"destination_type"`
	DestinationInstanceID *int64 `json:"destination_instance_id,omitempty"`
	DestinationDatabase   string `json:"destination_database,omitempty"`
	DestinationTable      string `json:"destination_table,omitempty"`
	BatchSize             int    `json:"batch_size,omitempty"`
	TxnSize               int    `json:"txn_size,omitempty"`
	SleepMS               int    `json:"sleep_ms,omitempty"`
	MaxReplicationLag     int    `json:"max_replication_lag,omitempty"`
	MaxThreadsRunning     int    `json:"max_threads_running,omitempty"`
	DeleteSource          bool   `json:"delete_source"`
	PTArchiverPath        string `json:"pt_archiver_path"`
}

type startParams struct {
	JobID     int64  `json:"job_id"`
	Mode      string `json:"mode"`
	Confirmed bool   `json:"confirmed"`
}

type controlParams struct {
	JobID     int64  `json:"job_id"`
	Action    string `json:"action"`
	Confirmed bool   `json:"confirmed,omitempty"`
}

type policyOptions struct {
	PTArchiverPath string `json:"pt_archiver_path"`
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
	policies repository.ArchivePolicyRepository,
	jobs repository.ArchiveJobRepository,
	tasks repository.TaskRepository,
	cipher *security.Cipher,
	dispatcher AgentDispatcher,
) *Service {
	return &Service{
		agents: agents, dbs: dbs, credentials: credentials,
		policies: policies, jobs: jobs, tasks: tasks,
		cipher: cipher, dispatcher: dispatcher,
	}
}

func (s *Service) CreatePolicy(ctx context.Context, req PolicyRequest) (domain.ArchivePolicy, error) {
	p, err := s.normalizePolicy(ctx, req)
	if err != nil {
		return domain.ArchivePolicy{}, err
	}
	return s.policies.Create(ctx, p)
}

func (s *Service) ListPolicies(ctx context.Context) ([]domain.ArchivePolicy, error) {
	return s.policies.List(ctx)
}

func (s *Service) ListJobs(ctx context.Context, policyID int64) ([]domain.ArchiveJob, error) {
	return s.jobs.List(ctx, policyID)
}

func (s *Service) PrecheckPolicy(ctx context.Context, policyID int64) (map[string]any, error) {
	p, err := s.policies.Get(ctx, policyID)
	if err != nil {
		return nil, err
	}
	source, dest, err := s.loadPolicyRuntimes(ctx, p)
	if err != nil {
		return nil, err
	}
	where, err := materializeWhere(p)
	if err != nil {
		return nil, err
	}
	params, err := s.actionParams(p, source, dest, where, 0)
	if err != nil {
		return nil, err
	}
	policy, _ := actionpolicy.Get("mysql.archive.precheck")
	resp, err := s.dispatcher.Dispatch(ctx, source.Agent.ID, agentproto.ActionRequest{
		TaskID: 0, Action: "mysql.archive.precheck", Risk: string(policy.Risk),
		ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 600, Params: params,
	})
	if err != nil {
		return nil, err
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, errors.New("invalid archive precheck response")
	}
	result["materialized_where"] = where
	return result, nil
}

func (s *Service) Start(ctx context.Context, policyID int64, confirmed bool) (domain.Task, error) {
	if !confirmed {
		return domain.Task{}, errors.New("mysql.archive.start is R3 and requires confirmed=true")
	}
	p, err := s.policies.Get(ctx, policyID)
	if err != nil {
		return domain.Task{}, err
	}
	if !p.Enabled {
		return domain.Task{}, errors.New("archive policy is disabled")
	}
	source, _, err := s.loadPolicyRuntimes(ctx, p)
	if err != nil {
		return domain.Task{}, err
	}
	job, err := s.jobs.Create(ctx, domain.ArchiveJob{PolicyID: policyID, Status: "pending"})
	if err != nil {
		return domain.Task{}, err
	}
	return s.createRunTask(ctx, p, source, job.ID, "start", true)
}

func (s *Service) Resume(ctx context.Context, jobID int64) (domain.Task, error) {
	job, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return domain.Task{}, err
	}
	if job.Status != "paused" && job.Status != "pause_requested" && job.Status != "interrupted" {
		return domain.Task{}, fmt.Errorf("archive job %d cannot resume from status %s", jobID, job.Status)
	}
	p, err := s.policies.Get(ctx, job.PolicyID)
	if err != nil {
		return domain.Task{}, err
	}
	source, _, err := s.loadPolicyRuntimes(ctx, p)
	if err != nil {
		return domain.Task{}, err
	}
	return s.createRunTask(ctx, p, source, jobID, "resume", false)
}

func (s *Service) Control(ctx context.Context, jobID int64, action string, confirmed bool) (domain.Task, error) {
	if action != "mysql.archive.pause" && action != "mysql.archive.stop" {
		return domain.Task{}, fmt.Errorf("unsupported archive control %q", action)
	}
	if _, err := actionpolicy.Validate(action, confirmed); err != nil {
		return domain.Task{}, err
	}
	job, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return domain.Task{}, err
	}
	if job.Status != "running" && job.Status != "pause_requested" {
		return domain.Task{}, fmt.Errorf("archive job %d is not running", jobID)
	}
	p, err := s.policies.Get(ctx, job.PolicyID)
	if err != nil {
		return domain.Task{}, err
	}
	source, _, err := s.loadPolicyRuntimes(ctx, p)
	if err != nil {
		return domain.Task{}, err
	}
	raw, _ := json.Marshal(controlParams{JobID: jobID, Action: action, Confirmed: confirmed})
	key := fmt.Sprintf("%s:%d", action, jobID)
	target := jobID
	agentID := source.Agent.ID
	return s.tasks.Create(ctx, domain.Task{
		TaskType: "mysql.archive.control", TargetType: "archive_job", TargetID: &target,
		AgentID: &agentID, ParametersJSON: string(raw), IdempotencyKey: &key,
	})
}

func (s *Service) createRunTask(
	ctx context.Context,
	p domain.ArchivePolicy,
	source runtime,
	jobID int64,
	mode string,
	confirmed bool,
) (domain.Task, error) {
	raw, _ := json.Marshal(startParams{JobID: jobID, Mode: mode, Confirmed: confirmed})
	key := fmt.Sprintf("mysql.archive.run:%d", jobID)
	target := p.SourceInstanceID
	agentID := source.Agent.ID
	task, err := s.tasks.Create(ctx, domain.Task{
		TaskType: "mysql.archive.run", TargetType: "database", TargetID: &target,
		AgentID: &agentID, ParametersJSON: string(raw), IdempotencyKey: &key,
	})
	if err != nil {
		return domain.Task{}, err
	}
	if err := s.jobs.AttachTask(ctx, jobID, task.ID); err != nil {
		return domain.Task{}, err
	}
	return task, nil
}

func (s *Service) RunHandler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		var tp startParams
		if err := json.Unmarshal([]byte(t.ParametersJSON), &tp); err != nil {
			return nil, err
		}
		job, err := s.jobs.Get(ctx, tp.JobID)
		if err != nil {
			return nil, err
		}
		p, err := s.policies.Get(ctx, job.PolicyID)
		if err != nil {
			return nil, err
		}
		source, dest, err := s.loadPolicyRuntimes(ctx, p)
		if err != nil {
			return nil, err
		}

		if tp.Mode == "resume" {
			resumePolicy, _ := actionpolicy.Get("mysql.archive.resume")
			if _, err := s.dispatcher.Dispatch(ctx, source.Agent.ID, agentproto.ActionRequest{
				TaskID: 0, Action: "mysql.archive.resume", Risk: string(resumePolicy.Risk),
				ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 60,
				Params: map[string]any{"archive_job_id": job.ID},
			}); err != nil {
				return nil, err
			}
		}

		where, err := materializeWhere(p)
		if err != nil {
			return nil, err
		}
		actionParams, err := s.actionParams(p, source, dest, where, job.ID)
		if err != nil {
			return nil, err
		}

		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 1, StepCode: "ARCHIVE_PRECHECK", StepName: "Archive precheck and dry-run",
			Status: "running", Progress: 10, RecoveryPolicy: "verify_before_retry",
		})
		prePolicy, _ := actionpolicy.Get("mysql.archive.precheck")
		preResp, err := s.dispatcher.Dispatch(ctx, source.Agent.ID, agentproto.ActionRequest{
			TaskID: 0, Action: "mysql.archive.precheck", Risk: string(prePolicy.Risk),
			ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 600, Params: actionParams,
		})
		if err != nil {
			_ = s.jobs.UpdateState(ctx, job.ID, "failed", job.ScannedRows, job.ArchivedRows, job.DeletedRows, job.FailedRows, job.SpeedRowsSec, job.LastProcessedKey, job.PauseReason, err.Error())
			return nil, err
		}
		precheck, _ := preResp.Result.(map[string]any)
		if ok, _ := precheck["ok"].(bool); !ok {
			err := fmt.Errorf("archive precheck blocked: %v", precheck)
			_ = s.jobs.UpdateState(ctx, job.ID, "failed", job.ScannedRows, job.ArchivedRows, job.DeletedRows, job.FailedRows, job.SpeedRowsSec, job.LastProcessedKey, job.PauseReason, err.Error())
			return nil, err
		}
		prePayload, _ := json.Marshal(precheck)
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 1, StepCode: "ARCHIVE_PRECHECK", StepName: "Archive precheck and dry-run",
			Status: "success", Progress: 20, OutputJSON: security.RedactJSON(string(prePayload)), RecoveryPolicy: "verify_before_retry",
		})
		lagProbe, err := s.archiveLagProbe(ctx, p)
		if err == nil && lagProbe != nil {
			probeCtx, stop := context.WithTimeout(ctx, 15*time.Second)
			err = lagProbe(probeCtx)
			stop()
		}
		if err != nil {
			_ = s.jobs.UpdateState(ctx, job.ID, "failed", job.ScannedRows, job.ArchivedRows, job.DeletedRows, job.FailedRows, job.SpeedRowsSec, job.LastProcessedKey, job.PauseReason, err.Error())
			return nil, fmt.Errorf("archive replication protection: %w", err)
		}

		_ = s.jobs.UpdateState(ctx, job.ID, "running", job.ScannedRows, job.ArchivedRows, job.DeletedRows, job.FailedRows, job.SpeedRowsSec, job.LastProcessedKey, "", "")
		startPolicy, _ := actionpolicy.Get("mysql.archive.start")
		confirmed := tp.Confirmed || tp.Mode == "resume"
		monitorCtx, cancelMonitor := context.WithCancel(ctx)
		lagFailures := make(chan error, 1)
		monitorDone := make(chan struct{})
		stopArchiver := func(stopCtx context.Context) error {
			_, stopErr := s.dispatcher.Dispatch(stopCtx, source.Agent.ID, agentproto.ActionRequest{
				TaskID: 0, Action: "mysql.archive.stop", Risk: "R3", Confirmed: true,
				ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 15,
				Params: map[string]any{"archive_job_id": job.ID},
			})
			return stopErr
		}
		go func() {
			defer close(monitorDone)
			watchArchiveLag(monitorCtx, lagProbe, stopArchiver, lagFailures)
		}()
		resp, err := s.dispatcher.Dispatch(ctx, source.Agent.ID, agentproto.ActionRequest{
			TaskID: 0, Action: "mysql.archive.start", Risk: string(startPolicy.Risk), Confirmed: confirmed,
			ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 86400, Params: actionParams,
		})
		cancelMonitor()
		<-monitorDone
		select {
		case monitorErr := <-lagFailures:
			err = fmt.Errorf("archive stopped by replication protection: %w", monitorErr)
		default:
		}
		if err != nil {
			scanned, archived, deleted, failed := job.ScannedRows, job.ArchivedRows, job.DeletedRows, job.FailedRows
			if partial, ok := resp.Result.(map[string]any); ok {
				scanned += number(partial["scanned_rows"])
				archived += number(partial["archived_rows"])
				deleted += number(partial["deleted_rows"])
				failed += number(partial["failed_rows"])
			}
			_ = s.jobs.UpdateState(ctx, job.ID, "failed", scanned, archived, deleted, failed, job.SpeedRowsSec, job.LastProcessedKey, job.PauseReason, err.Error())
			_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
				TaskID: t.ID, StepNo: 2, StepCode: "PT_ARCHIVER", StepName: "Run pt-archiver",
				Status: "failed", Progress: 70, ErrorMessage: err.Error(), RecoveryPolicy: "manual_on_unknown",
			})
			return nil, err
		}
		result, ok := resp.Result.(map[string]any)
		if !ok {
			return nil, errors.New("invalid archive execution response")
		}
		payload, _ := json.Marshal(result)
		state, _ := result["status"].(string)
		scanned := job.ScannedRows + number(result["scanned_rows"])
		archived := job.ArchivedRows + number(result["archived_rows"])
		deleted := job.DeletedRows + number(result["deleted_rows"])
		failed := job.FailedRows + number(result["failed_rows"])
		pauseReason, _ := result["pause_reason"].(string)

		jobState := "success"
		switch state {
		case "paused":
			jobState = "paused"
		case "stopped":
			jobState = "stopped"
		case "completed", "":
			jobState = "success"
		default:
			jobState = state
		}
		if err := s.jobs.UpdateState(ctx, job.ID, jobState, scanned, archived, deleted, failed, 0, "", pauseReason, ""); err != nil {
			return nil, err
		}
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 2, StepCode: "PT_ARCHIVER", StepName: "Run pt-archiver",
			Status: "success", Progress: 85, OutputJSON: security.RedactJSON(string(payload)), RecoveryPolicy: "manual_on_unknown",
		})
		_ = s.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID: t.ID, StepNo: 3, StepCode: "ARCHIVE_VERIFY", StepName: "Verify archive result",
			Status: "success", Progress: 100,
			OutputJSON:     fmt.Sprintf("{\"archive_job_id\":%d,\"status\":%q,\"scanned_rows\":%d,\"archived_rows\":%d,\"deleted_rows\":%d}", job.ID, jobState, scanned, archived, deleted),
			RecoveryPolicy: "verify_before_retry",
		})
		return map[string]any{
			"archive_job_id": job.ID, "status": jobState,
			"scanned_rows": scanned, "archived_rows": archived,
			"deleted_rows": deleted, "failed_rows": failed,
		}, nil
	}
}

func (s *Service) ControlHandler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		var p controlParams
		if err := json.Unmarshal([]byte(t.ParametersJSON), &p); err != nil {
			return nil, err
		}
		job, err := s.jobs.Get(ctx, p.JobID)
		if err != nil {
			return nil, err
		}
		policy, err := s.policies.Get(ctx, job.PolicyID)
		if err != nil {
			return nil, err
		}
		source, _, err := s.loadPolicyRuntimes(ctx, policy)
		if err != nil {
			return nil, err
		}
		action, err := actionpolicy.Validate(p.Action, p.Confirmed)
		if err != nil {
			return nil, err
		}
		resp, err := s.dispatcher.Dispatch(ctx, source.Agent.ID, agentproto.ActionRequest{
			TaskID: 0, Action: p.Action, Risk: string(action.Risk), Confirmed: p.Confirmed,
			ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 60,
			Params: map[string]any{"archive_job_id": job.ID},
		})
		if err != nil {
			return nil, err
		}
		next := job.Status
		reason := job.PauseReason
		if p.Action == "mysql.archive.pause" {
			next = "pause_requested"
			reason = "operator requested pause"
		}
		if p.Action == "mysql.archive.stop" {
			next = "stop_requested"
			reason = "operator requested stop"
		}
		_ = s.jobs.UpdateState(ctx, job.ID, next, job.ScannedRows, job.ArchivedRows, job.DeletedRows, job.FailedRows, job.SpeedRowsSec, job.LastProcessedKey, reason, "")
		return resp.Result, nil
	}
}

func (s *Service) normalizePolicy(ctx context.Context, req PolicyRequest) (domain.ArchivePolicy, error) {
	if req.Name == "" || req.SourceInstanceID <= 0 {
		return domain.ArchivePolicy{}, errors.New("name and source_instance_id are required")
	}
	if !validIdentifier(req.SourceDatabase) || !validIdentifier(req.SourceTable) {
		return domain.ArchivePolicy{}, errors.New("invalid source database or table")
	}
	if req.ArchiveColumn != "" && !validIdentifier(req.ArchiveColumn) {
		return domain.ArchivePolicy{}, errors.New("invalid archive_column")
	}
	if !safeWhere(req.WhereTemplate) {
		return domain.ArchivePolicy{}, errors.New("where_template is empty or contains forbidden SQL tokens")
	}
	if strings.Contains(req.WhereTemplate, ":cutoff") && req.RetentionDays <= 0 {
		return domain.ArchivePolicy{}, errors.New("retention_days must be positive when where_template uses :cutoff")
	}
	if req.DestinationType == "" {
		req.DestinationType = "same_instance"
	}
	if req.DestinationType != "same_instance" && req.DestinationType != "mysql_instance" {
		return domain.ArchivePolicy{}, errors.New("destination_type must be same_instance or mysql_instance")
	}
	if !validIdentifier(req.DestinationDatabase) || !validIdentifier(req.DestinationTable) {
		return domain.ArchivePolicy{}, errors.New("destination_database and destination_table are required")
	}
	if req.DestinationType == "mysql_instance" {
		if req.DestinationInstanceID == nil || *req.DestinationInstanceID <= 0 {
			return domain.ArchivePolicy{}, errors.New("destination_instance_id is required")
		}
		if *req.DestinationInstanceID == req.SourceInstanceID {
			return domain.ArchivePolicy{}, errors.New("use same_instance for the source instance")
		}
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 5000
	}
	if req.TxnSize <= 0 {
		req.TxnSize = req.BatchSize
	}
	if req.SleepMS < 0 {
		return domain.ArchivePolicy{}, errors.New("sleep_ms cannot be negative")
	}
	if req.MaxReplicationLag < 0 || req.MaxThreadsRunning < 0 {
		return domain.ArchivePolicy{}, errors.New("load protection thresholds cannot be negative")
	}
	if req.PTArchiverPath == "" || !filepath.IsAbs(req.PTArchiverPath) || filepath.Clean(req.PTArchiverPath) == "/" {
		return domain.ArchivePolicy{}, errors.New("pt_archiver_path must be an absolute path")
	}
	source, err := s.loadRuntime(ctx, req.SourceInstanceID)
	if err != nil {
		return domain.ArchivePolicy{}, err
	}
	if source.Agent.Status != "online" {
		return domain.ArchivePolicy{}, errors.New("source Agent is offline")
	}
	if req.DestinationType == "mysql_instance" {
		dest, err := s.loadRuntime(ctx, *req.DestinationInstanceID)
		if err != nil {
			return domain.ArchivePolicy{}, err
		}
		if dest.Agent.Status != "online" {
			return domain.ArchivePolicy{}, errors.New("destination Agent is offline")
		}
		if dest.Agent.ID != source.Agent.ID {
			return domain.ArchivePolicy{}, errors.New("V1 pt-archiver requires source and destination reachable from the same Agent host")
		}
	}
	opts, _ := json.Marshal(policyOptions{PTArchiverPath: req.PTArchiverPath})
	return domain.ArchivePolicy{
		Name: req.Name, SourceInstanceID: req.SourceInstanceID,
		SourceDatabase: req.SourceDatabase, SourceTable: req.SourceTable,
		ArchiveColumn: req.ArchiveColumn, WhereTemplate: req.WhereTemplate, RetentionDays: req.RetentionDays,
		DestinationType: req.DestinationType, DestinationInstanceID: req.DestinationInstanceID,
		DestinationDatabase: req.DestinationDatabase, DestinationTable: req.DestinationTable,
		BatchSize: req.BatchSize, TxnSize: req.TxnSize, SleepMS: req.SleepMS,
		MaxReplicationLag: req.MaxReplicationLag, MaxThreadsRunning: req.MaxThreadsRunning,
		DeleteSource: req.DeleteSource, Enabled: true, OptionsJSON: string(opts),
	}, nil
}

func (s *Service) loadPolicyRuntimes(ctx context.Context, p domain.ArchivePolicy) (runtime, *runtime, error) {
	source, err := s.loadRuntime(ctx, p.SourceInstanceID)
	if err != nil {
		return runtime{}, nil, err
	}
	if source.Agent.Status != "online" {
		return runtime{}, nil, errors.New("source Agent is offline")
	}
	if p.DestinationType != "mysql_instance" {
		return source, nil, nil
	}
	if p.DestinationInstanceID == nil {
		return runtime{}, nil, errors.New("destination instance is missing")
	}
	dest, err := s.loadRuntime(ctx, *p.DestinationInstanceID)
	if err != nil {
		return runtime{}, nil, err
	}
	if dest.Agent.Status != "online" {
		return runtime{}, nil, errors.New("destination Agent is offline")
	}
	if dest.Agent.ID != source.Agent.ID {
		return runtime{}, nil, errors.New("source and destination must be reachable from the same Agent host in V1")
	}
	return source, &dest, nil
}

func (s *Service) actionParams(p domain.ArchivePolicy, source runtime, dest *runtime, where string, jobID int64) (map[string]any, error) {
	var opts policyOptions
	if err := json.Unmarshal([]byte(p.OptionsJSON), &opts); err != nil {
		return nil, err
	}
	if opts.PTArchiverPath == "" {
		return nil, errors.New("pt_archiver_path missing from policy")
	}
	params := map[string]any{
		"base_dir": source.BaseDir, "run_dir": source.RunDir, "root_password": source.Password,
		"source_database": p.SourceDatabase, "source_table": p.SourceTable,
		"destination_database": p.DestinationDatabase, "destination_table": p.DestinationTable,
		"where": where, "pt_archiver_path": opts.PTArchiverPath,
		"batch_size": p.BatchSize, "txn_size": p.TxnSize, "sleep_ms": p.SleepMS,
		"max_replication_lag": p.MaxReplicationLag, "max_threads_running": p.MaxThreadsRunning,
		"delete_source": p.DeleteSource,
	}
	if jobID > 0 {
		params["archive_job_id"] = jobID
	}
	if dest != nil {
		params["destination_run_dir"] = dest.RunDir
		params["destination_password"] = dest.Password
	}
	return params, nil
}

func materializeWhere(p domain.ArchivePolicy) (string, error) {
	where := strings.TrimSpace(p.WhereTemplate)
	if strings.Contains(where, ":cutoff") {
		if p.RetentionDays <= 0 {
			return "", errors.New("retention_days must be positive")
		}
		cutoff := time.Now().UTC().AddDate(0, 0, -p.RetentionDays).Format("2006-01-02 15:04:05")
		where = strings.ReplaceAll(where, ":cutoff", "'"+cutoff+"'")
	}
	if !safeWhere(where) {
		return "", errors.New("materialized where expression is unsafe")
	}
	return where, nil
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
