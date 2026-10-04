package mysqlarchive

import (
	"strings"
	"testing"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

func TestMaterializeWhereCutoff(t *testing.T) {
	p := domain.ArchivePolicy{
		WhereTemplate: "create_time < :cutoff",
		RetentionDays: 30,
	}
	got, err := materializeWhere(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, ":cutoff") || !strings.Contains(got, "create_time < '") {
		t.Fatalf("unexpected materialized where: %s", got)
	}
}

func TestMaterializeWhereRejectsUnsafeSQL(t *testing.T) {
	_, err := materializeWhere(domain.ArchivePolicy{
		WhereTemplate: "id > 1; DROP TABLE x",
	})
	if err == nil {
		t.Fatal("expected unsafe where to be rejected")
	}
}

func TestMaterializeWhereRetentionRequired(t *testing.T) {
	_, err := materializeWhere(domain.ArchivePolicy{
		WhereTemplate: "created_at < :cutoff",
	})
	if err == nil {
		t.Fatal("expected retention requirement")
	}
}

func TestMaterializeWhereUsesUTC(t *testing.T) {
	p := domain.ArchivePolicy{WhereTemplate: "created_at < :cutoff", RetentionDays: 1}
	got, err := materializeWhere(p)
	if err != nil {
		t.Fatal(err)
	}
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	if !strings.Contains(got, yesterday) {
		t.Fatalf("expected UTC cutoff date %s in %s", yesterday, got)
	}
}

func TestCompareArchiveInspections(t *testing.T) {
	baseline := archiveInspection{SourceMatching: 100, DestinationMatching: 10, Bounded: true, Overlap: 0, UnionKeyDigest: "same", ProcessCheckOK: true}
	current := archiveInspection{SourceMatching: 60, DestinationMatching: 50, Bounded: true, Overlap: 0, UnionKeyDigest: "same", ProcessCheckOK: true}
	got := compareArchiveInspections(baseline, current, true)
	if !got.SafeToRetry || got.MovedRows != 40 {
		t.Fatalf("expected 40-row safe retry, got %+v", got)
	}
	if archiveResultVerified(got, "success") || !archiveResultVerified(got, "paused") {
		t.Fatal("a successful archive must have processed all matching source rows")
	}
	cases := []struct {
		name   string
		change func(*archiveInspection)
	}{
		{"overlap", func(v *archiveInspection) { v.Overlap = 1 }},
		{"missing key", func(v *archiveInspection) { v.UnionKeyDigest = "different" }},
		{"count drift", func(v *archiveInspection) { v.DestinationMatching = 49 }},
		{"process active", func(v *archiveInspection) { v.ProcessRunning = true }},
		{"process unknown", func(v *archiveInspection) { v.ProcessCheckOK = false }},
		{"unbounded", func(v *archiveInspection) { v.Bounded = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := current
			tc.change(&v)
			if result := compareArchiveInspections(baseline, v, true); result.SafeToRetry {
				t.Fatalf("unsafe retry allowed: %+v", result)
			}
		})
	}
	if result := compareArchiveInspections(baseline, current, false); result.SafeToRetry {
		t.Fatal("source-retaining retry allowed")
	}
	current.SourceMatching = 0
	current.DestinationMatching = 110
	if result := compareArchiveInspections(baseline, current, true); !result.Completed || result.SafeToRetry || result.MovedRows != 100 || !archiveResultVerified(result, "success") {
		t.Fatalf("expected completed reconciliation: %+v", result)
	}
}
