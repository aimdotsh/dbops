package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
)

type TaskRepo struct{ DB *sql.DB }

var taskClaimMu sync.Mutex

func (r TaskRepo) Create(ctx context.Context, t domain.Task) (domain.Task, error) {
	occurrence := repository.Occurrence(ctx)
	if occurrence != "" {
		t.IdempotencyKey = &occurrence
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if t.TaskNo == "" {
		t.TaskNo = fmt.Sprintf("TASK-%d", time.Now().UTC().UnixNano())
	}
	if t.ParametersJSON == "" {
		t.ParametersJSON = "{}"
	}
	res, err := r.DB.ExecContext(ctx, "INSERT INTO tasks(task_no,task_type,target_type,target_id,status,progress,parameters_json,result_json,agent_id,idempotency_key,created_at,queued_at,timeout_seconds,recovery_policy) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		t.TaskNo, t.TaskType, nullString(t.TargetType), t.TargetID, "queued", 0, t.ParametersJSON, "{}", t.AgentID, t.IdempotencyKey, now, now, 3600, "verify_before_retry")
	if err != nil {
		if occurrence != "" {
			var existing int64
			if lookupErr := r.DB.QueryRowContext(ctx, "SELECT id FROM tasks WHERE idempotency_key=?", occurrence).Scan(&existing); lookupErr == nil {
				return r.Get(ctx, existing)
			}
		}
		return t, err
	}
	t.ID, _ = res.LastInsertId()
	return r.Get(ctx, t.ID)
}

func (r TaskRepo) Get(ctx context.Context, id int64) (domain.Task, error) {
	return scanTask(r.DB.QueryRowContext(ctx, taskSelect+" WHERE id=?", id))
}

func (r TaskRepo) List(ctx context.Context, limit int) ([]domain.Task, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.DB.QueryContext(ctx, taskSelect+" ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r TaskRepo) ClaimNext(ctx context.Context, owner string, leaseSeconds int) (*domain.Task, error) {
	taskClaimMu.Lock()
	defer taskClaimMu.Unlock()

	now := time.Now().UTC()
	// A single write statement avoids a deferred transaction's read-to-write upgrade.
	// Serialize actions on an agent, while reserving a lane for archive controls.
	row := r.DB.QueryRowContext(ctx, `UPDATE tasks SET status='running',
 started_at=COALESCE(started_at,?),lease_owner=?,lease_expires_at=?
 WHERE id=(SELECT q.id FROM tasks q WHERE q.status='queued' AND
 (q.task_type='mysql.archive.control' OR NOT EXISTS
 (SELECT 1 FROM tasks active WHERE active.status IN ('running','interrupted')
 AND active.task_type<>'mysql.archive.control' AND
 (q.agent_id=active.agent_id OR q.task_type IN ('mysql.replication.create','mysql.restore_new') OR active.task_type IN ('mysql.replication.create','mysql.restore_new')))) ORDER BY q.id LIMIT 1)
 RETURNING id`, now.Format(time.RFC3339), owner, now.Add(time.Duration(leaseSeconds)*time.Second).Format(time.RFC3339))
	var id int64
	if err := row.Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	t, err := r.Get(ctx, id)
	return &t, err
}

func (r TaskRepo) UpdateStatus(ctx context.Context, id int64, status string, progress int, resultJSON, errMsg string) error {
	if resultJSON == "" {
		resultJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.DB.ExecContext(ctx, "UPDATE tasks SET status=?, progress=?, result_json=?, error_message=NULLIF(?,''), finished_at=CASE WHEN ? IN ('success','failed','cancelled','timeout') THEN ? ELSE finished_at END, lease_owner=NULL, lease_expires_at=NULL WHERE id=?",
		status, progress, resultJSON, errMsg, status, now, id)
	return err
}

func (r TaskRepo) UpdateProgress(ctx context.Context, id int64, progress int) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	_, err := r.DB.ExecContext(ctx, "UPDATE tasks SET progress=? WHERE id=?", progress, id)
	return err
}

func (r TaskRepo) RecoverExpired(ctx context.Context) (int64, error) {
	res, err := r.DB.ExecContext(ctx, "UPDATE tasks SET status='interrupted', lease_owner=NULL, lease_expires_at=NULL WHERE status='running' AND lease_expires_at IS NOT NULL AND lease_expires_at < ?",
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	// A cancelled context or process crash can prevent backup handlers from
	// recording failure. Reconcile only unfinished records, never verified backups.
	_, err = r.DB.ExecContext(ctx, `UPDATE backup_jobs SET status='failed',finished_at=?,
 error_message='task ended without a verified backup; inspect agent output before retry'
 WHERE status IN ('pending','running') AND task_id IN
 (SELECT id FROM tasks WHERE status IN ('interrupted','cancelled','failed','timeout'))`,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const taskSelect = "SELECT id,task_no,task_type,COALESCE(target_type,''),target_id,status,progress,parameters_json,result_json,agent_id,idempotency_key,created_at,queued_at,started_at,finished_at,lease_owner,lease_expires_at,error_code,error_message FROM tasks"

type scanner interface{ Scan(...any) error }

func scanTask(s scanner) (domain.Task, error) {
	var t domain.Task
	var created string
	var queued, started, finished, leaseExp sql.NullString
	var leaseOwner, errCode, errMsg, idempotencyKey sql.NullString
	err := s.Scan(&t.ID, &t.TaskNo, &t.TaskType, &t.TargetType, &t.TargetID, &t.Status, &t.Progress, &t.ParametersJSON, &t.ResultJSON, &t.AgentID, &idempotencyKey,
		&created, &queued, &started, &finished, &leaseOwner, &leaseExp, &errCode, &errMsg)
	if err != nil {
		return t, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if queued.Valid {
		v, _ := time.Parse(time.RFC3339, queued.String)
		t.QueuedAt = &v
	}
	if started.Valid {
		v, _ := time.Parse(time.RFC3339, started.String)
		t.StartedAt = &v
	}
	if finished.Valid {
		v, _ := time.Parse(time.RFC3339, finished.String)
		t.FinishedAt = &v
	}
	if leaseExp.Valid {
		v, _ := time.Parse(time.RFC3339, leaseExp.String)
		t.LeaseExpiresAt = &v
	}
	if idempotencyKey.Valid {
		v := idempotencyKey.String
		t.IdempotencyKey = &v
	}
	if leaseOwner.Valid {
		v := leaseOwner.String
		t.LeaseOwner = &v
	}
	if errCode.Valid {
		v := errCode.String
		t.ErrorCode = &v
	}
	if errMsg.Valid {
		v := errMsg.String
		t.ErrorMessage = &v
	}
	return t, nil
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// RenewLease cannot resurrect an interrupted task or another worker's claim.
func (r TaskRepo) RenewLease(ctx context.Context, id int64, owner string, seconds int) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE tasks SET lease_expires_at=? WHERE id=? AND status='running' AND lease_owner=?`, time.Now().UTC().Add(time.Duration(seconds)*time.Second).Format(time.RFC3339), id, owner)
	return requireOwned(res, err)
}

func (r TaskRepo) FinishOwned(ctx context.Context, id int64, owner, status, result, message string) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE tasks SET status=?,progress=CASE WHEN ?='success' THEN 100 ELSE progress END,result_json=?,error_message=NULLIF(?,''),finished_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND status='running' AND lease_owner=?`, status, status, result, message, time.Now().UTC().Format(time.RFC3339), id, owner)
	return requireOwned(res, err)
}

func requireOwned(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("task lease is no longer owned")
	}
	return nil
}
