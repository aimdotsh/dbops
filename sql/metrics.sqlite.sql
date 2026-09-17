-- DBOps V1.0 Metrics History Schema (SQLite)
-- 默认文件: /data/dbops/metrics.db
-- 30s 原始状态主要保存在内存，仅保存 latest 与降采样结果。

PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS metrics_schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS metric_latest (
  resource_type TEXT NOT NULL,
  resource_id INTEGER NOT NULL,
  collected_at TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  PRIMARY KEY(resource_type,resource_id)
);

CREATE TABLE IF NOT EXISTS metric_snapshots_5m (
  resource_type TEXT NOT NULL,
  resource_id INTEGER NOT NULL,
  bucket_ts TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  PRIMARY KEY(resource_type,resource_id,bucket_ts)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_metric_5m_time ON metric_snapshots_5m(bucket_ts);

CREATE TABLE IF NOT EXISTS metric_rollups_1h (
  resource_type TEXT NOT NULL,
  resource_id INTEGER NOT NULL,
  bucket_ts TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  PRIMARY KEY(resource_type,resource_id,bucket_ts)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_metric_1h_time ON metric_rollups_1h(bucket_ts);

CREATE TABLE IF NOT EXISTS metric_rollups_1d (
  resource_type TEXT NOT NULL,
  resource_id INTEGER NOT NULL,
  bucket_ts TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  PRIMARY KEY(resource_type,resource_id,bucket_ts)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_metric_1d_time ON metric_rollups_1d(bucket_ts);

CREATE TABLE IF NOT EXISTS capacity_daily (
  resource_type TEXT NOT NULL,
  resource_id INTEGER NOT NULL,
  capacity_key TEXT NOT NULL,
  day TEXT NOT NULL,
  total_bytes INTEGER,
  used_bytes INTEGER,
  free_bytes INTEGER,
  extra_json TEXT NOT NULL DEFAULT '{}',
  PRIMARY KEY(resource_type,resource_id,capacity_key,day)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS retention_state (
  series_name TEXT PRIMARY KEY,
  last_run_at TEXT,
  last_deleted_rows INTEGER NOT NULL DEFAULT 0,
  last_error TEXT
);
