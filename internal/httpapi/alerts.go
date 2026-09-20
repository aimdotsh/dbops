package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) listAlerts(c *gin.Context) {
	status := c.Query("status")
	limit := 200
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			limit = v
		}
	}
	items, err := s.alerts.List(c.Request.Context(), status, limit)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) acknowledgeAlert(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	if err := s.alerts.Acknowledge(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "ALERT_ACK_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "OK", "message": "acknowledged"})
}

func (s *Server) silenceAlert(c *gin.Context) {
	id, valid := parseID(c)
	if !valid {
		return
	}
	var req struct {
		Seconds int `json:"seconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": "INVALID_PARAMETER"})
		return
	}
	if err := s.alerts.Silence(c.Request.Context(), id, time.Duration(req.Seconds)*time.Second); err != nil {
		c.JSON(400, gin.H{"code": "SILENCE_REJECTED", "message": err.Error()})
		return
	}
	ok(c, req)
}
