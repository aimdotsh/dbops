package storage

import (
	"database/sql"
	_ "embed"
)

//go:embed schema.sqlite.sql
var metadataSchema string

//go:embed metrics.sqlite.sql
var metricsSchema string

func Migrate(meta, metrics *sql.DB) error {
	if _, err := meta.Exec(metadataSchema); err != nil {
		return err
	}
	if _, err := metrics.Exec(metricsSchema); err != nil {
		return err
	}
	return nil
}
