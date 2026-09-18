CREATE TABLE IF NOT EXISTS metrics_schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
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
CREATE TABLE IF NOT EXISTS metric_rollups_1h (
  resource_type TEXT NOT NULL,
  resource_id INTEGER NOT NULL,
  bucket_ts TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  PRIMARY KEY(resource_type,resource_id,bucket_ts)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS metric_rollups_1d (
  resource_type TEXT NOT NULL,
  resource_id INTEGER NOT NULL,
  bucket_ts TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  PRIMARY KEY(resource_type,resource_id,bucket_ts)
) WITHOUT ROWID;
