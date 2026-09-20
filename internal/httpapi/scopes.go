package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	authsvc "github.com/aimdotsh/dbops/internal/auth"
	"github.com/gin-gonic/gin"
)

func (s *Server) unrestricted(c *gin.Context) bool {
	if s.auth == nil || !s.auth.Enabled() {
		return true
	}
	claims, ok := currentClaims(c)
	return ok && authsvc.HasAnyRole(claims.Roles, authsvc.RoleSuperAdmin)
}
func (s *Server) hostAllowed(ctx context.Context, user, host int64) (bool, error) {
	var n int
	err := s.platformBackup.Metadata.QueryRowContext(ctx, `SELECT count(*) FROM hosts h JOIN user_resource_scopes s ON s.project_id=h.project_id AND s.environment_id=h.environment_id WHERE h.id=? AND s.user_id=?`, host, user).Scan(&n)
	return n > 0, err
}
func (s *Server) resourceAllowed(ctx context.Context, user int64, kind string, id int64) (bool, error) {
	db := s.platformBackup.Metadata
	if id <= 0 {
		return false, nil
	}
	if kind == "host" {
		return s.hostAllowed(ctx, user, id)
	}
	queries := map[string]string{
		"agent":    "SELECT host_id FROM agents WHERE id=?",
		"database": "SELECT host_id FROM database_instances WHERE id=?",
		"backup":   "SELECT d.host_id FROM backup_jobs b JOIN database_instances d ON d.id=b.database_instance_id WHERE b.id=?",
	}
	if query := queries[kind]; query != "" {
		var host sql.NullInt64
		if err := db.QueryRowContext(ctx, query, id).Scan(&host); err == sql.ErrNoRows {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return s.hostAllowed(ctx, user, host.Int64)
	}
	switch kind {
	case "replication":
		var a, b int64
		if err := db.QueryRowContext(ctx, "SELECT primary_instance_id,replica_instance_id FROM mysql_replications WHERE id=?", id).Scan(&a, &b); err != nil {
			return false, err
		}
		ok, err := s.resourceAllowed(ctx, user, "database", a)
		if !ok || err != nil {
			return ok, err
		}
		return s.resourceAllowed(ctx, user, "database", b)
	case "policy":
		var a int64
		var b sql.NullInt64
		if err := db.QueryRowContext(ctx, "SELECT source_instance_id,destination_instance_id FROM archive_policies WHERE id=?", id).Scan(&a, &b); err != nil {
			return false, err
		}
		ok, err := s.resourceAllowed(ctx, user, "database", a)
		if !ok || err != nil || !b.Valid {
			return ok, err
		}
		return s.resourceAllowed(ctx, user, "database", b.Int64)
	case "archive":
		var policy int64
		if err := db.QueryRowContext(ctx, "SELECT policy_id FROM archive_jobs WHERE id=?", id).Scan(&policy); err != nil {
			return false, err
		}
		return s.resourceAllowed(ctx, user, "policy", policy)
	case "alert":
		var typ string
		var target int64
		if err := db.QueryRowContext(ctx, "SELECT resource_type,resource_id FROM alert_events WHERE id=?", id).Scan(&typ, &target); err != nil {
			return false, err
		}
		if typ != "host" && typ != "database" {
			return false, nil
		}
		return s.resourceAllowed(ctx, user, typ, target)
	case "task":
		t, err := s.tasks.Get(ctx, id)
		if err != nil {
			return false, err
		}
		known := false
		if t.AgentID != nil {
			known = true
			ok, err := s.resourceAllowed(ctx, user, "agent", *t.AgentID)
			if !ok || err != nil {
				return ok, err
			}
		}
		if t.TargetID != nil && (t.TargetType == "host" || t.TargetType == "database") {
			known = true
			ok, err := s.resourceAllowed(ctx, user, t.TargetType, *t.TargetID)
			if !ok || err != nil {
				return ok, err
			}
		}
		var params map[string]any
		if err = json.Unmarshal([]byte(t.ParametersJSON), &params); err != nil {
			return false, err
		}
		ok, err := s.parameterScopes(ctx, user, params)
		return known && ok, err
	}
	return false, nil
}

var parameterKinds = map[string]string{"agent_id": "agent", "instance_id": "database", "primary_instance_id": "database", "replica_instance_id": "database", "source_instance_id": "database", "destination_instance_id": "database", "target_instance_id": "database", "backup_id": "backup", "policy_id": "policy", "archive_job_id": "archive", "backup_job_id": "backup"}

func (s *Server) parameterScopes(ctx context.Context, user int64, params map[string]any) (bool, error) {
	for key, kind := range parameterKinds {
		if value, exists := params[key]; exists && value != nil {
			n, ok := value.(float64)
			if !ok || n <= 0 || float64(int64(n)) != n {
				return false, nil
			}
			allowed, err := s.resourceAllowed(ctx, user, kind, int64(n))
			if !allowed || err != nil {
				return allowed, err
			}
		}
	}
	return true, nil
}
func (s *Server) scopeGuard(c *gin.Context) {
	c.Set("dbops.server", s)
	if s.unrestricted(c) {
		c.Next()
		return
	}
	claims, ok := currentClaims(c)
	if !ok {
		c.AbortWithStatus(401)
		return
	}
	deny := func() {
		c.AbortWithStatusJSON(403, gin.H{"code": "RESOURCE_FORBIDDEN", "message": "resource is outside the user's project and environment scopes"})
	}
	path := c.FullPath()
	kind := ""
	switch {
	case strings.HasPrefix(path, "/api/v1/hosts/"):
		kind = "host"
	case strings.HasPrefix(path, "/api/v1/agents/"):
		kind = "agent"
	case strings.Contains(path, "/instances/:id"):
		kind = "database"
	case strings.HasPrefix(path, "/api/v1/tasks/"):
		kind = "task"
	case strings.HasPrefix(path, "/api/v1/alerts/"):
		kind = "alert"
	case strings.Contains(path, "/replications/:id"):
		kind = "replication"
	case strings.Contains(path, "/archive/policies/:id"):
		kind = "policy"
	case strings.Contains(path, "/archive/jobs/:id"):
		kind = "archive"
	}
	if kind != "" {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			deny()
			return
		}
		allowed, err := s.resourceAllowed(c.Request.Context(), claims.UserID, kind, id)
		if err != nil || !allowed {
			deny()
			return
		}
	}
	if strings.HasPrefix(path, "/api/v1/metrics/") {
		typ := c.Query("resource_type")
		id, _ := strconv.ParseInt(c.Query("resource_id"), 10, 64)
		if typ != "host" && typ != "database" {
			deny()
			return
		}
		allowed, err := s.resourceAllowed(c.Request.Context(), claims.UserID, typ, id)
		if err != nil || !allowed {
			deny()
			return
		}
	}
	if c.Request.Method == "POST" && path != "/api/v1/software/packages" && c.Request.ContentLength != 0 {
		raw, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
		if err != nil || len(raw) > 1<<20 {
			c.AbortWithStatus(413)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		var params map[string]any
		if json.Unmarshal(raw, &params) != nil {
			c.AbortWithStatusJSON(400, gin.H{"code": "INVALID_PARAMETER"})
			return
		}
		for key := range params {
			if key != strings.ToLower(key) {
				deny()
				return
			}
		}
		allowed, err := s.parameterScopes(c.Request.Context(), claims.UserID, params)
		if err != nil || !allowed {
			deny()
			return
		}
		if path == "/api/v1/hosts" {
			project, pok := params["project_id"].(float64)
			env, eok := params["environment_id"].(float64)
			var n int
			if !pok || !eok {
				deny()
				return
			}
			err := s.platformBackup.Metadata.QueryRowContext(c.Request.Context(), "SELECT count(*) FROM user_resource_scopes WHERE user_id=? AND project_id=? AND environment_id=?", claims.UserID, project, env).Scan(&n)
			if err != nil || n == 0 {
				deny()
				return
			}
		}
	}
	c.Next()
}
func (s *Server) filterScoped(c *gin.Context, data any) (any, error) {
	if s.unrestricted(c) {
		return data, nil
	}
	kind := map[string]string{"/api/v1/hosts": "host", "/api/v1/agents": "agent", "/api/v1/databases": "database", "/api/v1/tasks": "task", "/api/v1/alerts": "alert", "/api/v1/mysql/backups": "backup", "/api/v1/postgres/backups": "backup", "/api/v1/doris/backups": "backup", "/api/v1/mysql/replications": "replication", "/api/v1/mysql/archive/policies": "policy", "/api/v1/mysql/archive/jobs": "archive"}[c.FullPath()]
	if kind == "" {
		return data, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	if err = json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	claims, _ := currentClaims(c)
	out := []map[string]any{}
	for _, item := range items {
		id, _ := item["id"].(float64)
		allowed, err := s.resourceAllowed(c.Request.Context(), claims.UserID, kind, int64(id))
		if err != nil {
			return nil, err
		}
		if allowed {
			out = append(out, item)
		}
	}
	return out, nil
}
func (s *Server) listScopes(c *gin.Context) {
	rows, err := s.platformBackup.Metadata.QueryContext(c.Request.Context(), "SELECT user_id,project_id,environment_id FROM user_resource_scopes ORDER BY user_id")
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var user, project, env int64
		if err = rows.Scan(&user, &project, &env); err != nil {
			fail(c, err)
			return
		}
		out = append(out, gin.H{"user_id": user, "project_id": project, "environment_id": env})
	}
	ok(c, out)
}
func (s *Server) setScope(c *gin.Context) {
	var req struct {
		UserID        int64 `json:"user_id"`
		HostID        int64 `json:"host_id"`
		ProjectID     int64 `json:"project_id"`
		EnvironmentID int64 `json:"environment_id"`
		Granted       bool  `json:"granted"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ProjectID <= 0 || req.EnvironmentID <= 0 || (req.UserID <= 0 && req.HostID <= 0) {
		c.JSON(400, gin.H{"code": "INVALID_PARAMETER"})
		return
	}
	tx, err := s.platformBackup.Metadata.BeginTx(c.Request.Context(), nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	// Numeric scope identifiers refer to projects/environments explicitly created by admins.
	if req.HostID > 0 {
		res, e := tx.ExecContext(c.Request.Context(), "UPDATE hosts SET project_id=?,environment_id=? WHERE id=?", req.ProjectID, req.EnvironmentID, req.HostID)
		err = e
		if err == nil {
			n, _ := res.RowsAffected()
			if n != 1 {
				err = errors.New("host not found")
			}
		}
	}
	if err == nil && req.UserID > 0 {
		if req.Granted {
			_, err = tx.ExecContext(c.Request.Context(), "INSERT OR IGNORE INTO user_resource_scopes(user_id,project_id,environment_id) VALUES(?,?,?)", req.UserID, req.ProjectID, req.EnvironmentID)
		} else {
			_, err = tx.ExecContext(c.Request.Context(), "DELETE FROM user_resource_scopes WHERE user_id=? AND project_id=? AND environment_id=?", req.UserID, req.ProjectID, req.EnvironmentID)
		}
	}
	if err != nil {
		c.JSON(400, gin.H{"code": "SCOPE_REJECTED", "message": err.Error()})
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	ok(c, req)
}
