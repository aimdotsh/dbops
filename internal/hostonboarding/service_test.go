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
