package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

type MySQLReplicationRepo struct{ DB *sql.DB }

func (r MySQLReplicationRepo) Create(ctx context.Context, v domain.MySQLReplication) (domain.MySQLReplication, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if v.Status == "" {
		v.Status = "configuring"
	}
	res, err := r.DB.ExecContext(ctx, `
INSERT INTO mysql_replications(
 primary_instance_id,replica_instance_id,replication_credential_id,gtid_enabled,
 io_thread_status,sql_thread_status,replication_lag_seconds,source_uuid,last_io_error,last_sql_error,
 last_checked_at,status,created_at,updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.PrimaryInstanceID, v.ReplicaInstanceID, v.ReplicationCredentialID, boolInt(v.GTIDEnabled),
		nullString(v.IOThreadStatus), nullString(v.SQLThreadStatus), v.ReplicationLagSeconds, nullString(v.SourceUUID),
		nullString(v.LastIOError), nullString(v.LastSQLError), nil, v.Status, now, now)
	if err != nil {
		return v, err
	}
	v.ID, _ = res.LastInsertId()
	return r.Get(ctx, v.ID)
}

func (r MySQLReplicationRepo) Get(ctx context.Context, id int64) (domain.MySQLReplication, error) {
	return scanReplication(r.DB.QueryRowContext(ctx, replicationSelect+" WHERE id=?", id))
}

func (r MySQLReplicationRepo) List(ctx context.Context) ([]domain.MySQLReplication, error) {
	rows, err := r.DB.QueryContext(ctx, replicationSelect+" ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MySQLReplication
	for rows.Next() {
		v, err := scanReplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r MySQLReplicationRepo) UpdateStatus(ctx context.Context, id int64, v domain.MySQLReplication) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.DB.ExecContext(ctx, `
UPDATE mysql_replications SET
 io_thread_status=?,sql_thread_status=?,replication_lag_seconds=?,source_uuid=?,
 last_io_error=?,last_sql_error=?,last_checked_at=?,status=?,updated_at=?
WHERE id=?`,
		nullString(v.IOThreadStatus), nullString(v.SQLThreadStatus), v.ReplicationLagSeconds,
		nullString(v.SourceUUID), nullString(v.LastIOError), nullString(v.LastSQLError),
		now, v.Status, now, id)
	return err
}

const replicationSelect = `
SELECT id,primary_instance_id,replica_instance_id,replication_credential_id,gtid_enabled,
 COALESCE(io_thread_status,''),COALESCE(sql_thread_status,''),replication_lag_seconds,
 COALESCE(source_uuid,''),COALESCE(last_io_error,''),COALESCE(last_sql_error,''),
 last_checked_at,status,created_at,updated_at
FROM mysql_replications`

type replicationScanner interface{ Scan(...any) error }

func scanReplication(s replicationScanner) (domain.MySQLReplication, error) {
	var v domain.MySQLReplication
	var credentialID, lag sql.NullInt64
	var gtid int
	var checked sql.NullString
	var created, updated string
	err := s.Scan(
		&v.ID, &v.PrimaryInstanceID, &v.ReplicaInstanceID, &credentialID, &gtid,
		&v.IOThreadStatus, &v.SQLThreadStatus, &lag, &v.SourceUUID, &v.LastIOError, &v.LastSQLError,
		&checked, &v.Status, &created, &updated,
	)
	if err != nil {
		return v, err
	}
	v.GTIDEnabled = gtid != 0
	if credentialID.Valid {
		x := credentialID.Int64
		v.ReplicationCredentialID = &x
	}
	if lag.Valid {
		x := lag.Int64
		v.ReplicationLagSeconds = &x
	}
	if checked.Valid {
		x, _ := time.Parse(time.RFC3339, checked.String)
		v.LastCheckedAt = &x
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339, created)
	v.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return v, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
