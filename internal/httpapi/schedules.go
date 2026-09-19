package httpapi

import (
	"encoding/json"
	"github.com/aimdotsh/dbops/internal/scheduler"
	"github.com/aimdotsh/dbops/internal/security"
	"github.com/gin-gonic/gin"
)

func (s *Server) listSchedules(c *gin.Context) {
	v, err := s.policies.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, v)
}
func (s *Server) createSchedule(c *gin.Context) {
	var v scheduler.Policy
	if err := c.ShouldBindJSON(&v); err != nil {
		c.JSON(400, gin.H{"code": "INVALID_PARAMETER"})
		return
	}
	var params any
	if err := json.Unmarshal(v.Parameters, &params); err != nil {
		c.JSON(400, gin.H{"code": "INVALID_PARAMETER"})
		return
	}
	before, _ := json.Marshal(params)
	after, _ := json.Marshal(security.RedactValue(params))
	if string(before) != string(after) {
		c.JSON(400, gin.H{"code": "SECRET_PARAMETER_REJECTED", "message": "schedules must reference managed instances; do not include credentials"})
		return
	}
	v, err := s.policies.Create(c.Request.Context(), v)
	if err != nil {
		c.JSON(400, gin.H{"code": "SCHEDULE_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(201, gin.H{"code": "OK", "data": v})
}
func (s *Server) toggleSchedule(c *gin.Context) {
	id, valid := parseID(c)
	if !valid {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": "INVALID_PARAMETER"})
		return
	}
	if err := s.policies.SetEnabled(c.Request.Context(), id, req.Enabled); err != nil {
		c.JSON(400, gin.H{"code": "SCHEDULE_REJECTED", "message": err.Error()})
		return
	}
	ok(c, req)
}
