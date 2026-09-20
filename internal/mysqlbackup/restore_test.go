package mysqlbackup

import (
	"context"
	"errors"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
	"strings"
	"testing"
)

type restoreBackups struct{ repository.BackupJobRepository }

type physicalRestoreBackups struct {
	repository.BackupJobRepository
	metadata string
}

func (r physicalRestoreBackups) Get(context.Context, int64) (domain.BackupJob, error) {
	return domain.BackupJob{TaskID: 10, Status: "success", BackupEngine: "xtrabackup", DatabaseInstanceID: 1, MetadataJSON: r.metadata}, nil
}

func TestPhysicalRestoreRequiresFullManifest(t *testing.T) {
	for _, tc := range []struct {
		meta    string
		allowed bool
	}{
		{`{}`, false},
		{`{"checksum_scope":"checkpoint"}`, false},
		{`{"checksum_scope":"sorted-file-manifest-v1"}`, true},
	} {
		s := &Service{backups: physicalRestoreBackups{metadata: tc.meta}, tasks: restoreTasks{parameters: `{}`}, dbs: restoreDatabases{}}
		_, _, err := s.validateRestore(context.Background(), RestoreRequest{BackupID: 1, TargetInstanceID: 2, Confirmed: true})
		if tc.allowed && !errors.Is(err, reachedRuntime) {
			t.Fatalf("full manifest rejected: %v", err)
		}
		if !tc.allowed && (err == nil || !strings.Contains(err.Error(), "full-file manifest")) {
			t.Fatalf("legacy backup not rejected: %v", err)
		}
	}
}

func (restoreBackups) Get(context.Context, int64) (domain.BackupJob, error) {
	return domain.BackupJob{TaskID: 10, Status: "success", BackupEngine: "mysqldump", DatabaseInstanceID: 1}, nil
}

type restoreTasks struct {
	repository.TaskRepository
	parameters string
}

func (r restoreTasks) Get(context.Context, int64) (domain.Task, error) {
	return domain.Task{ParametersJSON: r.parameters}, nil
}

type restoreDatabases struct{ repository.DatabaseRepository }

var reachedRuntime = errors.New("reached runtime lookup")

func (restoreDatabases) Get(context.Context, int64) (domain.DatabaseInstance, error) {
	return domain.DatabaseInstance{}, reachedRuntime
}
func TestRestoreRejectsSystemSchemasBeforeRuntime(t *testing.T) {
	for _, tc := range []struct {
		params  string
		allowed bool
	}{
		{`{"all_databases":true}`, false},
		{`{"databases":["mysql"]}`, false},
		{`{"databases":["orders","MySQL"]}`, false},
		{`{"databases":["sys"]}`, false},
		{`{}`, false},
		{`{"databases":["orders"]}`, true},
	} {
		t.Run(tc.params, func(t *testing.T) {
			s := &Service{backups: restoreBackups{}, tasks: restoreTasks{parameters: tc.params}, dbs: restoreDatabases{}}
			_, _, err := s.validateRestore(context.Background(), RestoreRequest{BackupID: 1, TargetInstanceID: 2, Confirmed: true})
			if tc.allowed {
				if !errors.Is(err, reachedRuntime) {
					t.Fatalf("user schema rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "system schemas") {
				t.Fatalf("unsafe restore was not blocked: %v", err)
			}
		})
	}
}
