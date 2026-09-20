package httpapi

import (
	"github.com/gin-gonic/gin"
	"time"
)

func (s *Server) resolveInterruptedTask(c *gin.Context) {
	id, valid := parseID(c)
	if !valid {
		return
	}
	var req struct {
		Confirmed bool `json:"confirmed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !req.Confirmed {
		c.JSON(400, gin.H{"code": "CONFIRMATION_REQUIRED", "message": "inspect agent execution and database state before releasing an interrupted task"})
		return
	}
	res, err := s.platformBackup.Metadata.ExecContext(c.Request.Context(), "UPDATE tasks SET status='cancelled',finished_at=?,error_message='manually reconciled; no automatic retry' WHERE id=? AND status='interrupted'", time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		fail(c, err)
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		c.JSON(409, gin.H{"code": "TASK_NOT_INTERRUPTED"})
		return
	}
	ok(c, gin.H{"id": id, "status": "cancelled"})
}
