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
);
CREATE TABLE backup_jobs (
 id INTEGER PRIMARY KEY, task_id INTEGER, status TEXT, finished_at TEXT, error_message TEXT
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

func TestLeaseOwnershipAndAgentSerialization(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	repo := TaskRepo{DB: db}
	ctx := context.Background()
	agent := int64(3)
	first, err := repo.Create(ctx, domain.Task{TaskType: "mysql.install", AgentID: &agent})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Create(ctx, domain.Task{TaskType: "mysql.backup", AgentID: &agent})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNext(ctx, "one", 60)
	if err != nil || claimed == nil || claimed.ID != first.ID {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	if next, err := repo.ClaimNext(ctx, "two", 60); err != nil || next != nil {
		t.Fatalf("concurrent same-agent operation: %v %v", next, err)
	}
	control, err := repo.Create(ctx, domain.Task{TaskType: "mysql.archive.control", AgentID: &agent})
	if err != nil {
		t.Fatal(err)
	}
	if next, err := repo.ClaimNext(ctx, "control", 60); err != nil || next == nil || next.ID != control.ID {
		t.Fatalf("control lane blocked: %v %v", next, err)
	}
	if err = repo.RenewLease(ctx, first.ID, "wrong-owner", 60); err == nil {
		t.Fatal("wrong owner renewed lease")
	}
	if err = repo.RenewLease(ctx, first.ID, "one", 60); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishOwned(ctx, first.ID, "wrong-owner", "success", "{}", ""); err == nil {
		t.Fatal("wrong owner completed task")
	}
	if err = repo.FinishOwned(ctx, first.ID, "one", "success", "{}", ""); err != nil {
		t.Fatal(err)
	}
	if err = repo.RenewLease(ctx, first.ID, "one", 60); err == nil {
		t.Fatal("terminal task resurrected")
	}
}

func TestNewHostRestoreLocksBothHostsUntilInterruptedReview(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	repo := TaskRepo{DB: db}
	ctx := context.Background()
	source, target := int64(1), int64(2)
	job, err := repo.Create(ctx, domain.Task{TaskType: "mysql.restore_new", AgentID: &target})
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimNext(ctx, "restore", 60); err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatal(claimed, err)
	}
	if _, err := repo.Create(ctx, domain.Task{TaskType: "mysql.backup", AgentID: &source}); err != nil {
		t.Fatal(err)
	}
	if next, err := repo.ClaimNext(ctx, "other", 60); err != nil || next != nil {
		t.Fatal("source not locked", next, err)
	}
	if err := repo.UpdateStatus(ctx, job.ID, "interrupted", 0, "{}", "review required"); err != nil {
		t.Fatal(err)
	}
	if next, err := repo.ClaimNext(ctx, "other", 60); err != nil || next != nil {
		t.Fatal("interrupted restore not locked", next, err)
	}
	if err := repo.UpdateStatus(ctx, job.ID, "cancelled", 0, "{}", "reviewed"); err != nil {
		t.Fatal(err)
	}
	if next, err := repo.ClaimNext(ctx, "other", 60); err != nil || next == nil {
		t.Fatal("review did not release lock", next, err)
	}
}
