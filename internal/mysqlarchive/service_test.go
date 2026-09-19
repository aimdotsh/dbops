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
