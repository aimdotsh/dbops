package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/aimdotsh/dbops/internal/domain"
	_ "modernc.org/sqlite"
)

func TestArchivePolicyZeroLagDisablesReplicaGuard(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE archive_policies (
id INTEGER PRIMARY KEY, name TEXT, source_instance_id INTEGER, source_database TEXT, source_table TEXT,
archive_column TEXT, where_template TEXT, retention_days INTEGER, destination_type TEXT,
destination_instance_id INTEGER, destination_database TEXT, destination_table TEXT, batch_size INTEGER,
txn_size INTEGER, sleep_ms INTEGER, max_replication_lag INTEGER, max_threads_running INTEGER,
delete_source INTEGER, enabled INTEGER, options_json TEXT, created_at TEXT, updated_at TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := (ArchivePolicyRepo{DB: db}).Create(context.Background(), domain.ArchivePolicy{
		Name: "standalone", SourceInstanceID: 1, SourceDatabase: "fixture", SourceTable: "events",
		WhereTemplate: "id > 0", DestinationType: "same_instance", MaxReplicationLag: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxReplicationLag != 0 {
		t.Fatalf("zero lag limit changed to %d", p.MaxReplicationLag)
	}
}
