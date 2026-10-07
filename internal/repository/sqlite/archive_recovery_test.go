package sqlite

import (
	"context"
	"testing"

	"github.com/aimdotsh/dbops/internal/domain"
)

func TestRecoveryMakesUnfinishedArchiveJobsReviewable(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	r := TaskRepo{DB: db}
	ctx := context.Background()
	for _, tc := range []struct{ task, job, want, verification string }{
		{"interrupted", "running", "interrupted", "needs_review"},
		{"cancelled", "pending", "interrupted", "needs_review"},
		{"failed", "pause_requested", "interrupted", "needs_review"},
		{"timeout", "running", "interrupted", "needs_review"},
		{"running", "running", "running", "baseline_captured"},
		{"success", "success", "success", "verified"},
		{"success", "paused", "paused", "baseline_captured"},
	} {
		task, err := r.Create(ctx, domain.Task{TaskType: "mysql.archive.run"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("UPDATE tasks SET status=? WHERE id=?", tc.task, task.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("INSERT INTO archive_jobs(id,task_id,status,verification_status) VALUES(?,?,?,?)", task.ID, task.ID, tc.job, map[bool]string{true: "verified", false: "baseline_captured"}[tc.job == "success"]); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, err = r.RecoverExpired(ctx); err != nil {
				t.Fatal(err)
			}
			var status, verification string
			if err = db.QueryRow("SELECT status,verification_status FROM archive_jobs WHERE id=?", task.ID).Scan(&status, &verification); err != nil {
				t.Fatal(err)
			}
			if status != tc.want || verification != tc.verification {
				t.Fatalf("task=%s job=%s: got %s/%s, want %s/%s", tc.task, tc.job, status, verification, tc.want, tc.verification)
			}
		}
	}
}
