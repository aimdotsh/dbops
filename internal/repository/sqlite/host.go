package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

type HostRepo struct{ DB *sql.DB }

func (r HostRepo) List(ctx context.Context) ([]domain.Host, error) {
	rows, err := r.DB.QueryContext(ctx, "SELECT id, hostname, ip_address, status, COALESCE(description,''), created_at, updated_at FROM hosts ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Host
	for rows.Next() {
		var h domain.Host
		var created, updated string
		if err := rows.Scan(&h.ID, &h.Hostname, &h.IPAddress, &h.Status, &h.Description, &created, &updated); err != nil {
			return nil, err
		}
		h.CreatedAt, _ = time.Parse(time.RFC3339, created)
		h.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r HostRepo) Create(ctx context.Context, h domain.Host) (domain.Host, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := r.DB.ExecContext(ctx, "INSERT INTO hosts(hostname, ip_address, status, description, created_at, updated_at) VALUES(?,?,?,?,?,?)",
		h.Hostname, h.IPAddress, "unknown", h.Description, now, now)
	if err != nil {
		return h, err
	}
	h.ID, _ = res.LastInsertId()
	h.Status = "unknown"
	h.CreatedAt, _ = time.Parse(time.RFC3339, now)
	h.UpdatedAt = h.CreatedAt
	return h, nil
}

func (r HostRepo) Get(ctx context.Context, id int64) (domain.Host, error) {
	var h domain.Host
	var created, updated string
	err := r.DB.QueryRowContext(ctx, "SELECT id, hostname, ip_address, status, COALESCE(description,''), created_at, updated_at FROM hosts WHERE id=?", id).
		Scan(&h.ID, &h.Hostname, &h.IPAddress, &h.Status, &h.Description, &created, &updated)
	if err != nil {
		return h, err
	}
	h.CreatedAt, _ = time.Parse(time.RFC3339, created)
	h.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return h, nil
}
