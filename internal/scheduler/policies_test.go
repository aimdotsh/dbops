package scheduler

import (
	"context"
	"encoding/json"
	"github.com/aimdotsh/dbops/internal/domain"
	repo "github.com/aimdotsh/dbops/internal/repository/sqlite"
	"github.com/aimdotsh/dbops/internal/storage"
	"path/filepath"
	"testing"
	"time"
)

func TestScheduleOccurrenceSurvivesCrashAfterTaskCompletion(t *testing.T) {
	dir := t.TempDir()
	stores, err := storage.Open(filepath.Join(dir, "m.db"), filepath.Join(dir, "s.db"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	if err = storage.Migrate(stores.Metadata, stores.Metrics); err != nil {
		t.Fatal(err)
	}
	r := repo.TaskRepo{DB: stores.Metadata}
	ctx := context.Background()
	p := Policies{DB: stores.Metadata, Handlers: map[string]func(context.Context, json.RawMessage) (domain.Task, error){"platform.backup": func(ctx context.Context, _ json.RawMessage) (domain.Task, error) {
		return r.Create(ctx, domain.Task{TaskType: "platform.backup"})
	}}}
	v, err := p.Create(ctx, Policy{Name: "daily", TaskType: "platform.backup", Parameters: json.RawMessage(`{}`), IntervalSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	_, err = stores.Metadata.Exec("UPDATE backup_schedules SET next_run=? WHERE id=?", due, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	tasks, err := r.List(ctx, 50)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks: %v %v", tasks, err)
	}
	if err = r.UpdateStatus(ctx, tasks[0].ID, "success", 100, "{}", ""); err != nil {
		t.Fatal(err)
	}
	// Simulate schedule cursor not persisted although the task already completed.
	_, err = stores.Metadata.Exec("UPDATE backup_schedules SET next_run=? WHERE id=?", due, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	tasks, err = r.List(ctx, 50)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("duplicated occurrence: %v %v", tasks, err)
	}
}
