package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

type ArchiveJobRepo struct{ DB *sql.DB }

func (r ArchiveJobRepo) Create(ctx context.Context, job domain.ArchiveJob) (domain.ArchiveJob, error) {
	res, err := r.DB.ExecContext(ctx, `
INSERT INTO archive_jobs(task_id,source_instance_id,source_database,source_table,destination_database,destination_table,status)
VALUES(?,?,?,?,?,?,?)`,
		job.TaskID, job.SourceInstanceID, job.SourceDatabase, job.SourceTable,
		nullString(job.DestinationDatabase), nullString(job.DestinationTable), "pending")
	if err != nil {
		return job, err
	}
	job.ID, _ = res.LastInsertId()
	return r.Get(ctx, job.ID)
}

func (r ArchiveJobRepo) Get(ctx context.Context, id int64) (domain.ArchiveJob, error) {
	return scanArchive(r.DB.QueryRowContext(ctx, archiveSelect+" WHERE id=?", id))
}

func (r ArchiveJobRepo) List(ctx context.Context, instanceID int64) ([]domain.ArchiveJob, error) {
	query := archiveSelect
	args := []any{}
	if instanceID > 0 {
		query += " WHERE source_instance_id=?"
		args = append(args, instanceID)
	}
	query += " ORDER BY id DESC"
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ArchiveJob
	for rows.Next() {
		j, err := scanArchive(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (r ArchiveJobRepo) MarkRunning(ctx context.Context, id int64) error {
	_, err := r.DB.ExecContext(ctx,
		"UPDATE archive_jobs SET status='running',started_at=? WHERE id=?",
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (r ArchiveJobRepo) MarkSuccess(ctx context.Context, id int64, scanned, archived, deleted, failed int64, verification string) error {
	_, err := r.DB.ExecContext(ctx, `
UPDATE archive_jobs
SET status='success',finished_at=?,scanned_rows=?,archived_rows=?,deleted_rows=?,failed_rows=?,
    verification_status=?,error_message=NULL
WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), scanned, archived, deleted, failed, verification, id)
	return err
}

func (r ArchiveJobRepo) MarkFailed(ctx context.Context, id int64, msg string) error {
	_, err := r.DB.ExecContext(ctx,
		"UPDATE archive_jobs SET status='failed',finished_at=?,error_message=? WHERE id=?",
		time.Now().UTC().Format(time.RFC3339), msg, id)
	return err
}

const archiveSelect = `SELECT id,task_id,source_instance_id,source_database,source_table,
COALESCE(destination_database,''),COALESCE(destination_table,''),status,
started_at,finished_at,scanned_rows,archived_rows,deleted_rows,failed_rows,
COALESCE(verification_status,''),COALESCE(error_message,'')
FROM archive_jobs`

type archiveScanner interface{ Scan(...any) error }

func scanArchive(s archiveScanner) (domain.ArchiveJob, error) {
	var j domain.ArchiveJob
	var started, finished sql.NullString
	if err := s.Scan(
		&j.ID, &j.TaskID, &j.SourceInstanceID, &j.SourceDatabase, &j.SourceTable,
		&j.DestinationDatabase, &j.DestinationTable, &j.Status,
		&started, &finished, &j.ScannedRows, &j.ArchivedRows, &j.DeletedRows, &j.FailedRows,
		&j.VerificationStatus, &j.ErrorMessage,
	); err != nil {
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
