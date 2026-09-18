package sqlite

import (
	"context"
	"testing"

	"github.com/aimdotsh/dbops/internal/domain"
)

func TestTaskStepAndEvent(t *testing.T) {
	db := testDBWithDetails(t)
	defer db.Close()

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
