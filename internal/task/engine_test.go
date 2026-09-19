package task

import (
	"context"
	"github.com/aimdotsh/dbops/internal/domain"
	repo "github.com/aimdotsh/dbops/internal/repository/sqlite"
	"github.com/aimdotsh/dbops/internal/storage"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestLongTaskRenewsAndShutdownInterrupts(t *testing.T) {
	dir := t.TempDir()
	stores, err := storage.Open(filepath.Join(dir, "meta.db"), filepath.Join(dir, "metrics.db"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	if err = storage.Migrate(stores.Metadata, stores.Metrics); err != nil {
		t.Fatal(err)
	}
	r := repo.TaskRepo{DB: stores.Metadata}
	created, err := r.Create(context.Background(), domain.Task{TaskType: "long"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := r.ClaimNext(context.Background(), "worker", 2)
	if err != nil {
		t.Fatal(err)
	}
	engine := New(r, slog.New(slog.NewTextHandler(io.Discard, nil)), 1, 2, 1)
	engine.Register("long", func(ctx context.Context, t domain.Task) (any, error) { <-ctx.Done(); return nil, ctx.Err() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { engine.execute(ctx, *task); close(done) }()
	time.Sleep(3 * time.Second)
	if n, err := r.RecoverExpired(context.Background()); err != nil || n != 0 {
		t.Fatalf("live lease expired: %d %v", n, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown stuck")
	}
	result, err := r.Get(context.Background(), created.ID)
	if err != nil || result.Status != "interrupted" {
		t.Fatalf("shutdown status: %+v %v", result, err)
	}
}
