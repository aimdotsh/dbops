package hostonboarding

import "testing"

func TestDiagnosisEvidenceDoesNotClaimSQLHealth(t *testing.T) {
	listeners := "LISTEN 0 128 127.0.0.1:13307 0.0.0.0:*\nLISTEN 0 128 [::]:1521 [::]:*\n"
	if !listenerHasPort(listeners, 13307) || !listenerHasPort(listeners, 1521) || listenerHasPort(listeners, 3306) {
		t.Fatal("listener ports parsed incorrectly")
	}
	if !processHasName("mysqld\nora_pmon_dbops\n", "ora_pmon_dbops") {
		t.Fatal("Oracle PMON not found")
	}
	active := true
	if got := diagnosisConclusion(InstanceDiagnosis{ServiceActive: &active}); got != "process_running" {
		t.Fatalf("unexpected result %q", got)
	}
	if got := diagnosisConclusion(InstanceDiagnosis{PortListening: &active}); got != "port_listening" {
		t.Fatalf("port alone claimed process: %q", got)
	}
}
