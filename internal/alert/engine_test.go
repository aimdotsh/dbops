package alert

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	metricstore "github.com/aimdotsh/dbops/internal/metrics"
	_ "modernc.org/sqlite"
)

func TestHeartbeatAlertLifecycle(t *testing.T) {
	meta, err := sql.Open("sqlite", "file:alert-meta?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer meta.Close()
	metricsDB, err := sql.Open("sqlite", "file:alert-metrics?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer metricsDB.Close()

	if _, err := meta.Exec(`
CREATE TABLE alert_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  resource_type TEXT NOT NULL,
  resource_id INTEGER NOT NULL,
  fingerprint TEXT NOT NULL,
  status TEXT NOT NULL,
  severity TEXT NOT NULL,
  message TEXT NOT NULL,
  started_at TEXT NOT NULL,
  last_seen_at TEXT,
  resolved_at TEXT,
  metadata_json TEXT NOT NULL DEFAULT '{}'
);
`); err != nil {
		t.Fatal(err)
	}

	if _, err := metricsDB.Exec(`
CREATE TABLE metric_latest(resource_type TEXT,resource_id INTEGER,collected_at TEXT,payload_json TEXT,PRIMARY KEY(resource_type,resource_id));
CREATE TABLE metric_snapshots_5m(resource_type TEXT,resource_id INTEGER,bucket_ts TEXT,payload_json TEXT,PRIMARY KEY(resource_type,resource_id,bucket_ts)) WITHOUT ROWID;
CREATE TABLE metric_rollups_1h(resource_type TEXT,resource_id INTEGER,bucket_ts TEXT,payload_json TEXT,PRIMARY KEY(resource_type,resource_id,bucket_ts)) WITHOUT ROWID;
CREATE TABLE metric_rollups_1d(resource_type TEXT,resource_id INTEGER,bucket_ts TEXT,payload_json TEXT,PRIMARY KEY(resource_type,resource_id,bucket_ts)) WITHOUT ROWID;
`); err != nil {
		t.Fatal(err)
	}

	store := metricstore.NewStore(metricsDB)
	ctx := context.Background()
	if err := store.Put(ctx, "host", 1, time.Now().UTC().Add(-2*time.Minute), map[string]any{
		"memory_used_pct": 20.0,
		"root_used_pct":   20.0,
	}); err != nil {
		t.Fatal(err)
	}

	engine := New(slog.Default(), true, 1, meta, store)
	if err := engine.EvaluateOnce(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := engine.List(ctx, "FIRING", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Severity != "P1" {
		t.Fatalf("expected heartbeat P1 alert, got %+v", events)
	}

	if err := store.Put(ctx, "host", 1, time.Now().UTC(), map[string]any{
		"memory_used_pct": 20.0,
		"root_used_pct":   20.0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := engine.EvaluateOnce(ctx); err != nil {
		t.Fatal(err)
	}
	resolved, err := engine.List(ctx, "RESOLVED", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected resolved heartbeat alert, got %+v", resolved)
	}
}
