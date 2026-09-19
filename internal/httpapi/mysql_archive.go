package httpapi

import (
	"net/http"
	"strconv"

	"github.com/aimdotsh/dbops/internal/mysqlarchive"
	"github.com/gin-gonic/gin"
)

type mysqlArchiveRequest struct {
	SourceDatabase      string `json:"source_database"`
	SourceTable         string `json:"source_table"`
	DestinationDatabase string `json:"destination_database,omitempty"`
	DestinationTable    string `json:"destination_table,omitempty"`
	Where               string `json:"where"`
	PTArchiverPath      string `json:"pt_archiver_path"`
	BatchSize           int    `json:"batch_size,omitempty"`
	TxnSize             int    `json:"txn_size,omitempty"`
	SleepMS             int    `json:"sleep_ms,omitempty"`
	DeleteSource        bool   `json:"delete_source"`
	Confirmed           bool   `json:"confirmed"`
}

func (s *Server) precheckMySQLArchive(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var body mysqlArchiveRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	out, err := s.mysqlArchive.Precheck(c.Request.Context(), mysqlarchive.Request{
		InstanceID:          id,
		SourceDatabase:      body.SourceDatabase,
		SourceTable:         body.SourceTable,
		DestinationDatabase: body.DestinationDatabase,
		DestinationTable:    body.DestinationTable,
		Where:               body.Where,
		PTArchiverPath:      body.PTArchiverPath,
		BatchSize:           body.BatchSize,
		TxnSize:             body.TxnSize,
		SleepMS:             body.SleepMS,
		DeleteSource:        body.DeleteSource,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_ARCHIVE_PRECHECK_FAILED", "message": err.Error()})
		return
	}
	ok(c, out)
}

func (s *Server) createMySQLArchive(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var body mysqlArchiveRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	task, err := s.mysqlArchive.CreateTask(c.Request.Context(), mysqlarchive.Request{
		InstanceID:          id,
		SourceDatabase:      body.SourceDatabase,
		SourceTable:         body.SourceTable,
		DestinationDatabase: body.DestinationDatabase,
		DestinationTable:    body.DestinationTable,
		Where:               body.Where,
		PTArchiverPath:      body.PTArchiverPath,
		BatchSize:           body.BatchSize,
		TxnSize:             body.TxnSize,
		SleepMS:             body.SleepMS,
		DeleteSource:        body.DeleteSource,
		Confirmed:           body.Confirmed,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_ARCHIVE_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}

func (s *Server) listMySQLArchiveJobs(c *gin.Context) {
	instanceID := int64(0)
	if raw := c.Query("instance_id"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "invalid instance_id"})
			return
		}
		instanceID = v
	}
	items, err := s.mysqlArchive.List(c.Request.Context(), instanceID)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}
