package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/aimdotsh/dbops/internal/repository/sqlite"
	_ "modernc.org/sqlite"
)

func authTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:auth-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	schema := `
CREATE TABLE users (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 username TEXT NOT NULL UNIQUE,
 password_hash TEXT NOT NULL,
 display_name TEXT,
 email TEXT,
 status TEXT NOT NULL DEFAULT 'active',
 last_login_at TEXT,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE roles (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 name TEXT NOT NULL UNIQUE,
 description TEXT,
 created_at TEXT NOT NULL
);
CREATE TABLE user_roles (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
 PRIMARY KEY(user_id,role_id)
);
CREATE TABLE refresh_tokens (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 token_hash TEXT NOT NULL UNIQUE,
 expires_at TEXT NOT NULL,
 revoked_at TEXT,
 created_at TEXT NOT NULL
);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestBootstrapLoginRefreshRotation(t *testing.T) {
	db := authTestDB(t)
	defer db.Close()

	repo := sqlite.AuthRepo{DB: db}
	svc, err := New(repo, Config{
		Enabled:                true,
		JWTSecret:              "0123456789abcdef0123456789abcdef",
		AccessTTL:              time.Minute,
		RefreshTTL:             time.Hour,
		BootstrapAdminUsername: "admin",
		BootstrapAdminPassword: "A-strong-test-password-123",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap must be idempotent: %v", err)
	}

	pair, err := svc.Login(ctx, "ADMIN", "A-strong-test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("missing issued tokens")
	}
	claims, err := svc.ParseAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Username != "admin" || !HasAnyRole(claims.Roles, RoleSuperAdmin) {
		t.Fatalf("unexpected claims: %+v", claims)
	}

	rotated, err := svc.Refresh(ctx, pair.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.RefreshToken == "" || rotated.RefreshToken == pair.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}
	if _, err := svc.Refresh(ctx, pair.RefreshToken); err == nil {
		t.Fatal("consumed refresh token must not be reusable")
	}
}

func TestCreateUserRoleAndLogin(t *testing.T) {
	db := authTestDB(t)
	defer db.Close()

	repo := sqlite.AuthRepo{DB: db}
	svc, err := New(repo, Config{
		Enabled:                true,
		JWTSecret:              "0123456789abcdef0123456789abcdef",
		AccessTTL:              time.Minute,
		RefreshTTL:             time.Hour,
		BootstrapAdminUsername: "admin",
		BootstrapAdminPassword: "A-strong-test-password-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}

	user, err := svc.CreateUser(ctx, CreateUserRequest{
		Username: "viewer1", Password: "Viewer-password-123", Role: RoleViewer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(user.Roles) != 1 || user.Roles[0] != RoleViewer {
		t.Fatalf("unexpected roles: %+v", user.Roles)
	}
	pair, err := svc.Login(ctx, "viewer1", "Viewer-password-123")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := svc.ParseAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if !HasAnyRole(claims.Roles, RoleViewer) || HasAnyRole(claims.Roles, RoleDBA) {
		t.Fatalf("unexpected viewer claims: %+v", claims)
	}
}
