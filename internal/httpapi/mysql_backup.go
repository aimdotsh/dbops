package httpapi

import (
	"net/http"
	"strconv"

	"github.com/aimdotsh/dbops/internal/mysqlbackup"
	"github.com/gin-gonic/gin"
)

type mysqlBackupRequest struct {
	Engine       string   `json:"engine,omitempty"`
	ToolPath     string   `json:"tool_path,omitempty"`
	AllDatabases bool     `json:"all_databases"`
	Databases    []string `json:"databases,omitempty"`
	OutputDir    string   `json:"output_dir,omitempty"`
	FileName     string   `json:"file_name,omitempty"`
}

func (s *Server) createMySQLBackup(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var body mysqlBackupRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	task, err := s.mysqlBackup.CreateTask(c.Request.Context(), mysqlbackup.CreateRequest{
		InstanceID: id, Engine: body.Engine, ToolPath: body.ToolPath, AllDatabases: body.AllDatabases, Databases: body.Databases,
		OutputDir: body.OutputDir, FileName: body.FileName,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_BACKUP_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}

func (s *Server) listMySQLBackups(c *gin.Context) {
	instanceID := int64(0)
	if raw := c.Query("instance_id"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "invalid instance_id"})
			return
		}
		instanceID = v
	}
	items, err := s.mysqlBackup.List(c.Request.Context(), instanceID)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) createMySQLRestore(c *gin.Context) {
	var req mysqlbackup.RestoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": "INVALID_PARAMETER"})
		return
	}
	t, err := s.mysqlBackup.CreateRestoreTask(c.Request.Context(), req)
	if err != nil {
		c.JSON(400, gin.H{"code": "MYSQL_RESTORE_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(202, gin.H{"code": "OK", "data": t})
}
