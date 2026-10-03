package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAssetsSPARouteDoesNotExposeStaticDirectory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerWeb(r)

	for _, path := range []string{"/assets", "/assets/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, res.Code)
		}
		if contentType := res.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
			t.Fatalf("GET %s content type = %q", path, contentType)
		}
		if !strings.Contains(res.Body.String(), "<!doctype html>") {
			t.Fatalf("GET %s did not return the SPA index", path)
		}
	}
}
