package metrics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type Snapshot struct {
	ResourceType string         `json:"resource_type"`
	ResourceID   int64          `json:"resource_id"`
	CollectedAt  time.Time      `json:"collected_at"`
	Payload      map[string]any `json:"payload"`
}

type Store struct{ DB *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{DB: db} }

func (s *Store) Put(ctx context.Context, resourceType string, resourceID int64, collectedAt time.Time, payload map[string]any) error {
	if resourceType == "" || resourceID <= 0 {
		return errors.New("invalid metric resource")
	}
	if collectedAt.IsZero() {
		collectedAt = time.Now().UTC()
	} else {
		collectedAt = collectedAt.UTC()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ts := collectedAt.Format(time.RFC3339)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
INSERT INTO metric_latest(resource_type,resource_id,collected_at,payload_json)
VALUES(?,?,?,?)
ON CONFLICT(resource_type,resource_id) DO UPDATE SET
  collected_at=excluded.collected_at,
  payload_json=excluded.payload_json
`, resourceType, resourceID, ts, string(raw)); err != nil {
		return err
	}

	for table, bucket := range map[string]time.Time{
		"metric_snapshots_5m": collectedAt.Truncate(5 * time.Minute),
		"metric_rollups_1h":   collectedAt.Truncate(time.Hour),
		"metric_rollups_1d":   time.Date(collectedAt.Year(), collectedAt.Month(), collectedAt.Day(), 0, 0, 0, 0, time.UTC),
	} {
		query := "INSERT INTO " + table + "(resource_type,resource_id,bucket_ts,payload_json) VALUES(?,?,?,?) " +
			"ON CONFLICT(resource_type,resource_id,bucket_ts) DO UPDATE SET payload_json=excluded.payload_json"
		if _, err := tx.ExecContext(ctx, query, resourceType, resourceID, bucket.Format(time.RFC3339), string(raw)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Latest(ctx context.Context, resourceType string, resourceID int64) (Snapshot, error) {
	var ts, raw string
	err := s.DB.QueryRowContext(ctx,
		"SELECT collected_at,payload_json FROM metric_latest WHERE resource_type=? AND resource_id=?",
		resourceType, resourceID).Scan(&ts, &raw)
	if err != nil {
		return Snapshot{}, err
	}
	return decodeSnapshot(resourceType, resourceID, ts, raw)
}

func (s *Store) Range(ctx context.Context, granularity, resourceType string, resourceID int64, from, to time.Time, limit int) ([]Snapshot, error) {
	table := map[string]string{"5m": "metric_snapshots_5m", "1h": "metric_rollups_1h", "1d": "metric_rollups_1d"}[granularity]
	if table == "" {
		return nil, errors.New("granularity must be 5m, 1h or 1d")
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.DB.QueryContext(ctx,
		"SELECT bucket_ts,payload_json FROM "+table+" WHERE resource_type=? AND resource_id=? AND bucket_ts>=? AND bucket_ts<=? ORDER BY bucket_ts LIMIT ?",
		resourceType, resourceID, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var ts, raw string
		if err := rows.Scan(&ts, &raw); err != nil {
			return nil, err
		}
		snap, err := decodeSnapshot(resourceType, resourceID, ts, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

func decodeSnapshot(resourceType string, resourceID int64, ts, raw string) (Snapshot, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return Snapshot{}, err
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ResourceType: resourceType, ResourceID: resourceID, CollectedAt: t, Payload: payload}, nil
}
