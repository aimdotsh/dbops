package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed web/dist/* web/dist/assets/*
var embeddedWeb embed.FS

func registerWeb(r *gin.Engine) {
	root, err := fs.Sub(embeddedWeb, "web/dist")
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(root))
	r.GET("/assets/*filepath", gin.WrapH(fileServer))

	serveIndex := func(c *gin.Context) {
		b, err := fs.ReadFile(root, "index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", b)
	}
	r.GET("/", serveIndex)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND"})
			return
		}
		serveIndex(c)
	})
}
