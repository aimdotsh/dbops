package hostonboarding

import "testing"

func TestNormalizeArch(t *testing.T) {
	for input, want := range map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"} {
		got, err := normalizeArch(input)
		if err != nil || got != want {
			t.Fatalf("normalizeArch(%q) = %q, %v", input, got, err)
		}
	}
	if _, err := normalizeArch("riscv64"); err == nil {
		t.Fatal("unsupported architecture accepted")
	}
}

func TestValidateServerURL(t *testing.T) {
	for _, value := range []string{"http://dbops:8080", "https://dbops.example.com"} {
		if err := validateServerURL(value); err != nil {
			t.Fatalf("valid URL %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"", "file:///tmp/a", "dbops:8080"} {
		if err := validateServerURL(value); err == nil {
			t.Fatalf("invalid URL %q accepted", value)
		}
	}
}

func TestParseHostProbeIgnoresLoginBanner(t *testing.T) {
	output := "Welcome to the server\r\n__DBOPS_PROBE_V1__\nOS=Linux\nARCH=aarch64\nHOSTNAME=db01\nSYSTEMCTL=/usr/bin/systemctl\nELEVATION=passwordless\n__DBOPS_PROBE_END__\n"
	probe, err := parseHostProbe(output)
	if err != nil {
		t.Fatal(err)
	}
	if probe.os != "Linux" || probe.arch != "aarch64" || probe.hostname != "db01" || probe.systemctl == "" {
		t.Fatalf("unexpected probe: %#v", probe)
	}
}

func TestParseHostProbeReportsMissingMarkerAndFields(t *testing.T) {
	if _, err := parseHostProbe("restricted shell"); err == nil {
		t.Fatal("missing marker accepted")
	}
	if _, err := parseHostProbe("__DBOPS_PROBE_V1__\nOS=Linux\n__DBOPS_PROBE_END__"); err == nil {
		t.Fatal("missing fields accepted")
	}
}
