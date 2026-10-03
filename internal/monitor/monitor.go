package monitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/aimdotsh/dbops/internal/metrics"
	"github.com/aimdotsh/dbops/internal/repository"
	"sync"
	"time"
)

type Collector func(context.Context, int64) (any, error)
type Monitor struct {
	Databases  repository.DatabaseRepository
	Agents     repository.AgentRepository
	Store      *metrics.Store
	Collectors map[string]Collector
	last       time.Time
}

func (m *Monitor) Tick(ctx context.Context) error {
	if time.Since(m.last) < 30*time.Second {
		return nil
	}
	m.last = time.Now()
	instances, err := m.Databases.List(ctx)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	sem := make(chan struct{}, 4)
	errs := make(chan error, len(instances))
	for _, inst := range instances {
		collect := m.Collectors[inst.DBType]
		if collect == nil {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		wg.Add(1)
		go func(instID, hostID int64, currentStatus string, collect Collector) {
			defer wg.Done()
			defer func() { <-sem }()
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if m.Agents != nil {
				agent, lookupErr := m.Agents.GetByHostID(work, hostID)
				if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
					errs <- lookupErr
					return
				}
				if lookupErr != nil || agent.Status != "online" {
					if err := m.Store.Put(ctx, "database", instID, time.Now().UTC(), map[string]any{"collection_state": "agent_unreachable"}); err != nil {
						errs <- err
					}
					if currentStatus != "unreachable" {
						if err := m.Databases.UpdateStatus(ctx, instID, "unreachable"); err != nil {
							errs <- err
						}
					}
					return
				}
			}
			value, err := collect(work, instID)
			desiredStatus := "online"
			payload := map[string]any{"up": 1}
			if err != nil {
				desiredStatus = "offline"
				payload = map[string]any{"up": 0}
			} else {
				raw, e := json.Marshal(value)
				if e == nil {
					_ = json.Unmarshal(raw, &payload)
				}
				payload["up"] = 1
			}
			if err = m.Store.Put(ctx, "database", instID, time.Now().UTC(), payload); err != nil {
				errs <- err
			}
			if currentStatus != desiredStatus {
				if err := m.Databases.UpdateStatus(ctx, instID, desiredStatus); err != nil {
					errs <- err
				}
			}
		}(inst.ID, inst.HostID, inst.Status, collect)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return nil
}
