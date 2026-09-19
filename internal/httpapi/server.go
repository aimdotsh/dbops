package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/mysqlinstall"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/aimdotsh/dbops/internal/software"
	"github.com/gin-gonic/gin"
)

type Server struct {
	http           *http.Server
	hosts          repository.HostRepository
	agents         repository.AgentRepository
	dbs            repository.DatabaseRepository
	tasks          repository.TaskRepository
	software       *software.Service
	mysqlInstaller *mysqlinstall.Service
}

func New(
	addr string,
	hosts repository.HostRepository,
	agents repository.AgentRepository,
	dbs repository.DatabaseRepository,
	tasks repository.TaskRepository,
	softwareService *software.Service,
	mysqlInstaller *mysqlinstall.Service,
	agentWS http.Handler,
	websocketPath string,
) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	s := &Server{hosts: hosts, agents: agents, dbs: dbs, tasks: tasks, software: softwareService, mysqlInstaller: mysqlInstaller}

	v1 := r.Group("/api/v1")
	v1.GET("/health", s.health)
	v1.GET("/hosts", s.listHosts)
	v1.POST("/hosts", s.createHost)
	v1.GET("/hosts/:id", s.getHost)
	v1.GET("/agents", s.listAgents)
	v1.GET("/agents/:id", s.getAgent)
	v1.GET("/databases", s.listDatabases)
	v1.GET("/software/packages", s.listSoftwarePackages)
	v1.POST("/software/packages", s.uploadSoftwarePackage)
	v1.GET("/software/packages/:id/download", s.downloadSoftwarePackage)
	v1.POST("/mysql/install", s.createMySQLInstall)
	v1.GET("/tasks", s.listTasks)
	v1.POST("/tasks", s.createTask)
	v1.GET("/tasks/:id", s.getTask)
	v1.GET("/tasks/:id/steps", s.listTaskSteps)
	v1.GET("/tasks/:id/events", s.listTaskEvents)

	if websocketPath == "" {
		websocketPath = "/api/v1/agent/ws"
	}
	r.GET(websocketPath, gin.WrapH(agentWS))

	s.http = &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

func (s *Server) Start() error {
	go func() {
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": "OK", "data": gin.H{"status": "up"}})
}

func (s *Server) listHosts(c *gin.Context) {
	items, err := s.hosts.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) createHost(c *gin.Context) {
	var in domain.Host
	if err := c.ShouldBindJSON(&in); err != nil || in.Hostname == "" || in.IPAddress == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "hostname and ip_address are required"})
		return
	}
	out, err := s.hosts.Create(c.Request.Context(), in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": "OK", "message": "success", "data": out})
}

func (s *Server) getHost(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	out, err := s.hosts.Get(c.Request.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND"})
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, out)
}

func (s *Server) listAgents(c *gin.Context) {
	items, err := s.agents.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) getAgent(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	out, err := s.agents.Get(c.Request.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND"})
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, out)
}

func (s *Server) listDatabases(c *gin.Context) {
	items, err := s.dbs.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) listTasks(c *gin.Context) {
	items, err := s.tasks.List(c.Request.Context(), 50)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) createTask(c *gin.Context) {
	var in domain.Task
	if err := c.ShouldBindJSON(&in); err != nil || in.TaskType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "task_type is required"})
		return
	}
	if in.TaskType == "agent.action" && in.AgentID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "agent_id is required for agent.action"})
		return
	}
	out, err := s.tasks.Create(c.Request.Context(), in)
	if err != nil {
		fail(c, err)
		return
	}
	_ = s.tasks.AddEvent(c.Request.Context(), domain.TaskEvent{
		TaskID:      out.ID,
		EventType:   "created",
		Level:       "INFO",
		Message:     "task created",
		PayloadJSON: "{}",
	})
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": out})
}

func (s *Server) getTask(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	out, err := s.tasks.Get(c.Request.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND"})
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, out)
}

func (s *Server) listTaskSteps(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	items, err := s.tasks.ListSteps(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) listTaskEvents(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	items, err := s.tasks.ListEvents(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER"})
		return 0, false
	}
	return id, true
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": "OK", "message": "success", "data": data})
}

func fail(c *gin.Context, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": err.Error()})
}
