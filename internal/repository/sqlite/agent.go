package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

type AgentRepo struct{ DB *sql.DB }

func (r AgentRepo) Upsert(ctx context.Context, a domain.Agent) (domain.Agent, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	caps, _ := json.Marshal(a.Capabilities)
	_, err := r.DB.ExecContext(ctx, `
INSERT INTO agents(agent_uuid,host_id,version,architecture,status,registered_at,last_heartbeat_at,capabilities_json,created_at,updated_at)
VALUES(?,?,?,?, 'online', ?, ?, ?, ?, ?)
ON CONFLICT(agent_uuid) DO UPDATE SET
  host_id=COALESCE(excluded.host_id,agents.host_id),
  version=excluded.version,
  architecture=excluded.architecture,
  status='online',
  last_heartbeat_at=excluded.last_heartbeat_at,
  capabilities_json=excluded.capabilities_json,
  updated_at=excluded.updated_at`,
		a.AgentUUID, a.HostID, a.Version, a.Architecture, now, now, string(caps), now, now)
	if err != nil {
		return a, err
	}
	return r.GetByUUID(ctx, a.AgentUUID)
}

func (r AgentRepo) Heartbeat(ctx context.Context, uuid string, capabilities []string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	caps, _ := json.Marshal(capabilities)
	_, err := r.DB.ExecContext(ctx,
		"UPDATE agents SET status='online',last_heartbeat_at=?,capabilities_json=?,updated_at=? WHERE agent_uuid=?",
		now, string(caps), now, uuid)
	return err
}

func (r AgentRepo) MarkOffline(ctx context.Context, uuid string) error {
	_, err := r.DB.ExecContext(ctx,
		"UPDATE agents SET status='offline',updated_at=? WHERE agent_uuid=?",
		time.Now().UTC().Format(time.RFC3339), uuid)
	return err
}

func (r AgentRepo) Get(ctx context.Context, id int64) (domain.Agent, error) {
	return scanAgent(r.DB.QueryRowContext(ctx,
		"SELECT id,agent_uuid,host_id,COALESCE(version,''),COALESCE(architecture,''),status,last_heartbeat_at,capabilities_json FROM agents WHERE id=?", id))
}

func (r AgentRepo) GetByUUID(ctx context.Context, uuid string) (domain.Agent, error) {
	return scanAgent(r.DB.QueryRowContext(ctx,
		"SELECT id,agent_uuid,host_id,COALESCE(version,''),COALESCE(architecture,''),status,last_heartbeat_at,capabilities_json FROM agents WHERE agent_uuid=?", uuid))
}

func (r AgentRepo) List(ctx context.Context) ([]domain.Agent, error) {
	rows, err := r.DB.QueryContext(ctx,
		"SELECT id,agent_uuid,host_id,COALESCE(version,''),COALESCE(architecture,''),status,last_heartbeat_at,capabilities_json FROM agents ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r AgentRepo) BindHostByIdentity(ctx context.Context, uuid, hostname, ip string) (*int64, error) {
	if hostname == "" || ip == "" {
		return nil, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.DB.ExecContext(ctx, `
INSERT INTO hosts(hostname,ip_address,status,created_at,updated_at)
VALUES(?,?,'online',?,?)
ON CONFLICT(ip_address) DO UPDATE SET
  hostname=excluded.hostname,
  status='online',
  updated_at=excluded.updated_at`, hostname, ip, now, now)
	if err != nil {
		return nil, err
	}
	var id int64
	if err := r.DB.QueryRowContext(ctx, "SELECT id FROM hosts WHERE ip_address=?", ip).Scan(&id); err != nil {
		return nil, err
	}
	if _, err := r.DB.ExecContext(ctx, "UPDATE agents SET host_id=? WHERE agent_uuid=?", id, uuid); err != nil {
		return nil, err
	}
	return &id, nil
}

func (r AgentRepo) GetCredentialHash(ctx context.Context, uuid string) (string, error) {
	var hash sql.NullString
	if err := r.DB.QueryRowContext(ctx, "SELECT token_hash FROM agents WHERE agent_uuid=?", uuid).Scan(&hash); err != nil {
		return "", err
	}
	if !hash.Valid {
		return "", nil
	}
	return hash.String, nil
}

func (r AgentRepo) SetCredentialHash(ctx context.Context, uuid, hash string) error {
	_, err := r.DB.ExecContext(ctx, "UPDATE agents SET token_hash=?,updated_at=? WHERE agent_uuid=?",
		hash, time.Now().UTC().Format(time.RFC3339), uuid)
	return err
}

type agentScanner interface{ Scan(...any) error }

func scanAgent(s agentScanner) (domain.Agent, error) {
	var a domain.Agent
	var heartbeat sql.NullString
	var caps string
	if err := s.Scan(&a.ID, &a.AgentUUID, &a.HostID, &a.Version, &a.Architecture, &a.Status, &heartbeat, &caps); err != nil {
		return a, err
	}
	if heartbeat.Valid {
		v, _ := time.Parse(time.RFC3339, heartbeat.String)
		a.LastHeartbeatAt = &v
	}
	_ = json.Unmarshal([]byte(caps), &a.Capabilities)
	return a, nil
}
