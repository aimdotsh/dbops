package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

type BackupJobRepo struct{ DB *sql.DB }

func (r BackupJobRepo) Create(ctx context.Context, job domain.BackupJob) (domain.BackupJob, error) {
	res, err := r.DB.ExecContext(ctx,
		"INSERT INTO backup_jobs(task_id,database_instance_id,backup_engine,backup_type,status,metadata_json) VALUES(?,?,?,?,?,?)",
		job.TaskID, job.DatabaseInstanceID, job.BackupEngine, job.BackupType, "pending", defaultJSON(job.MetadataJSON))
	if err != nil {
		return job, err
	}
	job.ID, _ = res.LastInsertId()
	return r.Get(ctx, job.ID)
}

func (r BackupJobRepo) Get(ctx context.Context, id int64) (domain.BackupJob, error) {
	return scanBackup(r.DB.QueryRowContext(ctx, backupSelect+" WHERE id=?", id))
}

func (r BackupJobRepo) List(ctx context.Context, instanceID int64) ([]domain.BackupJob, error) {
	query := backupSelect
	args := []any{}
	if instanceID > 0 {
		query += " WHERE database_instance_id=?"
		args = append(args, instanceID)
	}
	query += " ORDER BY id DESC"
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BackupJob
	for rows.Next() {
		j, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (r BackupJobRepo) MarkRunning(ctx context.Context, id int64) error {
	_, err := r.DB.ExecContext(ctx, "UPDATE backup_jobs SET status='running',started_at=? WHERE id=?",
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (r BackupJobRepo) MarkSuccess(ctx context.Context, id int64, size int64, path, checksum, metadata string) error {
	_, err := r.DB.ExecContext(ctx, "UPDATE backup_jobs SET status='success',finished_at=?,size_bytes=?,storage_path=?,checksum=?,metadata_json=?,error_message=NULL WHERE id=?",
		time.Now().UTC().Format(time.RFC3339), size, path, checksum, defaultJSON(metadata), id)
	return err
}

func (r BackupJobRepo) MarkFailed(ctx context.Context, id int64, msg string) error {
	_, err := r.DB.ExecContext(ctx, "UPDATE backup_jobs SET status='failed',finished_at=?,error_message=? WHERE id=?",
		time.Now().UTC().Format(time.RFC3339), msg, id)
	return err
}

const backupSelect = "SELECT id,task_id,database_instance_id,backup_engine,backup_type,started_at,finished_at,size_bytes,status,COALESCE(storage_path,''),COALESCE(checksum,''),metadata_json,COALESCE(error_message,'') FROM backup_jobs"

type backupScanner interface{ Scan(...any) error }

func scanBackup(s backupScanner) (domain.BackupJob, error) {
	var j domain.BackupJob
	var started, finished sql.NullString
	if err := s.Scan(&j.ID, &j.TaskID, &j.DatabaseInstanceID, &j.BackupEngine, &j.BackupType, &started, &finished, &j.SizeBytes, &j.Status, &j.StoragePath, &j.Checksum, &j.MetadataJSON, &j.ErrorMessage); err != nil {
		return j, err
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

func defaultJSON(v string) string {
	if v == "" {
		return "{}"
	}
	return v
}
