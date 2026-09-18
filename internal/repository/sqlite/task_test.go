package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}

	schema := `
CREATE TABLE tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_no TEXT NOT NULL UNIQUE,
  task_type TEXT NOT NULL,
  target_type TEXT,
  target_id INTEGER,
  status TEXT NOT NULL,
  progress INTEGER NOT NULL,
  parameters_json TEXT NOT NULL,
  result_json TEXT NOT NULL,
  agent_id INTEGER,
  created_at TEXT NOT NULL,
  queued_at TEXT,
  started_at TEXT,
  finished_at TEXT,
  timeout_seconds INTEGER NOT NULL,
  error_code TEXT,
  error_message TEXT,
  idempotency_key TEXT,
  lease_owner TEXT,
  lease_expires_at TEXT,
  recovery_policy TEXT NOT NULL
);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestClaimAndRecover(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := TaskRepo{DB: db}
	ctx := context.Background()

	task, err := repo.Create(ctx, domain.Task{
		TaskType:       "system.echo",
		ParametersJSON: `{"x":1}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := repo.ClaimNext(ctx, "worker-test", 1)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != task.ID || claimed.Status != "running" {
		t.Fatalf("unexpected claim: %+v", claimed)
	}

	if _, err := db.Exec(
		"UPDATE tasks SET lease_expires_at=? WHERE id=?",
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
		task.ID,
	); err != nil {
		t.Fatal(err)
	}

	n, err := repo.RecoverExpired(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected one recovered task, got %d", n)
	}
}
