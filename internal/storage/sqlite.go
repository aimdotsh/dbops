package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Stores struct {
	Metadata *sql.DB
	Metrics  *sql.DB
}

func Open(metadataPath, metricsPath string, metadataConns, metricsConns int) (*Stores, error) {
	for _, p := range []string{metadataPath, metricsPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
	}
	meta, err := openDB(metadataPath, metadataConns)
	if err != nil {
		return nil, err
	}
	metrics, err := openDB(metricsPath, metricsConns)
	if err != nil {
		meta.Close()
		return nil, err
	}
	return &Stores{Metadata: meta, Metrics: metrics}, nil
}

func openDB(path string, maxOpen int) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if maxOpen <= 0 {
		maxOpen = 4
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxOpen)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Stores) Close() error {
	var first error
	if s.Metadata != nil {
		first = s.Metadata.Close()
	}
	if s.Metrics != nil {
		if err := s.Metrics.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
