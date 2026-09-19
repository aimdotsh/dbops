package mysqlbackup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/domain"
	"strings"
)

type RestoreRequest struct {
	BackupID         int64 `json:"backup_id"`
	TargetInstanceID int64 `json:"target_instance_id"`
	Confirmed        bool  `json:"confirmed"`
}

func (s *Service) validateRestore(ctx context.Context, req RestoreRequest) (domain.BackupJob, runtime, error) {
	if !req.Confirmed {
		return domain.BackupJob{}, runtime{}, errors.New("restore is R4 and requires confirmed=true")
	}
	backup, err := s.backups.Get(ctx, req.BackupID)
	if err != nil {
		return backup, runtime{}, err
	}
	if backup.Status != "success" || backup.BackupEngine != "mysqldump" || backup.DatabaseInstanceID == req.TargetInstanceID {
		return backup, runtime{}, errors.New("select a successful logical backup and a different empty target")
	}
	backupTask, err := s.tasks.Get(ctx, backup.TaskID)
	if err != nil {
		return backup, runtime{}, err
	}
	var scope taskParams
	if err = json.Unmarshal([]byte(backupTask.ParametersJSON), &scope); err != nil {
		return backup, runtime{}, err
	}
	if scope.AllDatabases || len(scope.Databases) == 0 {
		return backup, runtime{}, errors.New("automated restore requires an explicit user-database backup; system schemas need manual recovery")
	}
	for _, name := range scope.Databases {
		switch strings.ToLower(name) {
		case "mysql", "sys", "information_schema", "performance_schema":
			return backup, runtime{}, errors.New("automated restore cannot overwrite system schemas")
		}
	}
	source, err := s.loadRuntime(ctx, backup.DatabaseInstanceID)
	if err != nil {
		return backup, runtime{}, err
	}
	target, err := s.loadRuntime(ctx, req.TargetInstanceID)
	if err != nil {
		return backup, target, err
	}
	if target.Agent.Status != "online" || source.Agent.ID != target.Agent.ID {
		return backup, target, errors.New("restore currently requires source and target on the same online agent")
	}
	if source.Instance.Version != target.Instance.Version {
		return backup, target, errors.New("source and target MySQL versions must match")
	}
	return backup, target, nil
}
func (s *Service) CreateRestoreTask(ctx context.Context, req RestoreRequest) (domain.Task, error) {
	_, rt, err := s.validateRestore(ctx, req)
	if err != nil {
		return domain.Task{}, err
	}
	raw, _ := json.Marshal(req)
	key := fmt.Sprintf("mysql.restore:%d", req.TargetInstanceID)
	return s.tasks.Create(ctx, domain.Task{TaskType: "mysql.restore", TargetType: "database", TargetID: &req.TargetInstanceID, AgentID: &rt.Agent.ID, ParametersJSON: string(raw), IdempotencyKey: &key})
}
func (s *Service) RestoreHandler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		var req RestoreRequest
		if err := json.Unmarshal([]byte(t.ParametersJSON), &req); err != nil {
			return nil, err
		}
		backup, rt, err := s.validateRestore(ctx, req)
		if err != nil {
			return nil, err
		}
		if err = s.tasks.UpsertStep(ctx, domain.TaskStep{TaskID: t.ID, StepNo: 1, StepCode: "RESTORE_PRECHECK", StepName: "Verify backup, target version and host", Status: "success", Progress: 10, RecoveryPolicy: "manual"}); err != nil {
			return nil, err
		}
		resp, err := s.dispatcher.Dispatch(ctx, rt.Agent.ID, agentproto.ActionRequest{Action: "mysql.restore", Risk: "R4", Confirmed: true, ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 86400, Params: map[string]any{"base_dir": rt.BaseDir, "run_dir": rt.RunDir, "root_password": rt.Password, "backup_path": backup.StoragePath, "sha256": backup.Checksum}})
		status, message := "success", ""
		if err != nil {
			status, message = "failed", err.Error()
		}
		if stepErr := s.tasks.UpsertStep(ctx, domain.TaskStep{TaskID: t.ID, StepNo: 2, StepCode: "MYSQL_RESTORE", StepName: "Checksum, empty-target check, restore and verify", Status: status, Progress: 100, ErrorMessage: message, RecoveryPolicy: "manual"}); stepErr != nil && err == nil {
			err = stepErr
		}
		return resp.Result, err
	}
}
