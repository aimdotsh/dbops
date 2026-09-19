package httpapi

import (
	"context"
	"encoding/json"
	"github.com/aimdotsh/dbops/internal/auth"
	"github.com/aimdotsh/dbops/internal/platformbackup"
	repo "github.com/aimdotsh/dbops/internal/repository/sqlite"
	"github.com/aimdotsh/dbops/internal/storage"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopeIsolationAndContentTypeBypass(t *testing.T) {
	dir := t.TempDir()
	stores, err := storage.Open(filepath.Join(dir, "meta.db"), filepath.Join(dir, "metrics.db"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	if err = storage.Migrate(stores.Metadata, stores.Metrics); err != nil {
		t.Fatal(err)
	}
	_, err = stores.Metadata.Exec(`INSERT INTO users(id,username,password_hash,status,created_at,updated_at) VALUES(1,'dba','x','active','','');INSERT INTO projects(id,name,created_at) VALUES(1,'p','');INSERT INTO environments(id,code,name) VALUES(1,'test','Test'),(2,'prod','Production');INSERT INTO hosts(id,hostname,ip_address,project_id,environment_id,created_at,updated_at) VALUES(1,'test','127.0.0.1',1,1,'',''),(2,'prod','127.0.0.2',1,2,'','');INSERT INTO user_resource_scopes VALUES(1,1,1);`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(nil, auth.Config{Enabled: true, JWTSecret: strings.Repeat("x", 32)})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{auth: a, platformBackup: &platformbackup.Service{Metadata: stores.Metadata}, hosts: repo.HostRepo{DB: stores.Metadata}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(authClaimsKey, auth.Claims{UserID: 1, Roles: []string{auth.RoleDBA}}) }, s.scopeGuard)
	r.GET("/api/v1/hosts", s.listHosts)
	r.GET("/api/v1/hosts/:id", s.getHost)
	r.POST("/api/v1/mysql/install", func(c *gin.Context) { c.Status(202) })
	for path, status := range map[string]int{"/api/v1/hosts/1": 200, "/api/v1/hosts/2": 403} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/hosts", nil))
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Data) != 1 {
		t.Fatalf("scope list leaked: %s", w.Body.String())
	}
	// Missing Content-Type must not bypass body-resource authorization.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/mysql/install", strings.NewReader(`{"agent_id":999}`)))
	if w.Code != 403 {
		t.Fatalf("body scope bypass: %d", w.Code)
	}
	for _, payload := range []string{`{"AGENT_ID":999}`, `{"Agent_Id":999}`, `{"agent_id":1,"AGENT_ID":999}`} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/mysql/install", strings.NewReader(payload)))
		if w.Code != 403 {
			t.Fatalf("case-insensitive binding bypass: %s status=%d", payload, w.Code)
		}
	}
	_, _ = stores.Metadata.ExecContext(context.Background(), "DELETE FROM user_resource_scopes")
	allowed, err := s.resourceAllowed(context.Background(), 1, "host", 1)
	if err != nil || allowed {
		t.Fatal("scope revoke did not take effect")
	}
}
