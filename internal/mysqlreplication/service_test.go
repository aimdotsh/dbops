package mysqlreplication

import "testing"

func TestOptionalInt64PreservesUnknownLag(t *testing.T) {
	if got := optionalInt64(nil); got != nil {
		t.Fatalf("NULL lag should remain unknown, got %v", *got)
	}
	got := optionalInt64(int64(7))
	if got == nil || *got != 7 {
		t.Fatalf("numeric lag was not preserved: %v", got)
	}
}
