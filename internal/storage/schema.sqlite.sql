CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS projects (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, description TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS environments (id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT NOT NULL UNIQUE, name TEXT NOT NULL, risk_level TEXT NOT NULL DEFAULT 'normal');
CREATE TABLE IF NOT EXISTS hosts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  hostname TEXT NOT NULL,
  ip_address TEXT NOT NULL UNIQUE,
  project_id INTEGER REFERENCES projects(id),
  environment_id INTEGER REFERENCES environments(id),
  os_name TEXT, os_version TEXT, kernel_version TEXT, architecture TEXT,
  cpu_cores INTEGER, memory_bytes INTEGER,
  status TEXT NOT NULL DEFAULT 'unknown', description TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS agents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_uuid TEXT NOT NULL UNIQUE,
  host_id INTEGER UNIQUE REFERENCES hosts(id) ON DELETE SET NULL,
  version TEXT, architecture TEXT,
  status TEXT NOT NULL DEFAULT 'offline',
  registered_at TEXT, last_heartbeat_at TEXT,
  token_hash TEXT,
  capabilities_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS database_clusters (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL, db_type TEXT NOT NULL, cluster_type TEXT NOT NULL,
  project_id INTEGER REFERENCES projects(id), environment_id INTEGER REFERENCES environments(id),
  status TEXT NOT NULL DEFAULT 'unknown', metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS database_instances (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL, db_type TEXT NOT NULL, version TEXT,
  host_id INTEGER REFERENCES hosts(id), cluster_id INTEGER REFERENCES database_clusters(id),
  port INTEGER, role TEXT,
  project_id INTEGER REFERENCES projects(id), environment_id INTEGER REFERENCES environments(id),
  status TEXT NOT NULL DEFAULT 'unknown', managed_mode TEXT NOT NULL DEFAULT 'imported',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(host_id,port)
);
CREATE TABLE IF NOT EXISTS tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_no TEXT NOT NULL UNIQUE,
  task_type TEXT NOT NULL, target_type TEXT, target_id INTEGER,
  status TEXT NOT NULL DEFAULT 'pending',
  progress INTEGER NOT NULL DEFAULT 0 CHECK(progress BETWEEN 0 AND 100),
  parameters_json TEXT NOT NULL DEFAULT '{}',
  result_json TEXT NOT NULL DEFAULT '{}',
  agent_id INTEGER REFERENCES agents(id),
  created_at TEXT NOT NULL, queued_at TEXT, started_at TEXT, finished_at TEXT,
  timeout_seconds INTEGER NOT NULL DEFAULT 3600,
  error_code TEXT, error_message TEXT,
  idempotency_key TEXT, lease_owner TEXT, lease_expires_at TEXT,
  recovery_policy TEXT NOT NULL DEFAULT 'verify_before_retry'
);
CREATE INDEX IF NOT EXISTS idx_tasks_status_created ON tasks(status,created_at);
CREATE TABLE IF NOT EXISTS resource_locks (
  lock_key TEXT PRIMARY KEY,
  owner_task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  lease_expires_at TEXT NOT NULL,
  acquired_at TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}'
);
