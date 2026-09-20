package httpapi

import (
	"context"
	"encoding/json"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/mysqlinstall"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func (s *Server) auditMutation(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		c.Next()
		return
	}
	// Reserve an audit record before changing state, without logging request secrets.
	actor := int64(0)
	if claims, ok := currentClaims(c); ok {
		actor = claims.UserID
	}
	res, err := s.platformBackup.Metadata.ExecContext(c.Request.Context(), "INSERT INTO operation_audit(actor_id,method,path,status_code,created_at) VALUES(?,?,?,?,?)", actor, c.Request.Method, c.Request.URL.Path, 0, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		c.AbortWithStatusJSON(503, gin.H{"code": "AUDIT_UNAVAILABLE"})
		return
	}
	id, _ := res.LastInsertId()
	c.Next()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.platformBackup.Metadata.ExecContext(ctx, "UPDATE operation_audit SET status_code=? WHERE id=?", c.Writer.Status(), id)
}
func (s *Server) listAudit(c *gin.Context) {
	rows, err := s.platformBackup.Metadata.QueryContext(c.Request.Context(), "SELECT id,actor_id,method,path,status_code,created_at FROM operation_audit ORDER BY id DESC LIMIT 500")
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, actor int64
		var method, path, created string
		var status int
		if err = rows.Scan(&id, &actor, &method, &path, &status, &created); err != nil {
			fail(c, err)
			return
		}
		out = append(out, gin.H{"id": id, "actor_id": actor, "method": method, "path": path, "status_code": status, "created_at": created})
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	ok(c, out)
}
func (s *Server) createMySQLPrecheck(c *gin.Context) {
	var body struct {
		AgentID     int64  `json:"agent_id"`
		Port        int    `json:"port"`
		DataDir     string `json:"data_dir"`
		InstallRoot string `json:"install_root"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.AgentID <= 0 || body.Port < 1 || body.Port > 65535 {
		c.JSON(400, gin.H{"code": "INVALID_PARAMETER"})
		return
	}
	layout := mysqlinstall.InstallRequest{Port: body.Port, InstallRoot: body.InstallRoot, DataDir: body.DataDir}
	if err := mysqlinstall.NormalizeLayout(&layout); err != nil {
		c.JSON(400, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	raw, _ := json.Marshal(map[string]any{"action": "mysql.precheck", "timeout_seconds": 60, "params": map[string]any{"port": body.Port, "data_dir": layout.DataDir}})
	t, err := s.tasks.Create(c.Request.Context(), domain.Task{TaskType: "agent.action", AgentID: &body.AgentID, ParametersJSON: string(raw)})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(202, gin.H{"code": "OK", "data": t})
}
