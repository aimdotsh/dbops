package mysqlarchive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/domain"
)

// archiveLagProbe checks the live replica rather than trusting a previously
// persisted lag value. A configured limit without a monitorable replica fails
// closed, so operators can explicitly choose zero for standalone sources.
func (s *Service) archiveLagProbe(ctx context.Context, p domain.ArchivePolicy) (func(context.Context) error, error) {
	if p.MaxReplicationLag == 0 {
		return nil, nil
	}
	if s.replications == nil {
		return nil, errors.New("replication monitor is unavailable")
	}
	items, err := s.replications.List(ctx)
	if err != nil {
		return nil, err
	}
	var replicaIDs []int64
	for _, item := range items {
		if item.PrimaryInstanceID == p.SourceInstanceID {
			replicaIDs = append(replicaIDs, item.ReplicaInstanceID)
		}
	}
	if len(replicaIDs) == 0 {
		replicaIDs = append(replicaIDs, p.SourceInstanceID) // source may itself be a replica
	}
	runtimes := make([]runtime, 0, len(replicaIDs))
	for _, replicaID := range replicaIDs {
		rt, err := s.loadRuntime(ctx, replicaID)
		if err != nil {
			return nil, fmt.Errorf("load replication monitor %d: %w", replicaID, err)
		}
		runtimes = append(runtimes, rt)
	}
	return func(checkCtx context.Context) error {
		for _, rt := range runtimes {
			resp, err := s.dispatcher.Dispatch(checkCtx, rt.Agent.ID, agentproto.ActionRequest{
				TaskID: 0, Action: "mysql.replication.status", Risk: "R0",
				ProtocolVersion: agentproto.ProtocolVersion, TimeoutSeconds: 15,
				Params: map[string]any{"base_dir": rt.BaseDir, "run_dir": rt.RunDir, "root_password": rt.Password},
			})
			if err != nil {
				return fmt.Errorf("replication lag check failed: %w", err)
			}
			result, ok := resp.Result.(map[string]any)
			if !ok || result["configured"] != true || result["status"] != "healthy" {
				return errors.New("replica is not configured or healthy")
			}
			lag, ok := result["replication_lag_seconds"]
			if !ok || lag == nil {
				return errors.New("replication lag is unknown")
			}
			seconds, valid := lagSeconds(lag)
			if !valid {
				return errors.New("replication lag value is invalid")
			}
			if seconds > int64(p.MaxReplicationLag) {
				return fmt.Errorf("replication lag %d seconds exceeds limit %d", seconds, p.MaxReplicationLag)
			}
		}
		return nil
	}, nil
}

func lagSeconds(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, v >= 0
	case int:
		return int64(v), v >= 0
	case float64:
		n := int64(v)
		return n, v >= 0 && float64(n) == v
	case json.Number:
		n, err := v.Int64()
		return n, err == nil && n >= 0
	default:
		return 0, false
	}
}

func watchArchiveLag(ctx context.Context, check func(context.Context) error, stopArchive func(context.Context) error, failures chan<- error) {
	if check == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probeCtx, stop := context.WithTimeout(ctx, 15*time.Second)
			err := check(probeCtx)
			stop()
			if err != nil && ctx.Err() == nil {
				stopCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
				stopErr := stopArchive(stopCtx)
				stop()
				if stopErr != nil {
					err = fmt.Errorf("%w; stopping archiver failed: %v", err, stopErr)
				}
				select {
				case failures <- err:
				default:
				}
				return
			}
		}
	}
}
