package mysqlbackup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/mysqlinstall"
	"github.com/aimdotsh/dbops/internal/security"
)

type NewHostRequest struct {
	mysqlinstall.InstallRequest
	BackupID int64  `json:"backup_id"`
	ToolPath string `json:"tool_path,omitempty"`
}

func (s *Service) SetInstaller(installer *mysqlinstall.Service) { s.installer = installer }

func (s *Service) validateNewHost(ctx context.Context, req NewHostRequest) (domain.BackupJob, runtime, NewHostRequest, error) {
	var rt runtime
	if s.installer == nil {
		return domain.BackupJob{}, rt, req, errors.New("installer unavailable")
	}
	backup, err := s.backups.Get(ctx, req.BackupID)
	if err != nil {
		return backup, rt, req, err
	}
	if backup.Status != "success" || (backup.BackupEngine != "mysqldump" && backup.BackupEngine != "xtrabackup") {
		return backup, rt, req, errors.New("successful MySQL backup required")
	}
	if backup.BackupEngine == "mysqldump" {
		task, err := s.tasks.Get(ctx, backup.TaskID)
		if err != nil {
			return backup, rt, req, err
		}
		var scope taskParams
		if err := json.Unmarshal([]byte(task.ParametersJSON), &scope); err != nil {
			return backup, rt, req, err
		}
		if scope.AllDatabases || len(scope.Databases) == 0 {
			return backup, rt, req, errors.New("logical restore requires explicit user databases")
		}
		for _, name := range scope.Databases {
			switch strings.ToLower(name) {
			case "mysql", "sys", "information_schema", "performance_schema":
				return backup, rt, req, errors.New("logical system schema restore is unsupported")
			}
		}
	} else {
		var meta struct {
			Scope string `json:"checksum_scope"`
		}
		if json.Unmarshal([]byte(backup.MetadataJSON), &meta) != nil || meta.Scope != "sorted-file-manifest-v1" {
			return backup, rt, req, errors.New("full-file manifest physical backup required")
		}
	}
	rt, err = s.loadRuntime(ctx, backup.DatabaseInstanceID)
	if err != nil {
		return backup, rt, req, err
	}
	agent, pkg, normalized, err := s.installer.ValidateNew(ctx, req.InstallRequest)
	if err != nil {
		return backup, rt, req, err
	}
	if agent.HostID == nil || *agent.HostID == rt.Instance.HostID {
		return backup, rt, req, errors.New("automatic new-host restore requires a different host; use confirmed restore on the original host")
	}
	if rt.Agent.Status != "online" {
		return backup, rt, req, errors.New("source backup agent is offline")
	}
	if pkg.Version != rt.Instance.Version {
		return backup, rt, req, errors.New("backup and target package versions must match")
	}
	req.InstallRequest = normalized
	return backup, rt, req, nil
}

func (s *Service) CreateNewHostRestore(ctx context.Context, req NewHostRequest) (domain.Task, error) {
	_, _, req, err := s.validateNewHost(ctx, req)
	if err != nil {
		return domain.Task{}, err
	}
	raw, _ := json.Marshal(req)
	key := fmt.Sprintf("mysql.restore_new:%d:%d", req.AgentID, req.Port)
	return s.tasks.Create(ctx, domain.Task{TaskType: "mysql.restore_new", TargetType: "agent", TargetID: &req.AgentID, AgentID: &req.AgentID, ParametersJSON: string(raw), IdempotencyKey: &key})
}

func (s *Service) NewHostHandler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		var req NewHostRequest
		if err := json.Unmarshal([]byte(t.ParametersJSON), &req); err != nil {
			return nil, err
		}
		backup, source, req, err := s.validateNewHost(ctx, req)
		if err != nil {
			return nil, err
		}
		id, err := security.RandomPassword(32)
		if err != nil {
			return nil, err
		}
		// RandomPassword may contain punctuation; use hex bytes for a path-safe id.
		transferID := fmt.Sprintf("%x", []byte(id))
		call := func(ctx context.Context, agent int64, op string, p map[string]any) (map[string]any, error) {
			p["transfer_id"] = transferID
			resp, err := s.dispatcher.Dispatch(ctx, agent, agentproto.ActionRequest{Action: "backup.transfer." + op, Risk: "R1", ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 86400, Params: p})
			if err != nil {
				return nil, err
			}
			r, ok := resp.Result.(map[string]any)
			if !ok && op != "cleanup" {
				return nil, errors.New("invalid transfer response")
			}
			return r, nil
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			for _, agent := range []int64{source.Agent.ID, req.AgentID} {
				_, _ = call(cleanup, agent, "cleanup", map[string]any{})
			}
		}()
		manifest, err := call(ctx, source.Agent.ID, "export", map[string]any{"backup_path": backup.StoragePath, "engine": backup.BackupEngine, "sha256": backup.Checksum})
		if err != nil {
			return nil, err
		}
		size := int64Value(manifest["size_bytes"])
		if size <= 0 {
			return nil, errors.New("empty backup transfer")
		}
		for offset := int64(0); offset < size; {
			chunk, err := call(ctx, source.Agent.ID, "read", map[string]any{"offset": offset})
			if err != nil {
				return nil, err
			}
			n := int64Value(chunk["bytes"])
			if n <= 0 || n > 256<<10 || offset+n > size {
				return nil, errors.New("invalid backup chunk")
			}
			ack, err := call(ctx, req.AgentID, "write", map[string]any{"offset": offset, "chunk": chunk["chunk"]})
			if err != nil {
				return nil, err
			}
			if int64Value(ack["bytes"]) != n {
				return nil, errors.New("short backup write")
			}
			offset += n
		}
		transferred, err := call(ctx, req.AgentID, "finish", map[string]any{"sha256": manifest["sha256"]})
		if err != nil {
			return nil, err
		}
		path, ok := transferred["path"].(string)
		if !ok || !filepath.IsAbs(path) {
			return nil, errors.New("invalid transferred backup path")
		}
		if backup.BackupEngine == "mysqldump" {
			path = filepath.Join(path, "backup.sql.gz")
		}
		if err := s.tasks.UpsertStep(ctx, domain.TaskStep{TaskID: t.ID, StepNo: 0, StepCode: "TRANSFER_BACKUP", StepName: "Verify and transfer backup to new host", Status: "success", Progress: 10, RecoveryPolicy: "manual"}); err != nil {
			return nil, err
		}
		installRaw, _ := json.Marshal(req.InstallRequest)
		installTask := t
		installTask.ParametersJSON = string(installRaw)
		result, err := s.installer.RestoreIntoNew(ctx, installTask, map[string]any{"engine": backup.BackupEngine, "backup_path": path, "sha256": backup.Checksum, "checksum_scope": "sorted-file-manifest-v1", "xtrabackup_bin": req.ToolPath})
		if err != nil {
			return nil, fmt.Errorf("new-host restore failed; inspect the new target before retry: %w", err)
		}
		return map[string]any{"restored": true, "mode": "new_host", "backup_id": backup.ID, "engine": backup.BackupEngine, "instance": result}, nil
	}
}
