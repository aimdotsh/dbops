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
	if _, err := meta.Exec(metadataSchema); err != nil {
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
