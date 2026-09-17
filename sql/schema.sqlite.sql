-- DBOps V1.0 Metadata Schema (SQLite)
-- 默认文件: /data/dbops/dbops.db
-- 时间字段统一保存 UTC RFC3339 TEXT；JSON 保存为 TEXT。

PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  display_name TEXT,
  email TEXT,
  status TEXT NOT NULL DEFAULT 'active',
  last_login_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS roles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, description TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS permissions (id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT NOT NULL UNIQUE, name TEXT NOT NULL, resource_type TEXT, action TEXT);
CREATE TABLE IF NOT EXISTS user_roles (user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE, PRIMARY KEY(user_id,role_id));
CREATE TABLE IF NOT EXISTS role_permissions (role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE, permission_id INTEGER NOT NULL REFERENCES permissions(id) ON DELETE CASCADE, PRIMARY KEY(role_id,permission_id));

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
CREATE INDEX IF NOT EXISTS idx_hosts_project_env ON hosts(project_id,environment_id);

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
CREATE TABLE IF NOT EXISTS agent_status (
  agent_id INTEGER PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
  cpu_usage REAL, memory_usage REAL, load_1 REAL,
  running_tasks INTEGER NOT NULL DEFAULT 0,
  disk_summary_json TEXT NOT NULL DEFAULT '[]',
  last_report_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS credentials (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  credential_type TEXT NOT NULL,
  username TEXT,
  encrypted_secret TEXT NOT NULL,
  encryption_version INTEGER NOT NULL DEFAULT 1,
  metadata_json TEXT NOT NULL DEFAULT '{}',
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
  data_dir TEXT, config_path TEXT, credential_id INTEGER REFERENCES credentials(id),
  status TEXT NOT NULL DEFAULT 'unknown', managed_mode TEXT NOT NULL DEFAULT 'imported',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(host_id,port)
);
CREATE INDEX IF NOT EXISTS idx_db_instances_type_status ON database_instances(db_type,status);
CREATE INDEX IF NOT EXISTS idx_db_instances_cluster ON database_instances(cluster_id);

CREATE TABLE IF NOT EXISTS software_packages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  software_name TEXT NOT NULL, version TEXT NOT NULL, os_family TEXT, architecture TEXT,
  package_type TEXT, file_name TEXT, storage_path TEXT, download_url TEXT, sha256 TEXT,
  compatibility_json TEXT NOT NULL DEFAULT '{}', status TEXT NOT NULL DEFAULT 'available',
  created_at TEXT NOT NULL,
  UNIQUE(software_name,version,os_family,architecture)
);
CREATE TABLE IF NOT EXISTS mysql_config_templates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE, mysql_major_version TEXT, description TEXT,
  config_template TEXT NOT NULL, variables_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS mysql_server_ids (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id INTEGER NOT NULL UNIQUE,
  instance_id INTEGER UNIQUE REFERENCES database_instances(id),
  status TEXT NOT NULL DEFAULT 'allocated', created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS mysql_replications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  cluster_id INTEGER REFERENCES database_clusters(id),
  primary_instance_id INTEGER NOT NULL REFERENCES database_instances(id),
  replica_instance_id INTEGER NOT NULL UNIQUE REFERENCES database_instances(id),
  replication_credential_id INTEGER REFERENCES credentials(id),
  gtid_enabled INTEGER NOT NULL DEFAULT 1,
  io_thread_status TEXT, sql_thread_status TEXT, replication_lag_seconds INTEGER,
  source_uuid TEXT, last_io_error TEXT, last_sql_error TEXT, last_checked_at TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_no TEXT NOT NULL UNIQUE,
  task_type TEXT NOT NULL, target_type TEXT, target_id INTEGER,
  status TEXT NOT NULL DEFAULT 'pending',
  progress INTEGER NOT NULL DEFAULT 0 CHECK(progress BETWEEN 0 AND 100),
  parameters_json TEXT NOT NULL DEFAULT '{}', result_json TEXT NOT NULL DEFAULT '{}',
  agent_id INTEGER REFERENCES agents(id), created_by INTEGER REFERENCES users(id),
  created_at TEXT NOT NULL, queued_at TEXT, started_at TEXT, finished_at TEXT,
  timeout_seconds INTEGER NOT NULL DEFAULT 3600,
  error_code TEXT, error_message TEXT,
  idempotency_key TEXT, lease_owner TEXT, lease_expires_at TEXT,
  recovery_policy TEXT NOT NULL DEFAULT 'verify_before_retry'
);
CREATE INDEX IF NOT EXISTS idx_tasks_status_created ON tasks(status,created_at);
CREATE INDEX IF NOT EXISTS idx_tasks_target ON tasks(target_type,target_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_active_idempotency ON tasks(idempotency_key) WHERE idempotency_key IS NOT NULL AND status IN ('pending','queued','running','paused','interrupted');

CREATE TABLE IF NOT EXISTS task_steps (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  step_no INTEGER NOT NULL, step_code TEXT NOT NULL, step_name TEXT,
  status TEXT NOT NULL DEFAULT 'pending', progress INTEGER NOT NULL DEFAULT 0,
  started_at TEXT, finished_at TEXT, output_json TEXT, error_message TEXT,
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

CREATE TABLE IF NOT EXISTS storage_endpoints (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE, storage_type TEXT NOT NULL,
  endpoint TEXT, bucket TEXT, base_path TEXT,
  credential_id INTEGER REFERENCES credentials(id),
  options_json TEXT NOT NULL DEFAULT '{}', status TEXT NOT NULL DEFAULT 'enabled', created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS backup_policies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL, database_instance_id INTEGER NOT NULL REFERENCES database_instances(id),
  backup_engine TEXT NOT NULL, backup_type TEXT NOT NULL,
  cron_expr TEXT NOT NULL, next_run_at TEXT, misfire_policy TEXT NOT NULL DEFAULT 'run_once',
  retention_days INTEGER NOT NULL DEFAULT 30,
  storage_endpoint_id INTEGER REFERENCES storage_endpoints(id),
  options_json TEXT NOT NULL DEFAULT '{}', enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS backup_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER UNIQUE REFERENCES tasks(id), policy_id INTEGER REFERENCES backup_policies(id),
  database_instance_id INTEGER NOT NULL REFERENCES database_instances(id),
  backup_engine TEXT NOT NULL, backup_type TEXT NOT NULL,
  parent_backup_job_id INTEGER REFERENCES backup_jobs(id),
  started_at TEXT, finished_at TEXT, size_bytes INTEGER,
  status TEXT NOT NULL DEFAULT 'pending', storage_path TEXT, checksum TEXT,
  metadata_json TEXT NOT NULL DEFAULT '{}', error_message TEXT
);
CREATE TABLE IF NOT EXISTS restore_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER UNIQUE REFERENCES tasks(id), backup_job_id INTEGER NOT NULL REFERENCES backup_jobs(id),
  target_instance_id INTEGER REFERENCES database_instances(id), restore_mode TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending', started_at TEXT, finished_at TEXT,
  verification_status TEXT, options_json TEXT NOT NULL DEFAULT '{}', error_message TEXT
);

CREATE TABLE IF NOT EXISTS archive_policies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL, source_instance_id INTEGER NOT NULL REFERENCES database_instances(id),
  source_database TEXT NOT NULL, source_table TEXT NOT NULL,
  archive_column TEXT, where_template TEXT NOT NULL, retention_days INTEGER,
  destination_type TEXT NOT NULL,
  destination_instance_id INTEGER REFERENCES database_instances(id),
  destination_database TEXT, destination_table TEXT,
  storage_endpoint_id INTEGER REFERENCES storage_endpoints(id),
  batch_size INTEGER NOT NULL DEFAULT 5000, txn_size INTEGER,
  sleep_ms INTEGER NOT NULL DEFAULT 200,
  max_replication_lag INTEGER NOT NULL DEFAULT 30,
  max_threads_running INTEGER, max_cpu_pct REAL, max_io_util_pct REAL,
  delete_source INTEGER NOT NULL DEFAULT 1,
  cron_expr TEXT, next_run_at TEXT, misfire_policy TEXT NOT NULL DEFAULT 'skip',
  enabled INTEGER NOT NULL DEFAULT 1, options_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS archive_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER UNIQUE REFERENCES tasks(id), policy_id INTEGER NOT NULL REFERENCES archive_policies(id),
  status TEXT NOT NULL DEFAULT 'pending', started_at TEXT, finished_at TEXT,
  scanned_rows INTEGER NOT NULL DEFAULT 0, archived_rows INTEGER NOT NULL DEFAULT 0,
  deleted_rows INTEGER NOT NULL DEFAULT 0, failed_rows INTEGER NOT NULL DEFAULT 0,
  speed_rows_sec INTEGER, last_processed_key TEXT, pause_reason TEXT,
  verification_status TEXT, error_message TEXT
);

CREATE TABLE IF NOT EXISTS alert_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL, db_type TEXT, resource_type TEXT NOT NULL,
  metric_name TEXT NOT NULL, operator TEXT NOT NULL, threshold REAL NOT NULL,
  duration_seconds INTEGER NOT NULL DEFAULT 0, severity TEXT NOT NULL,
  labels_json TEXT NOT NULL DEFAULT '{}', repeat_interval_seconds INTEGER NOT NULL DEFAULT 3600,
  enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS alert_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  rule_id INTEGER REFERENCES alert_rules(id),
  resource_type TEXT NOT NULL, resource_id INTEGER NOT NULL,
  fingerprint TEXT NOT NULL, status TEXT NOT NULL, severity TEXT NOT NULL,
  message TEXT NOT NULL, started_at TEXT NOT NULL, last_seen_at TEXT,
  acknowledged_at TEXT, resolved_at TEXT, metadata_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_alert_events_open ON alert_events(status,severity,started_at);
CREATE TABLE IF NOT EXISTS alert_rule_states (
  fingerprint TEXT PRIMARY KEY,
  first_true_at TEXT, last_true_at TEXT, last_notified_at TEXT,
  state_json TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS silences (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL, matchers_json TEXT NOT NULL DEFAULT '{}',
  starts_at TEXT NOT NULL, ends_at TEXT NOT NULL, created_by INTEGER REFERENCES users(id), created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS notification_channels (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE, channel_type TEXT NOT NULL,
  credential_id INTEGER REFERENCES credentials(id), config_json TEXT NOT NULL DEFAULT '{}', enabled INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER REFERENCES users(id), username TEXT, action TEXT NOT NULL,
  resource_type TEXT, resource_id INTEGER, request_params_json TEXT NOT NULL DEFAULT '{}',
  result TEXT, client_ip TEXT, task_id INTEGER REFERENCES tasks(id), created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_user_time ON audit_logs(user_id,created_at);

CREATE TABLE IF NOT EXISTS system_settings (
  setting_key TEXT PRIMARY KEY,
  setting_value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  updated_by INTEGER REFERENCES users(id)
);
