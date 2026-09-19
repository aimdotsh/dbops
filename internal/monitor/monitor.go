package monitor

import (
	"context"
	"encoding/json"
	"github.com/aimdotsh/dbops/internal/metrics"
	"github.com/aimdotsh/dbops/internal/repository"
	"sync"
	"time"
)

type Collector func(context.Context, int64) (any, error)
type Monitor struct {
	Databases  repository.DatabaseRepository
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
		go func(id int64, collect Collector) {
			defer wg.Done()
			defer func() { <-sem }()
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			value, err := collect(work, id)
			payload := map[string]any{"up": 1}
			if err != nil {
				payload = map[string]any{"up": 0}
			} else {
				raw, e := json.Marshal(value)
				if e == nil {
					_ = json.Unmarshal(raw, &payload)
				}
				payload["up"] = 1
			}
			if err = m.Store.Put(ctx, "database", id, time.Now().UTC(), payload); err != nil {
				errs <- err
			}
		}(inst.ID, collect)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return nil
}
