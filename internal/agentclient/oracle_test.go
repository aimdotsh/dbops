package agentclient

import "testing"

func TestValidateOracleDatafilePath(t *testing.T) {
	for _, bad := range []string{"", "/", "relative/a.dbf", "/tmp/a.dbf';drop"} {
		if err := validateOracleDatafilePath(bad); err == nil {
			t.Fatalf("expected invalid path: %q", bad)
		}
	}
	if err := validateOracleDatafilePath("/u01/oradata/TEST/users02.dbf"); err != nil {
		t.Fatal(err)
	}
}

func TestOracleIdentifier(t *testing.T) {
	for _, good := range []string{"ORCL", "USERS", "APP_DATA_01", "SYS$USERS"} {
		if !validOracleIdentifier(good) {
			t.Fatalf("expected valid identifier: %q", good)
		}
	}
	for _, bad := range []string{"", "USERS;DROP", "A B", "A-B"} {
		if validOracleIdentifier(bad) {
			t.Fatalf("expected invalid identifier: %q", bad)
		}
	}
}
