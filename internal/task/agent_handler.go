package task

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/aimdotsh/dbops/internal/actionpolicy"
	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/aimdotsh/dbops/internal/security"
)

type AgentDispatcher interface {
	Dispatch(context.Context, int64, agentproto.ActionRequest) (agentproto.ActionResponse, error)
}

type agentActionParams struct {
	Action         string         `json:"action"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
	Confirmed      bool           `json:"confirmed,omitempty"`
	Params         map[string]any `json:"params,omitempty"`
}

func AgentActionHandler(dispatcher AgentDispatcher, repo repository.TaskRepository) Handler {
	return func(ctx context.Context, t domain.Task) (any, error) {
		if t.AgentID == nil || *t.AgentID <= 0 {
			return nil, errors.New("agent_id is required for agent.action")
		}

		var params agentActionParams
		if err := json.Unmarshal([]byte(t.ParametersJSON), &params); err != nil {
			return nil, err
		}
		if params.Action == "" {
			return nil, errors.New("action is required")
		}

		policy, err := actionpolicy.Validate(params.Action, params.Confirmed)
		if err != nil {
			return nil, err
		}

		_ = repo.AddEvent(ctx, domain.TaskEvent{
			TaskID:      t.ID,
			EventType:   "dispatch",
			Level:       "INFO",
			Message:     "dispatching action to agent",
			PayloadJSON: security.RedactJSON(t.ParametersJSON),
		})

		resp, err := dispatcher.Dispatch(ctx, *t.AgentID, agentproto.ActionRequest{
			TaskID:          t.ID,
			Action:          params.Action,
			Risk:            string(policy.Risk),
			Confirmed:       params.Confirmed,
			ProtocolVersion: agentproto.ProtocolVersion,
			TimeoutSeconds:  params.TimeoutSeconds,
			Params:          params.Params,
		})
		if err != nil {
			_ = repo.AddEvent(ctx, domain.TaskEvent{
				TaskID:      t.ID,
				EventType:   "dispatch_error",
				Level:       "ERROR",
				Message:     err.Error(),
				PayloadJSON: "{}",
			})
			return nil, err
		}
		return resp.Result, nil
	}
}
