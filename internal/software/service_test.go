package software

import "testing"

func TestInferPackageType(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"mysql.tar.gz", "tar.gz"},
		{"mysql.tgz", "tgz"},
		{"percona-xtrabackup-80_8.0.35-36-1.noble_arm64.deb", "deb"},
		{"unknown.bin", ""},
	} {
		if got := inferPackageType(tc.name); got != tc.want {
			t.Fatalf("inferPackageType(%q)=%q, want %q", tc.name, got, tc.want)
		}
	}
}
