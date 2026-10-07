package httpapi

import (
	"github.com/gin-gonic/gin"
	"time"
)

func (s *Server) listProjects(c *gin.Context)     { s.listScopeNames(c, "projects") }
func (s *Server) listEnvironments(c *gin.Context) { s.listScopeNames(c, "environments") }
func (s *Server) listScopeNames(c *gin.Context, table string) {
	rows, err := s.platformBackup.Metadata.QueryContext(c.Request.Context(), "SELECT id,name FROM "+table+" ORDER BY id")
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			fail(c, err)
			return
		}
		out = append(out, gin.H{"id": id, "name": name})
	}
	ok(c, out)
}
func (s *Server) createProject(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(400, gin.H{"code": "INVALID_PARAMETER"})
		return
	}
	res, err := s.platformBackup.Metadata.ExecContext(c.Request.Context(), "INSERT INTO projects(name,created_at) VALUES(?,?)", req.Name, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		c.JSON(400, gin.H{"code": "PROJECT_REJECTED"})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"code": "OK", "data": gin.H{"id": id, "name": req.Name}})
}
func (s *Server) createEnvironment(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" || req.Code == "" {
		c.JSON(400, gin.H{"code": "INVALID_PARAMETER"})
		return
	}
	res, err := s.platformBackup.Metadata.ExecContext(c.Request.Context(), "INSERT INTO environments(name,code) VALUES(?,?)", req.Name, req.Code)
	if err != nil {
		c.JSON(400, gin.H{"code": "ENVIRONMENT_REJECTED"})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"code": "OK", "data": gin.H{"id": id, "name": req.Name}})
}
