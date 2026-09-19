package actionpolicy

import "testing"

func TestHighRiskRequiresConfirmation(t *testing.T) {
	if _, err := Validate("mysql.stop", false); err == nil {
		t.Fatal("expected mysql.stop to require confirmation")
	}
	p, err := Validate("mysql.stop", true)
	if err != nil {
		t.Fatal(err)
	}
	if p.Risk != R3 {
		t.Fatalf("unexpected risk: %s", p.Risk)
	}
}

func TestReadOnlyDoesNotRequireConfirmation(t *testing.T) {
	p, err := Validate("mysql.precheck", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Risk != R0 {
		t.Fatalf("unexpected risk: %s", p.Risk)
	}
}
