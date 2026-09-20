package storage

import (
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed schema.sqlite.sql
var metadataSchema string

//go:embed metrics.sqlite.sql
var metricsSchema string

func Migrate(meta, metrics *sql.DB) error {
	if _, err := meta.Exec(`CREATE TABLE IF NOT EXISTS operation_audit (
 id INTEGER PRIMARY KEY AUTOINCREMENT, actor_id INTEGER NOT NULL, method TEXT NOT NULL,
 path TEXT NOT NULL, status_code INTEGER NOT NULL, created_at TEXT NOT NULL
 )`); err != nil {
		return err
	}

	if _, err := meta.Exec(metadataSchema); err != nil {
		return err
	}
	if _, err := meta.Exec(`CREATE TABLE IF NOT EXISTS backup_schedules(
 id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,task_type TEXT NOT NULL,
 parameters_json TEXT NOT NULL,interval_seconds INTEGER NOT NULL,enabled INTEGER NOT NULL,
 next_run TEXT NOT NULL,last_error TEXT NOT NULL DEFAULT '',last_task_id INTEGER REFERENCES tasks(id));
 CREATE UNIQUE INDEX IF NOT EXISTS idx_task_schedule_occurrence ON tasks(idempotency_key) WHERE substr(idempotency_key,1,9)='schedule:';
 `); err != nil {
		return err
	}
	if _, err := meta.Exec(`CREATE TABLE IF NOT EXISTS alert_outbox(id INTEGER PRIMARY KEY AUTOINCREMENT,alert_id INTEGER NOT NULL REFERENCES alert_events(id),state TEXT NOT NULL,channel TEXT NOT NULL,attempts INTEGER NOT NULL DEFAULT 0,next_attempt TEXT NOT NULL,delivered_at TEXT,last_error TEXT NOT NULL DEFAULT '',UNIQUE(alert_id,state,channel));
 CREATE TABLE IF NOT EXISTS alert_silences(fingerprint TEXT PRIMARY KEY,until_time TEXT NOT NULL);`); err != nil {
		return err
	}
	if _, err := meta.Exec(`CREATE TABLE IF NOT EXISTS user_resource_scopes(user_id INTEGER NOT NULL REFERENCES users(id),project_id INTEGER NOT NULL REFERENCES projects(id),environment_id INTEGER NOT NULL REFERENCES environments(id),PRIMARY KEY(user_id,project_id,environment_id))`); err != nil {
		return err
	}
	if err := ensureColumn(meta, "database_instances", "data_dir", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(meta, "database_instances", "config_path", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(meta, "database_instances", "credential_id", "INTEGER REFERENCES credentials(id)"); err != nil {
		return err
	}
	if err := ensureColumn(meta, "software_packages", "size_bytes", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := metrics.Exec(metricsSchema); err != nil {
		return err
	}
	return nil
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}
