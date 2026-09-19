package metrics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestStorePutLatestAndRange(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := `
CREATE TABLE metric_latest(resource_type TEXT,resource_id INTEGER,collected_at TEXT,payload_json TEXT,PRIMARY KEY(resource_type,resource_id));
CREATE TABLE metric_snapshots_5m(resource_type TEXT,resource_id INTEGER,bucket_ts TEXT,payload_json TEXT,PRIMARY KEY(resource_type,resource_id,bucket_ts)) WITHOUT ROWID;
CREATE TABLE metric_rollups_1h(resource_type TEXT,resource_id INTEGER,bucket_ts TEXT,payload_json TEXT,PRIMARY KEY(resource_type,resource_id,bucket_ts)) WITHOUT ROWID;
CREATE TABLE metric_rollups_1d(resource_type TEXT,resource_id INTEGER,bucket_ts TEXT,payload_json TEXT,PRIMARY KEY(resource_type,resource_id,bucket_ts)) WITHOUT ROWID;
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	now := time.Date(2026, 9, 19, 8, 3, 0, 0, time.UTC)
	if err := store.Put(context.Background(), "host", 7, now, map[string]any{"memory_used_pct": 42.5}); err != nil {
		t.Fatal(err)
	}
	latest, err := store.Latest(context.Background(), "host", 7)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Payload["memory_used_pct"].(float64) != 42.5 {
		t.Fatalf("unexpected latest: %+v", latest)
	}
	items, err := store.Range(context.Background(), "5m", "host", 7, now.Add(-time.Hour), now.Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one range item, got %d", len(items))
	}
}
