package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type LockRepo struct{ DB *sql.DB }

func (r LockRepo) Acquire(ctx context.Context, key string, taskID int64, leaseSeconds int) (bool, error) {
	now := time.Now().UTC()
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM resource_locks WHERE lock_key=? AND lease_expires_at < ?", key, now.Format(time.RFC3339)); err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO resource_locks(lock_key,owner_task_id,lease_expires_at,acquired_at,metadata_json) VALUES(?,?,?,?, '{}')",
		key, taskID, now.Add(time.Duration(leaseSeconds)*time.Second).Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return false, nil
		}
		return false, err
	}
	return true, tx.Commit()
}

func (r LockRepo) Release(ctx context.Context, key string, taskID int64) error {
	_, err := r.DB.ExecContext(ctx, "DELETE FROM resource_locks WHERE lock_key=? AND owner_task_id=?", key, taskID)
	return err
}
