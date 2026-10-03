package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

type ArchivePolicyRepo struct{ DB *sql.DB }
type ArchiveJobRepo struct{ DB *sql.DB }

func (r ArchivePolicyRepo) Create(ctx context.Context, p domain.ArchivePolicy) (domain.ArchivePolicy, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if p.BatchSize <= 0 {
		p.BatchSize = 5000
	}
	if p.SleepMS < 0 {
		p.SleepMS = 0
	}
	if p.OptionsJSON == "" {
		p.OptionsJSON = "{}"
	}
	res, err := r.DB.ExecContext(ctx, `
INSERT INTO archive_policies(
 name,source_instance_id,source_database,source_table,archive_column,where_template,retention_days,
 destination_type,destination_instance_id,destination_database,destination_table,batch_size,txn_size,sleep_ms,
 max_replication_lag,max_threads_running,delete_source,enabled,options_json,created_at,updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.Name, p.SourceInstanceID, p.SourceDatabase, p.SourceTable, nullString(p.ArchiveColumn), p.WhereTemplate, nullableInt(p.RetentionDays),
		p.DestinationType, p.DestinationInstanceID, nullString(p.DestinationDatabase), nullString(p.DestinationTable),
		p.BatchSize, nullableInt(p.TxnSize), p.SleepMS, p.MaxReplicationLag, nullableInt(p.MaxThreadsRunning),
		boolInt(p.DeleteSource), boolInt(p.Enabled), p.OptionsJSON, now, now)
	if err != nil {
		return p, err
	}
	p.ID, _ = res.LastInsertId()
	return r.Get(ctx, p.ID)
}

func (r ArchivePolicyRepo) Get(ctx context.Context, id int64) (domain.ArchivePolicy, error) {
	return scanArchivePolicy(r.DB.QueryRowContext(ctx, archivePolicySelect+" WHERE id=?", id))
}

func (r ArchivePolicyRepo) List(ctx context.Context) ([]domain.ArchivePolicy, error) {
	rows, err := r.DB.QueryContext(ctx, archivePolicySelect+" ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ArchivePolicy
	for rows.Next() {
		p, err := scanArchivePolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const archivePolicySelect = `SELECT id,name,source_instance_id,source_database,source_table,COALESCE(archive_column,''),where_template,
COALESCE(retention_days,0),destination_type,destination_instance_id,COALESCE(destination_database,''),COALESCE(destination_table,''),
batch_size,COALESCE(txn_size,0),sleep_ms,max_replication_lag,COALESCE(max_threads_running,0),delete_source,enabled,options_json,created_at,updated_at
FROM archive_policies`

func scanArchivePolicy(s scanner) (domain.ArchivePolicy, error) {
	var p domain.ArchivePolicy
	var deleteSource, enabled int
	var created, updated string
	if err := s.Scan(
		&p.ID, &p.Name, &p.SourceInstanceID, &p.SourceDatabase, &p.SourceTable, &p.ArchiveColumn, &p.WhereTemplate,
		&p.RetentionDays, &p.DestinationType, &p.DestinationInstanceID, &p.DestinationDatabase, &p.DestinationTable,
		&p.BatchSize, &p.TxnSize, &p.SleepMS, &p.MaxReplicationLag, &p.MaxThreadsRunning,
		&deleteSource, &enabled, &p.OptionsJSON, &created, &updated,
	); err != nil {
		return p, err
	}
	p.DeleteSource = deleteSource == 1
	p.Enabled = enabled == 1
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return p, nil
}

func (r ArchiveJobRepo) Create(ctx context.Context, j domain.ArchiveJob) (domain.ArchiveJob, error) {
	res, err := r.DB.ExecContext(ctx,
		"INSERT INTO archive_jobs(task_id,policy_id,status) VALUES(?,?,?)",
		j.TaskID, j.PolicyID, "pending")
	if err != nil {
		return j, err
	}
	j.ID, _ = res.LastInsertId()
	return r.Get(ctx, j.ID)
}

func (r ArchiveJobRepo) Get(ctx context.Context, id int64) (domain.ArchiveJob, error) {
	return scanArchiveJob(r.DB.QueryRowContext(ctx, archiveJobSelect+" WHERE id=?", id))
}

func (r ArchiveJobRepo) List(ctx context.Context, policyID int64) ([]domain.ArchiveJob, error) {
	query := archiveJobSelect
	args := []any{}
	if policyID > 0 {
		query += " WHERE policy_id=?"
		args = append(args, policyID)
	}
	query += " ORDER BY id DESC"
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ArchiveJob
	for rows.Next() {
		j, err := scanArchiveJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (r ArchiveJobRepo) AttachTask(ctx context.Context, id, taskID int64) error {
	_, err := r.DB.ExecContext(ctx, "UPDATE archive_jobs SET task_id=? WHERE id=?", taskID, id)
	return err
}

func (r ArchiveJobRepo) UpdateState(
	ctx context.Context, id int64, status string,
	scanned, archived, deleted, failed, speed int64,
	lastKey, pauseReason, errMsg string,
) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if status == "success" || status == "failed" || status == "stopped" {
		_, err := r.DB.ExecContext(ctx, `
UPDATE archive_jobs SET status=?,started_at=COALESCE(started_at,?),finished_at=?,
scanned_rows=?,archived_rows=?,deleted_rows=?,failed_rows=?,speed_rows_sec=?,
last_processed_key=NULLIF(?,''),pause_reason=NULLIF(?,''),verification_status=?,
error_message=NULLIF(?, '')
WHERE id=?`,
			status, now, now, scanned, archived, deleted, failed, speed, lastKey, pauseReason,
			verificationStatus(status), errMsg, id)
		return err
	}
	_, err := r.DB.ExecContext(ctx, `
UPDATE archive_jobs SET status=?,started_at=COALESCE(started_at,?),
scanned_rows=?,archived_rows=?,deleted_rows=?,failed_rows=?,speed_rows_sec=?,
last_processed_key=NULLIF(?,''),pause_reason=NULLIF(?,''),error_message=NULLIF(?, '')
WHERE id=?`,
		status, now, scanned, archived, deleted, failed, speed, lastKey, pauseReason, errMsg, id)
	return err
}

func verificationStatus(status string) string {
	if status == "success" {
		return "verified"
	}
	if status == "failed" {
		return "failed"
	}
	return ""
}

const archiveJobSelect = `SELECT id,task_id,policy_id,status,started_at,finished_at,scanned_rows,archived_rows,deleted_rows,
failed_rows,COALESCE(speed_rows_sec,0),COALESCE(last_processed_key,''),COALESCE(pause_reason,''),
COALESCE(verification_status,''),COALESCE(error_message,'')
FROM archive_jobs`

func scanArchiveJob(s scanner) (domain.ArchiveJob, error) {
	var j domain.ArchiveJob
	var taskID sql.NullInt64
	var started, finished sql.NullString
	if err := s.Scan(
		&j.ID, &taskID, &j.PolicyID, &j.Status, &started, &finished,
		&j.ScannedRows, &j.ArchivedRows, &j.DeletedRows, &j.FailedRows,
		&j.SpeedRowsSec, &j.LastProcessedKey, &j.PauseReason,
		&j.VerificationStatus, &j.ErrorMessage,
	); err != nil {
		return j, err
	}
	if taskID.Valid {
		v := taskID.Int64
		j.TaskID = &v
	}
	if started.Valid {
		v, _ := time.Parse(time.RFC3339, started.String)
		j.StartedAt = &v
	}
	if finished.Valid {
		v, _ := time.Parse(time.RFC3339, finished.String)
		j.FinishedAt = &v
	}
	return j, nil
}

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
