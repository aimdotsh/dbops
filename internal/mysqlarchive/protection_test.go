package mysqlarchive

import "testing"

func TestLagSecondsRejectsUnknownAndInvalidValues(t *testing.T) {
	for _, v := range []any{nil, "unknown", -1, -1.0, 1.5} {
		if _, ok := lagSeconds(v); ok {
			t.Fatalf("accepted invalid lag: %v", v)
		}
	}
	for _, v := range []any{0, int64(2), float64(3)} {
		if _, ok := lagSeconds(v); !ok {
			t.Fatalf("rejected valid lag: %v", v)
		}
	}
}
