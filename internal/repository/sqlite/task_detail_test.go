package sqlite

import (
	"context"
	"testing"

	"github.com/aimdotsh/dbops/internal/domain"
)

func TestTaskStepAndEvent(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	if _, err := db.Exec(`
CREATE TABLE task_steps (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL,
  step_no INTEGER NOT NULL,
  step_code TEXT NOT NULL,
  step_name TEXT,
  status TEXT NOT NULL,
  progress INTEGER NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  output_json TEXT,
  error_message TEXT,
  recovery_policy TEXT,
  UNIQUE(task_id,step_no)
);
CREATE TABLE task_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL,
  event_time TEXT NOT NULL,
  event_type TEXT NOT NULL,
  step_code TEXT,
  level TEXT NOT NULL,
  message TEXT,
  payload_json TEXT NOT NULL
);`); err != nil {
		t.Fatal(err)
	}

	repo := TaskRepo{DB: db}
	ctx := context.Background()
	task, err := repo.Create(ctx, domain.Task{TaskType: "system.echo"})
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.UpsertStep(ctx, domain.TaskStep{
		TaskID: task.ID, StepNo: 1, StepCode: "TEST", StepName: "Test",
		Status: "running", Progress: 20,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertStep(ctx, domain.TaskStep{
		TaskID: task.ID, StepNo: 1, StepCode: "TEST", StepName: "Test",
		Status: "success", Progress: 100, OutputJSON: "{}",
	}); err != nil {
		t.Fatal(err)
	}

	steps, err := repo.ListSteps(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Status != "success" || steps[0].Progress != 100 {
		t.Fatalf("unexpected steps: %+v", steps)
	}

	if err := repo.AddEvent(ctx, domain.TaskEvent{
		TaskID: task.ID, EventType: "test", Message: "ok",
	}); err != nil {
		t.Fatal(err)
	}
	events, err := repo.ListEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Message != "ok" {
		t.Fatalf("unexpected events: %+v", events)
	}
}
