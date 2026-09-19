package oracle

import "testing"

func TestOracleServerValidation(t *testing.T) {
	for _, bad := range []string{"", "/", "relative.dbf", "/u01/a.dbf'"} {
		if err := validatePath(bad); err == nil {
			t.Fatalf("expected invalid path: %q", bad)
		}
	}
	if err := validatePath("/u01/oradata/ORCL/users02.dbf"); err != nil {
		t.Fatal(err)
	}
	if !validIdentifier("APP_DATA") {
		t.Fatal("expected APP_DATA to be valid")
	}
	if validIdentifier("APP DATA") {
		t.Fatal("identifier with spaces must be invalid")
	}
}
