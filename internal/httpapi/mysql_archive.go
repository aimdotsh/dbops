package httpapi

import (
	"net/http"
	"strconv"

	"github.com/aimdotsh/dbops/internal/mysqlarchive"
	"github.com/gin-gonic/gin"
)

type archiveStartRequest struct {
	Confirmed bool `json:"confirmed"`
}

func (s *Server) createMySQLArchivePolicy(c *gin.Context) {
	var body mysqlarchive.PolicyRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	out, err := s.mysqlArchive.CreatePolicy(c.Request.Context(), body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_ARCHIVE_POLICY_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": "OK", "message": "created", "data": out})
}

func (s *Server) listMySQLArchivePolicies(c *gin.Context) {
	items, err := s.mysqlArchive.ListPolicies(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) precheckMySQLArchivePolicy(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	out, err := s.mysqlArchive.PrecheckPolicy(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_ARCHIVE_PRECHECK_FAILED", "message": err.Error()})
		return
	}
	ok(c, out)
}

func (s *Server) startMySQLArchivePolicy(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var body archiveStartRequest
	_ = c.ShouldBindJSON(&body)
	task, err := s.mysqlArchive.Start(c.Request.Context(), id, body.Confirmed)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_ARCHIVE_START_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}

func (s *Server) listMySQLArchiveJobs(c *gin.Context) {
	policyID := int64(0)
	if raw := c.Query("policy_id"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "invalid policy_id"})
			return
		}
		policyID = v
	}
	items, err := s.mysqlArchive.ListJobs(c.Request.Context(), policyID)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) pauseMySQLArchiveJob(c *gin.Context) {
	s.controlMySQLArchiveJob(c, "mysql.archive.pause")
}

func (s *Server) stopMySQLArchiveJob(c *gin.Context) {
	s.controlMySQLArchiveJob(c, "mysql.archive.stop")
}

func (s *Server) resumeMySQLArchiveJob(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	task, err := s.mysqlArchive.Resume(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_ARCHIVE_RESUME_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}

func (s *Server) controlMySQLArchiveJob(c *gin.Context, action string) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var body archiveStartRequest
	_ = c.ShouldBindJSON(&body)
	task, err := s.mysqlArchive.Control(c.Request.Context(), id, action, body.Confirmed)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_ARCHIVE_CONTROL_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}
