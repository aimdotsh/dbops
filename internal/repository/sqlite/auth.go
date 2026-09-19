package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

type AuthRepo struct{ DB *sql.DB }

func (r AuthRepo) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	err := r.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

func (r AuthRepo) CreateUser(ctx context.Context, u domain.User, role string) (domain.User, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if u.Status == "" {
		u.Status = "active"
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return u, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
INSERT INTO users(username,password_hash,display_name,email,status,created_at,updated_at)
VALUES(?,?,?,?,?,?,?)`,
		strings.ToLower(strings.TrimSpace(u.Username)), u.PasswordHash, nullString(u.DisplayName),
		nullString(u.Email), u.Status, now, now)
	if err != nil {
		return u, err
	}
	u.ID, _ = res.LastInsertId()
	if role != "" {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO roles(name,description,created_at) VALUES(?,?,?) ON CONFLICT(name) DO NOTHING",
			role, role+" role", now); err != nil {
			return u, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO user_roles(user_id,role_id)
SELECT ?,id FROM roles WHERE name=?`, u.ID, role); err != nil {
			return u, err
		}
	}
	if err := tx.Commit(); err != nil {
		return u, err
	}
	return r.GetUser(ctx, u.ID)
}

func (r AuthRepo) GetUser(ctx context.Context, id int64) (domain.User, error) {
	u, err := scanUser(r.DB.QueryRowContext(ctx, userSelect+" WHERE id=?", id))
	if err != nil {
		return u, err
	}
	u.Roles, err = r.roles(ctx, u.ID)
	return u, err
}

func (r AuthRepo) GetUserByUsername(ctx context.Context, username string) (domain.User, error) {
	u, err := scanUser(r.DB.QueryRowContext(ctx, userSelect+" WHERE username=?", strings.ToLower(strings.TrimSpace(username))))
	if err != nil {
		return u, err
	}
	u.Roles, err = r.roles(ctx, u.ID)
	return u, err
}

func (r AuthRepo) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := r.DB.QueryContext(ctx, userSelect+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		u.Roles, err = r.roles(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r AuthRepo) UpdateLastLogin(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.DB.ExecContext(ctx, "UPDATE users SET last_login_at=?,updated_at=? WHERE id=?", now, now, id)
	return err
}

func (r AuthRepo) StoreRefreshToken(ctx context.Context, userID int64, tokenHash, expiresAt string) error {
	_, err := r.DB.ExecContext(ctx,
		"INSERT INTO refresh_tokens(user_id,token_hash,expires_at,created_at) VALUES(?,?,?,?)",
		userID, tokenHash, expiresAt, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (r AuthRepo) ConsumeRefreshToken(ctx context.Context, tokenHash, now string) (int64, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var userID int64
	err = tx.QueryRowContext(ctx, `
SELECT user_id FROM refresh_tokens
WHERE token_hash=? AND revoked_at IS NULL AND expires_at>?`, tokenHash, now).Scan(&userID)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE refresh_tokens SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL",
		now, tokenHash)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return 0, errors.New("refresh token already consumed")
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return userID, nil
}

func (r AuthRepo) roles(ctx context.Context, userID int64) ([]string, error) {
	rows, err := r.DB.QueryContext(ctx, `
SELECT r.name FROM roles r
JOIN user_roles ur ON ur.role_id=r.id
WHERE ur.user_id=?
ORDER BY r.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

const userSelect = `SELECT id,username,password_hash,COALESCE(display_name,''),COALESCE(email,''),status,last_login_at,created_at,updated_at FROM users`

func scanUser(s scanner) (domain.User, error) {
	var u domain.User
	var last sql.NullString
	var created, updated string
	if err := s.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Email, &u.Status, &last, &created, &updated); err != nil {
		return u, err
	}
	if last.Valid {
		v, _ := time.Parse(time.RFC3339, last.String)
		u.LastLoginAt = &v
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	u.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return u, nil
}
