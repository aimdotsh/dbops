package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

func (r TaskRepo) AddEvent(ctx context.Context, event domain.TaskEvent) error {
	if event.EventTime.IsZero() {
		event.EventTime = time.Now().UTC()
	}
	if event.Level == "" {
		event.Level = "INFO"
	}
	if event.PayloadJSON == "" {
		event.PayloadJSON = "{}"
	}
	_, err := r.DB.ExecContext(ctx, `
INSERT INTO task_events(task_id,event_time,event_type,step_code,level,message,payload_json)
VALUES(?,?,?,?,?,?,?)`,
		event.TaskID,
		event.EventTime.Format(time.RFC3339),
		event.EventType,
		nullString(event.StepCode),
		event.Level,
		event.Message,
		event.PayloadJSON,
	)
	return err
}

func (r TaskRepo) ListEvents(ctx context.Context, taskID int64) ([]domain.TaskEvent, error) {
	rows, err := r.DB.QueryContext(ctx, `
SELECT id,task_id,event_time,event_type,COALESCE(step_code,''),level,COALESCE(message,''),payload_json
FROM task_events
WHERE task_id=?
ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.TaskEvent
	for rows.Next() {
		var event domain.TaskEvent
		var ts string
		if err := rows.Scan(&event.ID, &event.TaskID, &ts, &event.EventType, &event.StepCode, &event.Level, &event.Message, &event.PayloadJSON); err != nil {
			return nil, err
		}
		event.EventTime, _ = time.Parse(time.RFC3339, ts)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r TaskRepo) UpsertStep(ctx context.Context, step domain.TaskStep) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if step.Status == "" {
		step.Status = "running"
	}
	if step.RecoveryPolicy == "" {
		step.RecoveryPolicy = "verify_before_retry"
	}
	_, err := r.DB.ExecContext(ctx, `
INSERT INTO task_steps(task_id,step_no,step_code,step_name,status,progress,started_at,output_json,error_message,recovery_policy)
VALUES(?,?,?,?,?,?,?,NULLIF(?,''),NULLIF(?,''),?)
ON CONFLICT(task_id,step_no) DO UPDATE SET
  step_code=excluded.step_code,
  step_name=excluded.step_name,
  status=excluded.status,
  progress=excluded.progress,
  finished_at=CASE WHEN excluded.status IN ('success','failed','cancelled') THEN ? ELSE task_steps.finished_at END,
  output_json=COALESCE(excluded.output_json,task_steps.output_json),
  error_message=COALESCE(excluded.error_message,task_steps.error_message),
  recovery_policy=excluded.recovery_policy`,
		step.TaskID,
		step.StepNo,
		step.StepCode,
		step.StepName,
		step.Status,
		step.Progress,
		now,
		step.OutputJSON,
		step.ErrorMessage,
		step.RecoveryPolicy,
		now,
	)
	return err
}

func (r TaskRepo) ListSteps(ctx context.Context, taskID int64) ([]domain.TaskStep, error) {
	rows, err := r.DB.QueryContext(ctx, `
SELECT id,task_id,step_no,step_code,COALESCE(step_name,''),status,progress,started_at,finished_at,
       COALESCE(output_json,''),COALESCE(error_message,''),COALESCE(recovery_policy,'')
FROM task_steps
WHERE task_id=?
ORDER BY step_no`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.TaskStep
	for rows.Next() {
		var step domain.TaskStep
		var started, finished sql.NullString
		if err := rows.Scan(&step.ID, &step.TaskID, &step.StepNo, &step.StepCode, &step.StepName, &step.Status, &step.Progress, &started, &finished, &step.OutputJSON, &step.ErrorMessage, &step.RecoveryPolicy); err != nil {
			return nil, err
		}
		if started.Valid {
			v, _ := time.Parse(time.RFC3339, started.String)
			step.StartedAt = &v
		}
		if finished.Valid {
			v, _ := time.Parse(time.RFC3339, finished.String)
			step.FinishedAt = &v
		}
		out = append(out, step)
	}
	return out, rows.Err()
}
