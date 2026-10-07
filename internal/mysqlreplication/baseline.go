package mysqlreplication

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/security"
)

// seedReplica transfers a GTID-bearing logical snapshot through authenticated
// Agent connections. A failed import is never retried automatically: the
// partial target and private snapshot remain available for DBA inspection.
func (s *Service) seedReplica(ctx context.Context, taskID int64, primary, replica runtime) (map[string]any, error) {
	random, err := security.RandomPassword(32)
	if err != nil {
		return nil, err
	}
	id := fmt.Sprintf("%x", []byte(random))
	path := ""
	transferred := false
	succeeded := false
	defer func() {
		if !succeeded {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = s.dispatchCreate(cleanupCtx, taskID, primary, map[string]any{"mode": "baseline_cleanup", "baseline_id": id})
		if transferred {
			for _, agent := range []int64{primary.Agent.ID, replica.Agent.ID} {
				_, _ = s.transferCall(cleanupCtx, agent, id, "cleanup", map[string]any{})
			}
		}
	}()

	backup, err := s.dispatchCreate(ctx, taskID, primary, map[string]any{"mode": "baseline_export", "baseline_id": id})
	if err != nil {
		return nil, fmt.Errorf("GTID baseline export failed: %w", err)
	}
	path = stringValue(backup["path"])
	checksum := stringValue(backup["sha256"])
	if !filepath.IsAbs(path) || len(checksum) != 64 {
		return nil, errors.New("invalid baseline export result")
	}
	if primary.Agent.ID != replica.Agent.ID {
		transferred = true
		manifest, err := s.transferCall(ctx, primary.Agent.ID, id, "export", map[string]any{"backup_path": path, "engine": "mysqldump", "sha256": checksum})
		if err != nil {
			return nil, fmt.Errorf("baseline transfer export failed: %w", err)
		}
		size := int64Value(manifest["size_bytes"])
		if size <= 0 {
			return nil, errors.New("empty baseline transfer")
		}
		for offset := int64(0); offset < size; {
			chunk, err := s.transferCall(ctx, primary.Agent.ID, id, "read", map[string]any{"offset": offset})
			if err != nil {
				return nil, err
			}
			n := int64Value(chunk["bytes"])
			if n <= 0 || n > 256<<10 || offset+n > size {
				return nil, errors.New("invalid baseline chunk")
			}
			ack, err := s.transferCall(ctx, replica.Agent.ID, id, "write", map[string]any{"offset": offset, "chunk": chunk["chunk"]})
			if err != nil {
				return nil, err
			}
			if int64Value(ack["bytes"]) != n {
				return nil, errors.New("short baseline write")
			}
			offset += n
		}
		finished, err := s.transferCall(ctx, replica.Agent.ID, id, "finish", map[string]any{"sha256": manifest["sha256"]})
		if err != nil {
			return nil, err
		}
		root := stringValue(finished["path"])
		if !filepath.IsAbs(root) {
			return nil, errors.New("invalid transferred baseline path")
		}
		path = filepath.Join(root, "backup.sql.gz")
	}
	result, err := s.dispatchCreate(ctx, taskID, replica, map[string]any{"mode": "baseline_import", "backup_path": path, "sha256": checksum})
	if err != nil {
		return nil, fmt.Errorf("baseline import failed; inspect replica before retry: %w", err)
	}
	if !boolValue(result["restored"]) || int64Value(result["user_databases"]) == 0 {
		return nil, errors.New("baseline import did not verify user databases")
	}
	succeeded = true
	return map[string]any{"seeded": true, "user_databases": result["user_databases"], "sha256": checksum}, nil
}

func (s *Service) transferCall(ctx context.Context, agentID int64, id, op string, params map[string]any) (map[string]any, error) {
	params["transfer_id"] = id
	resp, err := s.dispatcher.Dispatch(ctx, agentID, agentproto.ActionRequest{
		TaskID: 0, Action: "backup.transfer." + op, Risk: "R1",
		ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 86400, Params: params,
	})
	if err != nil {
		return nil, err
	}
	if op == "cleanup" {
		return nil, nil
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		return nil, errors.New("invalid baseline transfer response")
	}
	return result, nil
}
