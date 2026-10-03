package monitor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/metrics"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/aimdotsh/dbops/internal/storage"
)

type monitorDatabases struct {
	repository.DatabaseRepository
	instance domain.DatabaseInstance
}

func (d *monitorDatabases) List(context.Context) ([]domain.DatabaseInstance, error) {
	return []domain.DatabaseInstance{d.instance}, nil
}

func (d *monitorDatabases) UpdateStatus(_ context.Context, id int64, status string) error {
	if id != d.instance.ID {
		return errors.New("unexpected instance")
	}
	d.instance.Status = status
	return nil
}

func TestTickUpdatesDatabaseAvailability(t *testing.T) {
	root := t.TempDir()
	stores, err := storage.Open(filepath.Join(root, "metadata.db"), filepath.Join(root, "metrics.db"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	if err := storage.Migrate(stores.Metadata, stores.Metrics); err != nil {
		t.Fatal(err)
	}
	dbs := &monitorDatabases{instance: domain.DatabaseInstance{ID: 1, DBType: "oracle", Status: "online"}}
	m := &Monitor{Databases: dbs, Store: metrics.NewStore(stores.Metrics), Collectors: map[string]Collector{
		"oracle": func(context.Context, int64) (any, error) { return nil, errors.New("database stopped") },
	}}
	ctx := context.Background()
	if err := m.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if dbs.instance.Status != "offline" {
		t.Fatalf("stopped database remained %q", dbs.instance.Status)
	}
	snapshot, err := m.Store.Latest(ctx, "database", 1)
	if err != nil || snapshot.Payload["up"] != float64(0) {
		t.Fatalf("expected down metric, got %+v, %v", snapshot, err)
	}
	m.Collectors["oracle"] = func(context.Context, int64) (any, error) { return map[string]any{"sessions": 2}, nil }
	m.last = time.Now().Add(-31 * time.Second)
	if err := m.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if dbs.instance.Status != "online" {
		t.Fatalf("recovered database remained %q", dbs.instance.Status)
	}
}
