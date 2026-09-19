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
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.ServerIDReservation{}, err
	}
	defer tx.Rollback()

	var existing domain.ServerIDReservation
	var created string
	var instanceID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
SELECT id,server_id,host_id,port,COALESCE(task_id,0),instance_id,status,created_at
FROM mysql_server_ids WHERE host_id=? AND port=?`, hostID, port).
		Scan(&existing.ID, &existing.ServerID, &existing.HostID, &existing.Port, &existing.TaskID, &instanceID, &existing.Status, &created)
	if err == nil {
		existing.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if instanceID.Valid {
			v := instanceID.Int64
			existing.InstanceID = &v
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.ServerIDReservation{}, err
	}

	var next int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(server_id),1000)+1 FROM mysql_server_ids").Scan(&next); err != nil {
		return domain.ServerIDReservation{}, err
	}
	if next > 4294967295 {
		return domain.ServerIDReservation{}, errors.New("server_id pool exhausted")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.ExecContext(ctx, `
INSERT INTO mysql_server_ids(server_id,host_id,port,task_id,status,created_at)
VALUES(?,?,?,?, 'reserved', ?)`, next, hostID, port, taskID, now)
	if err != nil {
		return domain.ServerIDReservation{}, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return domain.ServerIDReservation{}, err
	}
	return domain.ServerIDReservation{
		ID: id, ServerID: next, HostID: hostID, Port: port, TaskID: taskID,
		Status: "reserved", CreatedAt: time.Now().UTC(),
	}, nil
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
