package httpapi

import (
	"net/http"

	"github.com/aimdotsh/dbops/internal/mysqlreplication"
	"github.com/gin-gonic/gin"
)

func (s *Server) listMySQLReplications(c *gin.Context) {
	items, err := s.mysqlReplication.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) createMySQLReplication(c *gin.Context) {
	var req mysqlreplication.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	task, err := s.mysqlReplication.CreateTask(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_REPLICATION_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}

func (s *Server) refreshMySQLReplication(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	item, err := s.mysqlReplication.Refresh(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_REPLICATION_REFRESH_FAILED", "message": err.Error()})
		return
	}
	ok(c, item)
}
