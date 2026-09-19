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
  capabilities_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agents_status_heartbeat ON agents(status,last_heartbeat_at);

CREATE TABLE IF NOT EXISTS credentials (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  credential_type TEXT NOT NULL,
  username TEXT,
  encrypted_secret TEXT NOT NULL,
  encryption_version INTEGER NOT NULL DEFAULT 1,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
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
  data_dir TEXT, config_path TEXT, credential_id INTEGER REFERENCES credentials(id),
  status TEXT NOT NULL DEFAULT 'unknown', managed_mode TEXT NOT NULL DEFAULT 'imported',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(host_id,port)
);
CREATE INDEX IF NOT EXISTS idx_db_instances_type_status ON database_instances(db_type,status);

CREATE TABLE IF NOT EXISTS software_packages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  software_name TEXT NOT NULL,
  version TEXT NOT NULL,
  os_family TEXT NOT NULL,
  architecture TEXT NOT NULL,
  package_type TEXT NOT NULL,
  file_name TEXT NOT NULL,
  storage_path TEXT NOT NULL,
  download_url TEXT,
  sha256 TEXT NOT NULL,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  compatibility_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'available',
  created_at TEXT NOT NULL,
  UNIQUE(software_name,version,os_family,architecture)
);
CREATE INDEX IF NOT EXISTS idx_software_packages_lookup ON software_packages(software_name,status,os_family,architecture);

CREATE TABLE IF NOT EXISTS mysql_config_templates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  mysql_major_version TEXT,
  description TEXT,
  config_template TEXT NOT NULL,
  variables_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mysql_server_ids (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id INTEGER NOT NULL UNIQUE,
  host_id INTEGER NOT NULL REFERENCES hosts(id),
  port INTEGER NOT NULL,
  task_id INTEGER REFERENCES tasks(id),
  instance_id INTEGER UNIQUE REFERENCES database_instances(id),
  status TEXT NOT NULL DEFAULT 'reserved',
  created_at TEXT NOT NULL,
  UNIQUE(host_id,port)
);

CREATE TABLE IF NOT EXISTS mysql_replications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  primary_instance_id INTEGER NOT NULL REFERENCES database_instances(id),
  replica_instance_id INTEGER NOT NULL UNIQUE REFERENCES database_instances(id),
  replication_credential_id INTEGER REFERENCES credentials(id),
  gtid_enabled INTEGER NOT NULL DEFAULT 1,
  io_thread_status TEXT,
  sql_thread_status TEXT,
  replication_lag_seconds INTEGER,
  source_uuid TEXT,
  last_io_error TEXT,
  last_sql_error TEXT,
  last_checked_at TEXT,
  status TEXT NOT NULL DEFAULT 'unknown',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_mysql_replication_primary ON mysql_replications(primary_instance_id);

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
CREATE INDEX IF NOT EXISTS idx_tasks_agent_status ON tasks(agent_id,status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_active_idempotency ON tasks(idempotency_key)
WHERE idempotency_key IS NOT NULL AND status IN ('pending','queued','running','paused','interrupted');

CREATE TABLE IF NOT EXISTS task_steps (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  step_no INTEGER NOT NULL,
  step_code TEXT NOT NULL,
  step_name TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  progress INTEGER NOT NULL DEFAULT 0,
  started_at TEXT,
  finished_at TEXT,
  output_json TEXT,
  error_message TEXT,
  recovery_policy TEXT,
  UNIQUE(task_id,step_no)
);

CREATE TABLE IF NOT EXISTS task_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  event_time TEXT NOT NULL,
  event_type TEXT NOT NULL,
  step_code TEXT,
  level TEXT NOT NULL DEFAULT 'INFO',
  message TEXT,
  payload_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_task_events_task_time ON task_events(task_id,event_time);

CREATE TABLE IF NOT EXISTS resource_locks (
  lock_key TEXT PRIMARY KEY,
  owner_task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  lease_expires_at TEXT NOT NULL,
  acquired_at TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}'
);


CREATE TABLE IF NOT EXISTS backup_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL UNIQUE REFERENCES tasks(id) ON DELETE CASCADE,
  database_instance_id INTEGER NOT NULL REFERENCES database_instances(id),
  backup_engine TEXT NOT NULL,
  backup_type TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending',
  storage_path TEXT,
  checksum TEXT,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  error_message TEXT
);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_instance_status ON backup_jobs(database_instance_id,status);


CREATE TABLE IF NOT EXISTS archive_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL UNIQUE REFERENCES tasks(id) ON DELETE CASCADE,
  source_instance_id INTEGER NOT NULL REFERENCES database_instances(id),
  source_database TEXT NOT NULL,
  source_table TEXT NOT NULL,
  destination_database TEXT,
  destination_table TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  started_at TEXT,
  finished_at TEXT,
  scanned_rows INTEGER NOT NULL DEFAULT 0,
  archived_rows INTEGER NOT NULL DEFAULT 0,
  deleted_rows INTEGER NOT NULL DEFAULT 0,
  failed_rows INTEGER NOT NULL DEFAULT 0,
  verification_status TEXT,
  error_message TEXT
);
CREATE INDEX IF NOT EXISTS idx_archive_jobs_source_status ON archive_jobs(source_instance_id,status);
