package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateExistingArchiveJobsBeforeRetryIndex(t *testing.T) {
	meta, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer meta.Close()
	metrics, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer metrics.Close()
	if _, err := meta.Exec(`CREATE TABLE archive_jobs (id INTEGER PRIMARY KEY, task_id INTEGER, policy_id INTEGER NOT NULL, status TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(meta, metrics); err != nil {
		t.Fatal(err)
	}
	if _, err := meta.Exec(`INSERT INTO archive_jobs(policy_id,status,retry_of_job_id) VALUES(1,'pending',NULL)`); err != nil {
		t.Fatal(err)
	}
	var indexName string
	if err := meta.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_archive_jobs_retry_of'`).Scan(&indexName); err != nil {
		t.Fatal(err)
	}
	if indexName != "idx_archive_jobs_retry_of" {
		t.Fatalf("unexpected index %q", indexName)
	}
}
