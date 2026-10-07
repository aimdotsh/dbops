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
WHERE excluded.collected_at >= metric_latest.collected_at
`, resourceType, resourceID, ts, string(raw)); err != nil {
		return err
	}

	for table, bucket := range map[string]time.Time{
		"metric_snapshots_5m": collectedAt.Truncate(5 * time.Minute),
		"metric_rollups_1h":   collectedAt.Truncate(time.Hour),
		"metric_rollups_1d":   time.Date(collectedAt.Year(), collectedAt.Month(), collectedAt.Day(), 0, 0, 0, 0, time.UTC),
	} {
		var previous string
		if err := tx.QueryRowContext(ctx, "SELECT payload_json FROM "+table+" WHERE resource_type=? AND resource_id=? AND bucket_ts=?", resourceType, resourceID, bucket.Format(time.RFC3339)).Scan(&previous); err != nil && err != sql.ErrNoRows {
			return err
		}
		aggregate, err := aggregatePayload(previous, payload)
		if err != nil {
			return err
		}
		query := "INSERT INTO " + table + "(resource_type,resource_id,bucket_ts,payload_json) VALUES(?,?,?,?) " +
			"ON CONFLICT(resource_type,resource_id,bucket_ts) DO UPDATE SET payload_json=excluded.payload_json"
		if _, err := tx.ExecContext(ctx, query, resourceType, resourceID, bucket.Format(time.RFC3339), aggregate); err != nil {
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

func (s *Store) ListLatest(ctx context.Context, resourceType string) ([]Snapshot, error) {
	rows, err := s.DB.QueryContext(ctx,
		"SELECT resource_id,collected_at,payload_json FROM metric_latest WHERE resource_type=? ORDER BY resource_id",
		resourceType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var id int64
		var ts, raw string
		if err := rows.Scan(&id, &ts, &raw); err != nil {
			return nil, err
		}
		snap, err := decodeSnapshot(resourceType, id, ts, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

// Numeric gauges retain mean/min/max/count instead of silently replacing history
// with the final heartbeat. Non-numeric values retain the latest observed value.
func aggregatePayload(previous string, payload map[string]any) (string, error) {
	out := map[string]any{}
	if previous != "" {
		if err := json.Unmarshal([]byte(previous), &out); err != nil {
			return "", err
		}
	}
	stats, _ := out["_aggregation"].(map[string]any)
	if stats == nil {
		stats = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	normalized := map[string]any{}
	if err = json.Unmarshal(raw, &normalized); err != nil {
		return "", err
	}
	for key, value := range normalized {
		if key == "_aggregation" {
			continue
		}
		n, numeric := value.(float64)
		if !numeric {
			out[key] = value
			continue
		}
		stat, _ := stats[key].(map[string]any)
		if stat == nil {
			stat = map[string]any{"sum": float64(0), "count": float64(0), "min": n, "max": n}
		}
		stat["sum"] = stat["sum"].(float64) + n
		stat["count"] = stat["count"].(float64) + 1
		if n < stat["min"].(float64) {
			stat["min"] = n
		}
		if n > stat["max"].(float64) {
			stat["max"] = n
		}
		stats[key] = stat
		out[key] = stat["sum"].(float64) / stat["count"].(float64)
	}
	out["_aggregation"] = stats
	b, err := json.Marshal(out)
	return string(b), err
}

func (s *Store) Retain(ctx context.Context, now time.Time, days5m, days1h, days1d int) error {
	for table, days := range map[string]int{"metric_snapshots_5m": days5m, "metric_rollups_1h": days1h, "metric_rollups_1d": days1d} {
		if days <= 0 {
			return errors.New("metric retention must be positive")
		}
		if _, err := s.DB.ExecContext(ctx, "DELETE FROM "+table+" WHERE bucket_ts < ?", now.UTC().AddDate(0, 0, -days).Format(time.RFC3339)); err != nil {
			return err
		}
	}
	_, err := s.DB.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)")
	return err
}
