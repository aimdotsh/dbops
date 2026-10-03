package agentclient

import "testing"

func TestParseArchiveStatsRealOutput(t *testing.T) {
	output := "TIME ELAPSED COUNT\nSELECT 100\nINSERT 100\nDELETE 100\nAction Count Time Pct\ninserting 100 0.01 1.0"
	scanned, archived, deleted, ok := parseArchiveStats(output)
	if !ok || scanned != 100 || archived != 100 || deleted != 100 {
		t.Fatalf("real stats: scanned=%d archived=%d deleted=%d ok=%v", scanned, archived, deleted, ok)
	}
	_, _, _, ok = parseArchiveStats("command completed without statistics")
	if ok {
		t.Fatal("missing statistics accepted")
	}
}
