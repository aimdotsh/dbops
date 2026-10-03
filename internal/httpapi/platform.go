package httpapi

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func (s *Server) listPlatformBackups(c *gin.Context) {
	items, err := s.platformBackup.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}
func (s *Server) createPlatformBackup(c *gin.Context) {
	t, err := s.platformBackup.CreateTask(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "data": t})
}
