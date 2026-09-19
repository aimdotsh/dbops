package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
	"time"
)

type Policy struct {
	ID              int64           `json:"id"`
	Name            string          `json:"name"`
	TaskType        string          `json:"task_type"`
	Parameters      json.RawMessage `json:"parameters"`
	IntervalSeconds int             `json:"interval_seconds"`
	Enabled         bool            `json:"enabled"`
	NextRun         string          `json:"next_run"`
	LastError       string          `json:"last_error"`
	LastTaskID      *int64          `json:"last_task_id,omitempty"`
}
type Policies struct {
	DB       *sql.DB
	Handlers map[string]func(context.Context, json.RawMessage) (domain.Task, error)
}

func (p *Policies) Create(ctx context.Context, v Policy) (Policy, error) {
	if v.Name == "" || len(v.Name) > 128 || v.IntervalSeconds < 60 || v.IntervalSeconds > 366*86400 {
		return v, errors.New("name and interval_seconds between 60 and 31622400 required")
	}
	if p.Handlers[v.TaskType] == nil {
		return v, errors.New("unsupported scheduled task type")
	}
	if !json.Valid(v.Parameters) {
		return v, errors.New("parameters must be valid JSON")
	}
	v.NextRun = time.Now().UTC().Add(time.Duration(v.IntervalSeconds) * time.Second).Format(time.RFC3339)
	result, err := p.DB.ExecContext(ctx, `INSERT INTO backup_schedules(name,task_type,parameters_json,interval_seconds,enabled,next_run) VALUES(?,?,?,?,?,?)`, v.Name, v.TaskType, string(v.Parameters), v.IntervalSeconds, v.Enabled, v.NextRun)
	if err != nil {
		return v, err
	}
	v.ID, err = result.LastInsertId()
	return v, err
}
func (p *Policies) List(ctx context.Context) ([]Policy, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id,name,task_type,parameters_json,interval_seconds,enabled,next_run,last_error,last_task_id FROM backup_schedules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Policy{}
	for rows.Next() {
		var v Policy
		var raw string
		if err = rows.Scan(&v.ID, &v.Name, &v.TaskType, &raw, &v.IntervalSeconds, &v.Enabled, &v.NextRun, &v.LastError, &v.LastTaskID); err != nil {
			return nil, err
		}
		v.Parameters = json.RawMessage(raw)
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *Policies) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	r, err := p.DB.ExecContext(ctx, "UPDATE backup_schedules SET enabled=? WHERE id=?", enabled, id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if n != 1 {
		return errors.New("schedule not found")
	}
	return err
}
func (p *Policies) Tick(ctx context.Context) error {
	items, err := p.List(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, v := range items {
		if !v.Enabled {
			continue
		}
		due, err := time.Parse(time.RFC3339, v.NextRun)
		if err != nil {
			return err
		}
		if due.After(now) {
			continue
		}
		handler := p.Handlers[v.TaskType]
		if handler == nil {
			continue
		}
		key := fmt.Sprintf("schedule:%d:%s", v.ID, v.NextRun)
		t, runErr := handler(repository.WithOccurrence(ctx, key), v.Parameters)
		message := ""
		var taskID any
		if runErr != nil {
			message = runErr.Error()
		} else {
			taskID = t.ID
		}
		next := now.Add(time.Duration(v.IntervalSeconds) * time.Second).Format(time.RFC3339)
		if _, err = p.DB.ExecContext(ctx, "UPDATE backup_schedules SET next_run=?,last_error=?,last_task_id=? WHERE id=? AND next_run=?", next, message, taskID, v.ID, v.NextRun); err != nil {
			return err
		}
	}
	return nil
}
