package mysqlservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aimdotsh/dbops/internal/actionpolicy"
	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
)

type AgentDispatcher interface {
	Dispatch(context.Context, int64, agentproto.ActionRequest) (agentproto.ActionResponse, error)
}

type Service struct {
	agents     repository.AgentRepository
	dbs        repository.DatabaseRepository
	tasks      repository.TaskRepository
	dispatcher AgentDispatcher
}

type Request struct {
	InstanceID int64  `json:"instance_id"`
	Action     string `json:"action"`
	Confirmed  bool   `json:"confirmed"`
}

type taskParams struct {
	InstanceID int64  `json:"instance_id"`
	Action     string `json:"action"`
	Confirmed  bool   `json:"confirmed"`
}

func New(agents repository.AgentRepository, dbs repository.DatabaseRepository, tasks repository.TaskRepository, dispatcher AgentDispatcher) *Service {
	return &Service{agents: agents, dbs: dbs, tasks: tasks, dispatcher: dispatcher}
}

func (s *Service) CreateTask(ctx context.Context, req Request) (domain.Task, error) {
	if req.InstanceID <= 0 {
		return domain.Task{}, errors.New("instance_id is required")
	}
	if req.Action != "mysql.start" && req.Action != "mysql.stop" && req.Action != "mysql.restart" {
		return domain.Task{}, fmt.Errorf("unsupported MySQL service action %q", req.Action)
	}
	if _, err := actionpolicy.Validate(req.Action, req.Confirmed); err != nil {
		return domain.Task{}, err
	}
	inst, err := s.dbs.Get(ctx, req.InstanceID)
	if err != nil {
		return domain.Task{}, err
	}
	if inst.DBType != "mysql" {
		return domain.Task{}, errors.New("instance is not MySQL")
	}
	agent, err := s.agents.GetByHostID(ctx, inst.HostID)
	if err != nil {
		return domain.Task{}, err
	}
	if agent.Status != "online" {
		return domain.Task{}, errors.New("Agent is offline")
	}
	raw, _ := json.Marshal(taskParams{InstanceID: req.InstanceID, Action: req.Action, Confirmed: req.Confirmed})
	key := fmt.Sprintf("%s:%d", req.Action, req.InstanceID)
	target := req.InstanceID
	return s.tasks.Create(ctx, domain.Task{
		TaskType: "mysql.service", TargetType: "database", TargetID: &target,
		AgentID: &agent.ID, ParametersJSON: string(raw), IdempotencyKey: &key,
	})
}

func (s *Service) Handler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		if t.AgentID == nil {
			return nil, errors.New("mysql.service requires agent_id")
		}
		var p taskParams
		if err := json.Unmarshal([]byte(t.ParametersJSON), &p); err != nil {
			return nil, err
		}
		policy, err := actionpolicy.Validate(p.Action, p.Confirmed)
		if err != nil {
			return nil, err
		}
		inst, err := s.dbs.Get(ctx, p.InstanceID)
		if err != nil {
			return nil, err
		}
		var meta struct {
			InstallResult struct {
				BaseDir     string `json:"base_dir"`
				RunDir      string `json:"run_dir"`
				ServiceName string `json:"service_name"`
				ServiceMode string `json:"service_mode"`
			} `json:"install_result"`
		}
		if err := json.Unmarshal([]byte(inst.MetadataJSON), &meta); err != nil {
			return nil, err
		}
		rt := meta.InstallResult
		if rt.BaseDir == "" || rt.RunDir == "" || inst.ConfigPath == "" {
			return nil, errors.New("instance runtime metadata is incomplete")
		}
		resp, err := s.dispatcher.Dispatch(ctx, *t.AgentID, agentproto.ActionRequest{
			TaskID: 0, Action: p.Action, Risk: string(policy.Risk), Confirmed: p.Confirmed,
			ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 180,
			Params: map[string]any{
				"base_dir": rt.BaseDir, "run_dir": rt.RunDir, "config_path": inst.ConfigPath,
				"service_name": rt.ServiceName, "service_mode": rt.ServiceMode, "port": inst.Port,
			},
		})
		if err != nil {
			return nil, err
		}
		status := "online"
		if p.Action == "mysql.stop" {
			status = "offline"
		}
		if err := s.dbs.UpdateStatus(ctx, inst.ID, status); err != nil {
			return nil, err
		}
		return map[string]any{"instance_id": inst.ID, "action": p.Action, "status": status, "agent_result": resp.Result}, nil
	}
}
