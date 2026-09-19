package httpapi

import (
	"net/http"

	"github.com/aimdotsh/dbops/internal/mysqlservice"
	"github.com/gin-gonic/gin"
)

type confirmationRequest struct {
	Confirmed bool `json:"confirmed"`
}

func (s *Server) startMySQLInstance(c *gin.Context) {
	s.createMySQLServiceTask(c, "mysql.start")
}

func (s *Server) stopMySQLInstance(c *gin.Context) {
	s.createMySQLServiceTask(c, "mysql.stop")
}

func (s *Server) restartMySQLInstance(c *gin.Context) {
	s.createMySQLServiceTask(c, "mysql.restart")
}

func (s *Server) createMySQLServiceTask(c *gin.Context, action string) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var body confirmationRequest
	_ = c.ShouldBindJSON(&body)
	task, err := s.mysqlService.CreateTask(c.Request.Context(), mysqlservice.Request{
		InstanceID: id,
		Action:     action,
		Confirmed:  body.Confirmed,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_SERVICE_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}
