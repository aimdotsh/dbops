package sqlite

import (
	"context"
	"testing"

	"github.com/aimdotsh/dbops/internal/domain"
)

func TestRecoveryReconcilesUnfinishedBackups(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	r := TaskRepo{DB: db}
	ctx := context.Background()
	for _, tc := range []struct{ task, backup, want string }{
		{"interrupted", "running", "failed"},
		{"cancelled", "pending", "failed"},
		{"failed", "running", "failed"},
		{"timeout", "running", "failed"},
		{"running", "running", "running"},
		{"queued", "pending", "pending"},
		{"interrupted", "success", "success"},
		{"success", "success", "success"},
	} {
		task, err := r.Create(ctx, domain.Task{TaskType: "mysql.backup"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("UPDATE tasks SET status=? WHERE id=?", tc.task, task.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("INSERT INTO backup_jobs(id,task_id,status) VALUES(?,?,?)", task.ID, task.ID, tc.backup); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, err = r.RecoverExpired(ctx); err != nil {
				t.Fatal(err)
			}
			var got string
			if err = db.QueryRow("SELECT status FROM backup_jobs WHERE id=?", task.ID).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("task=%s backup=%s: got %s, want %s", tc.task, tc.backup, got, tc.want)
			}
		}
	}
}
