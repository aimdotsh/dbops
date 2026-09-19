package httpapi

import (
	"net/http"
	"strconv"

	dorissvc "github.com/aimdotsh/dbops/internal/doris"
	"github.com/gin-gonic/gin"
)

func (s *Server) onboardDoris(c *gin.Context) {
	var body dorissvc.OnboardRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	instance, err := s.doris.Onboard(c.Request.Context(), body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "DORIS_ONBOARD_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": "OK", "message": "created", "data": instance})
}

func (s *Server) dorisClusterStatus(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	out, err := s.doris.ClusterStatus(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "DORIS_CLUSTER_STATUS_FAILED", "message": err.Error()})
		return
	}
	ok(c, out)
}

func (s *Server) createDorisBackup(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var body dorissvc.BackupRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	task, err := s.doris.CreateBackupTask(c.Request.Context(), id, body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "DORIS_BACKUP_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}

func (s *Server) listDorisBackups(c *gin.Context) {
	instanceID := int64(0)
	if raw := c.Query("instance_id"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "invalid instance_id"})
			return
		}
		instanceID = v
	}
	items, err := s.doris.ListBackups(c.Request.Context(), instanceID)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}
