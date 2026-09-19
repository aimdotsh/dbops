package httpapi

import (
	"net/http"

	oraclesvc "github.com/aimdotsh/dbops/internal/oracle"
	"github.com/gin-gonic/gin"
)

func (s *Server) onboardOracle(c *gin.Context) {
	var body oraclesvc.OnboardRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	instance, err := s.oracle.Onboard(c.Request.Context(), body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "ORACLE_ONBOARD_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": "OK", "message": "created", "data": instance})
}

func (s *Server) oracleStatus(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	out, err := s.oracle.Status(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "ORACLE_STATUS_FAILED", "message": err.Error()})
		return
	}
	ok(c, out)
}

func (s *Server) oracleTablespaces(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	out, err := s.oracle.Tablespaces(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "ORACLE_TABLESPACE_QUERY_FAILED", "message": err.Error()})
		return
	}
	ok(c, out)
}

func (s *Server) oracleDatafiles(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	out, err := s.oracle.Datafiles(c.Request.Context(), id, c.Query("tablespace"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "ORACLE_DATAFILE_QUERY_FAILED", "message": err.Error()})
		return
	}
	ok(c, out)
}

func (s *Server) addOracleDatafile(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var body oraclesvc.AddDatafileRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	task, err := s.oracle.CreateAddTask(c.Request.Context(), id, body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "ORACLE_DATAFILE_ADD_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}

func (s *Server) resizeOracleDatafile(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var body oraclesvc.ResizeDatafileRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	task, err := s.oracle.CreateResizeTask(c.Request.Context(), id, body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "ORACLE_DATAFILE_RESIZE_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}
