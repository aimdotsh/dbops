package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	metricstore "github.com/aimdotsh/dbops/internal/metrics"
)

type Rule struct {
	Name      string
	Metric    string
	Operator  string
	Threshold float64
	Duration  time.Duration
	Severity  string
}

type Event struct {
	ID           int64      `json:"id"`
	ResourceType string     `json:"resource_type"`
	ResourceID   int64      `json:"resource_id"`
	Fingerprint  string     `json:"fingerprint"`
	Status       string     `json:"status"`
	Severity     string     `json:"severity"`
	Message      string     `json:"message"`
	StartedAt    time.Time  `json:"started_at"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	MetadataJSON string     `json:"metadata_json,omitempty"`
}

type Engine struct {
	logger   *slog.Logger
	enabled  bool
	interval time.Duration
	db       *sql.DB
	metrics  *metricstore.Store
	rules    []Rule

	mu        sync.Mutex
	firstTrue map[string]time.Time
}

func New(logger *slog.Logger, enabled bool, seconds int, db *sql.DB, metrics *metricstore.Store) *Engine {
	if seconds <= 0 {
		seconds = 15
	}
	return &Engine{
		logger: logger, enabled: enabled, interval: time.Duration(seconds) * time.Second,
		db: db, metrics: metrics, firstTrue: map[string]time.Time{},
		rules: []Rule{
			{Name: "HostDisk85", Metric: "root_used_pct", Operator: ">=", Threshold: 85, Duration: 5 * time.Minute, Severity: "P2"},
			{Name: "HostDisk95", Metric: "root_used_pct", Operator: ">=", Threshold: 95, Duration: time.Minute, Severity: "P1"},
			{Name: "HostMemory90", Metric: "memory_used_pct", Operator: ">=", Threshold: 90, Duration: 5 * time.Minute, Severity: "P2"},
			{Name: "AgentHeartbeatTimeout", Metric: "heartbeat_age_seconds", Operator: ">", Threshold: 90, Duration: 0, Severity: "P1"},
		},
	}
}

func (e *Engine) Start(ctx context.Context) {
	if !e.enabled || e.db == nil || e.metrics == nil {
		return
	}
	go func() {
		if err := e.EvaluateOnce(ctx); err != nil {
			e.logger.Warn("initial alert evaluation failed", "error", err)
		}
		ticker := time.NewTicker(e.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := e.EvaluateOnce(ctx); err != nil {
					e.logger.Warn("alert evaluation failed", "error", err)
				}
			}
		}
	}()
}

func (e *Engine) EvaluateOnce(ctx context.Context) error {
	if e.metrics == nil || e.db == nil {
		return nil
	}
	snaps, err := e.metrics.ListLatest(ctx, "host")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	active := map[string]bool{}

	for _, snap := range snaps {
		for _, rule := range e.rules {
			value, ok := metricValue(rule.Metric, snap, now)
			if !ok {
				continue
			}
			fingerprint := fmt.Sprintf("%s:host:%d", rule.Name, snap.ResourceID)
			if compare(value, rule.Operator, rule.Threshold) {
				active[fingerprint] = true
				if !e.durationSatisfied(fingerprint, now, rule.Duration) {
					continue
				}
				message := fmt.Sprintf("%s: %.2f %s %.2f", rule.Name, value, rule.Operator, rule.Threshold)
				meta, _ := json.Marshal(map[string]any{
					"rule": rule.Name, "metric": rule.Metric, "value": value, "threshold": rule.Threshold,
					"operator": rule.Operator,
				})
				if err := e.upsertFiring(ctx, "host", snap.ResourceID, fingerprint, rule.Severity, message, string(meta), now); err != nil {
					return err
				}
			} else {
				e.clearDuration(fingerprint)
				if err := e.resolve(ctx, fingerprint, now); err != nil {
					return err
				}
			}
		}
	}

	// Resolve events whose resource disappeared from latest metrics only after a fresh
	// evaluation explicitly sees a false condition. Missing metrics remain open because
	// heartbeat timeout is itself represented by the snapshot age.
	_ = active
	return nil
}

func (e *Engine) durationSatisfied(key string, now time.Time, duration time.Duration) bool {
	if duration <= 0 {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	first, ok := e.firstTrue[key]
	if !ok {
		e.firstTrue[key] = now
		return false
	}
	return now.Sub(first) >= duration
}

func (e *Engine) clearDuration(key string) {
	e.mu.Lock()
	delete(e.firstTrue, key)
	e.mu.Unlock()
}

func (e *Engine) upsertFiring(ctx context.Context, resourceType string, resourceID int64, fingerprint, severity, message, metadata string, now time.Time) error {
	var id int64
	err := e.db.QueryRowContext(ctx,
		"SELECT id FROM alert_events WHERE fingerprint=? AND status IN ('FIRING','ACKNOWLEDGED') ORDER BY id DESC LIMIT 1",
		fingerprint).Scan(&id)
	if err == nil {
		_, err = e.db.ExecContext(ctx,
			"UPDATE alert_events SET last_seen_at=?,severity=?,message=?,metadata_json=? WHERE id=?",
			now.Format(time.RFC3339), severity, message, metadata, id)
		return err
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = e.db.ExecContext(ctx, `
INSERT INTO alert_events(resource_type,resource_id,fingerprint,status,severity,message,started_at,last_seen_at,metadata_json)
VALUES(?,?,?,'FIRING',?,?,?,?,?)
`, resourceType, resourceID, fingerprint, severity, message, now.Format(time.RFC3339), now.Format(time.RFC3339), metadata)
	return err
}

func (e *Engine) resolve(ctx context.Context, fingerprint string, now time.Time) error {
	_, err := e.db.ExecContext(ctx, `
UPDATE alert_events
SET status='RESOLVED',resolved_at=?,last_seen_at=?
WHERE fingerprint=? AND status IN ('FIRING','ACKNOWLEDGED')
`, now.Format(time.RFC3339), now.Format(time.RFC3339), fingerprint)
	return err
}

func (e *Engine) List(ctx context.Context, status string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	query := `
SELECT id,resource_type,resource_id,fingerprint,status,severity,message,started_at,last_seen_at,resolved_at,metadata_json
FROM alert_events`
	args := []any{}
	if status != "" {
		query += " WHERE status=?"
		args = append(args, strings.ToUpper(status))
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var ev Event
		var started string
		var lastSeen, resolved sql.NullString
		if err := rows.Scan(&ev.ID, &ev.ResourceType, &ev.ResourceID, &ev.Fingerprint, &ev.Status, &ev.Severity, &ev.Message, &started, &lastSeen, &resolved, &ev.MetadataJSON); err != nil {
			return nil, err
		}
		ev.StartedAt, _ = time.Parse(time.RFC3339, started)
		if lastSeen.Valid {
			v, _ := time.Parse(time.RFC3339, lastSeen.String)
			ev.LastSeenAt = &v
		}
		if resolved.Valid {
			v, _ := time.Parse(time.RFC3339, resolved.String)
			ev.ResolvedAt = &v
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (e *Engine) Acknowledge(ctx context.Context, id int64) error {
	res, err := e.db.ExecContext(ctx,
		"UPDATE alert_events SET status='ACKNOWLEDGED' WHERE id=? AND status='FIRING'", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("alert %d is not firing", id)
	}
	return nil
}

func metricValue(name string, snap metricstore.Snapshot, now time.Time) (float64, bool) {
	if name == "heartbeat_age_seconds" {
		return math.Max(0, now.Sub(snap.CollectedAt).Seconds()), true
	}
	v, ok := snap.Payload[name]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

func compare(value float64, op string, threshold float64) bool {
	switch op {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return false
	}
}
