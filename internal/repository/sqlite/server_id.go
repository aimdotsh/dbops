package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

type ServerIDRepo struct{ DB *sql.DB }

func (r ServerIDRepo) Reserve(ctx context.Context, taskID, hostID int64, port int) (domain.ServerIDReservation, error) {
	// Allocate under one SQLite write statement. A deferred read transaction can
	// fail with SQLITE_BUSY_SNAPSHOT when a second host allocates concurrently.
	var reservation domain.ServerIDReservation
	var created string
	var instanceID sql.NullInt64
	err := r.DB.QueryRowContext(ctx, `
 INSERT INTO mysql_server_ids(server_id,host_id,port,task_id,status,created_at)
 SELECT COALESCE(MAX(server_id),1000)+1,?,?,?,'reserved',?
 FROM mysql_server_ids HAVING COALESCE(MAX(server_id),1000)<4294967295
 ON CONFLICT(host_id,port) DO UPDATE SET host_id=excluded.host_id
 RETURNING id,server_id,host_id,port,COALESCE(task_id,0),instance_id,status,created_at`,
		hostID, port, taskID, time.Now().UTC().Format(time.RFC3339)).Scan(
		&reservation.ID, &reservation.ServerID, &reservation.HostID, &reservation.Port, &reservation.TaskID, &instanceID, &reservation.Status, &created)
	if errors.Is(err, sql.ErrNoRows) {
		// An exhausted pool can still return an already-reserved host/port.
		err = r.DB.QueryRowContext(ctx, `SELECT id,server_id,host_id,port,COALESCE(task_id,0),instance_id,status,created_at FROM mysql_server_ids WHERE host_id=? AND port=?`, hostID, port).Scan(&reservation.ID, &reservation.ServerID, &reservation.HostID, &reservation.Port, &reservation.TaskID, &instanceID, &reservation.Status, &created)
		if errors.Is(err, sql.ErrNoRows) {
			return reservation, errors.New("server_id pool exhausted")
		}
	}
	if err != nil {
		return reservation, err
	}
	reservation.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if instanceID.Valid {
		v := instanceID.Int64
		reservation.InstanceID = &v
	}
	return reservation, nil
}

func (r ServerIDRepo) BindInstance(ctx context.Context, reservationID, instanceID int64) error {
	_, err := r.DB.ExecContext(ctx,
		"UPDATE mysql_server_ids SET instance_id=?,status='bound' WHERE id=?",
		instanceID, reservationID)
	return err
}

func (r ServerIDRepo) MarkFailed(ctx context.Context, reservationID int64) error {
	_, err := r.DB.ExecContext(ctx,
		"UPDATE mysql_server_ids SET status='failed' WHERE id=? AND status='reserved'",
		reservationID)
	return err
}
