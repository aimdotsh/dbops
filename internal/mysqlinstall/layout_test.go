package mysqlinstall

import "testing"

func TestPerPortLayoutAndOverrides(t *testing.T) {
	a := InstallRequest{Port: 13307}
	if err := NormalizeLayout(&a); err != nil {
		t.Fatal(err)
	}
	if a.BaseDir != "/opt/dbops/mysql/13307/base" || a.DataDir != "/opt/dbops/mysql/13307/data" || a.ConfigPath != "/opt/dbops/mysql/13307/conf/my.cnf" || a.ServiceName != "dbops-mysql13307" {
		t.Fatalf("unexpected layout: %+v", a)
	}
	b := InstallRequest{Port: 13308, InstallRoot: "/srv/custom/", DataDir: "/volume/mysql-data", ServiceName: "custom-mysql.service"}
	if err := NormalizeLayout(&b); err != nil {
		t.Fatal(err)
	}
	if b.BaseDir != "/srv/custom/mysql/13308/base" || b.DataDir != "/volume/mysql-data" || b.ServiceName != "custom-mysql" {
		t.Fatalf("custom overrides lost: %+v", b)
	}
	if err := NormalizeLayout(&b); err != nil {
		t.Fatal(err)
	}
	if b.ServiceName != "custom-mysql" {
		t.Fatal("normalization is not idempotent")
	}
	for _, req := range []InstallRequest{{InstallRoot: "/"}, {InstallRoot: "relative"}, {InstallRoot: "/srv/has space"}, {ServiceName: "../../other.service"}, {Port: 65536}} {
		if err := NormalizeLayout(&req); err == nil {
			t.Fatalf("unsafe layout accepted: %+v", req)
		}
	}
}
