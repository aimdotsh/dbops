package mysqlreplication

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/domain"
)

func TestOptionalInt64PreservesUnknownLag(t *testing.T) {
	if got := optionalInt64(nil); got != nil {
		t.Fatalf("NULL lag should remain unknown, got %v", *got)
	}
	got := optionalInt64(int64(7))
	if got == nil || *got != 7 {
		t.Fatalf("numeric lag was not preserved: %v", got)
	}
}

func TestSupportsSafeGTIDDump(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    bool
	}{
		{"8.0.31", false}, {"8.0.32", true}, {"8.0.46-commercial", true},
		{"8.4.0", true}, {"5.7.44", false}, {"unknown", false},
	} {
		if got := supportsSafeGTIDDump(tc.version); got != tc.want {
			t.Errorf("version %q: got %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestCreateRequiresOneBaselineMode(t *testing.T) {
	service := &Service{}
	for _, tc := range []struct{ prepared, auto bool }{{false, false}, {true, true}} {
		_, err := service.CreateTask(context.Background(), CreateRequest{
			PrimaryInstanceID: 1, ReplicaInstanceID: 2,
			BaselineReady: tc.prepared, AutoBaseline: tc.auto, Confirmed: true,
		})
		if err == nil {
			t.Errorf("prepared=%v auto=%v: expected rejection", tc.prepared, tc.auto)
		}
	}
}

func TestCreateRejectsInvalidSourceHostBeforeTaskCreation(t *testing.T) {
	service := &Service{}
	for _, host := range []string{"localhost", "127.0.0.1", "0.0.0.0", "224.0.0.1", "fe80::1", "2001:db8::1"} {
		_, err := service.CreateTask(context.Background(), CreateRequest{
			PrimaryInstanceID: 1, ReplicaInstanceID: 2, SourceHost: host,
			AutoBaseline: true, Confirmed: true,
		})
		if err == nil || !strings.Contains(err.Error(), "source_host") {
			t.Errorf("source_host %q: expected validation error, got %v", host, err)
		}
	}
}

func TestWaitForHealthyReplicationAllowsConnectionStartup(t *testing.T) {
	reads := 0
	status, err := waitForHealthyReplication(context.Background(), time.Second, time.Millisecond, func() (map[string]any, error) {
		reads++
		if reads == 1 {
			return map[string]any{"status": "degraded", "io_thread_status": "Connecting"}, nil
		}
		return map[string]any{"status": "healthy", "io_thread_status": "Yes"}, nil
	})
	if err != nil || status["status"] != "healthy" || reads != 2 {
		t.Fatalf("status=%v err=%v reads=%d", status, err, reads)
	}
}

type baselineDispatcher struct{ calls []string }

func (f *baselineDispatcher) Dispatch(_ context.Context, agentID int64, req agentproto.ActionRequest) (agentproto.ActionResponse, error) {
	if req.TaskID != 0 {
		return agentproto.ActionResponse{}, fmt.Errorf("transfer exposed task payload")
	}
	mode, _ := req.Params["mode"].(string)
	f.calls = append(f.calls, fmt.Sprintf("%d:%s:%s", agentID, req.Action, mode))
	result := map[string]any{}
	switch req.Action {
	case "mysql.replication.create":
		switch mode {
		case "baseline_export":
			result = map[string]any{"path": "/tmp/source.sql.gz", "sha256": strings.Repeat("a", 64)}
		case "baseline_import":
			if req.Params["backup_path"] != "/tmp/transfer/backup.sql.gz" {
				return agentproto.ActionResponse{}, fmt.Errorf("incorrect transferred path")
			}
			result = map[string]any{"restored": true, "user_databases": float64(1)}
		case "baseline_cleanup":
			result = map[string]any{"cleaned": true}
		default:
			return agentproto.ActionResponse{}, fmt.Errorf("unexpected mode %q", mode)
		}
	case "backup.transfer.export":
		result = map[string]any{"size_bytes": float64(3), "sha256": strings.Repeat("b", 64)}
	case "backup.transfer.read":
		result = map[string]any{"bytes": float64(3), "chunk": "YWJj"}
	case "backup.transfer.write":
		result = map[string]any{"bytes": float64(3)}
	case "backup.transfer.finish":
		result = map[string]any{"path": "/tmp/transfer"}
	case "backup.transfer.cleanup":
	default:
		return agentproto.ActionResponse{}, fmt.Errorf("unexpected action %q", req.Action)
	}
	return agentproto.ActionResponse{Result: result}, nil
}

func TestSeedReplicaTransfersBeforeImport(t *testing.T) {
	fake := &baselineDispatcher{}
	svc := &Service{dispatcher: fake}
	primary := runtime{Agent: domain.Agent{ID: 1}, BaseDir: "/mysql/primary", RunDir: "/run/primary", Password: "source-secret"}
	replica := runtime{Agent: domain.Agent{ID: 2}, BaseDir: "/mysql/replica", RunDir: "/run/replica", Password: "replica-secret"}
	result, err := svc.seedReplica(context.Background(), 1, primary, replica)
	if err != nil {
		t.Fatal(err)
	}
	if !boolValue(result["seeded"]) {
		t.Fatalf("baseline not seeded: %v", result)
	}
	want := []string{
		"1:mysql.replication.create:baseline_export", "1:backup.transfer.export:",
		"1:backup.transfer.read:", "2:backup.transfer.write:",
		"2:backup.transfer.finish:", "2:mysql.replication.create:baseline_import",
		"1:mysql.replication.create:baseline_cleanup", "1:backup.transfer.cleanup:", "2:backup.transfer.cleanup:",
	}
	if strings.Join(fake.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
}
