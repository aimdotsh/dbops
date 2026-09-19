package httpapi

import (
	"net/http"
	"strconv"

	pgsvc "github.com/aimdotsh/dbops/internal/postgres"
	"github.com/gin-gonic/gin"
)

func (s *Server) onboardPostgres(c *gin.Context) {
	var body pgsvc.OnboardRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	instance, err := s.postgres.Onboard(c.Request.Context(), body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "POSTGRES_ONBOARD_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": "OK", "message": "created", "data": instance})
}

func (s *Server) postgresStatus(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	out, err := s.postgres.Status(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "POSTGRES_STATUS_FAILED", "message": err.Error()})
		return
	}
	ok(c, out)
}

func (s *Server) postgresReplicationStatus(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	out, err := s.postgres.ReplicationStatus(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "POSTGRES_REPLICATION_STATUS_FAILED", "message": err.Error()})
		return
	}
	ok(c, out)
}

func (s *Server) createPostgresBackup(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var body pgsvc.BackupRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	task, err := s.postgres.CreateBackupTask(c.Request.Context(), id, body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "POSTGRES_BACKUP_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}

func (s *Server) listPostgresBackups(c *gin.Context) {
	instanceID := int64(0)
	if raw := c.Query("instance_id"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "invalid instance_id"})
			return
		}
		instanceID = v
	}
	items, err := s.postgres.ListBackups(c.Request.Context(), instanceID)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}
