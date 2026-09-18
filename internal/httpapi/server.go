package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/gin-gonic/gin"
)

type Server struct {
	http  *http.Server
	hosts repository.HostRepository
	dbs   repository.DatabaseRepository
	tasks repository.TaskRepository
}

func New(addr string, hosts repository.HostRepository, dbs repository.DatabaseRepository, tasks repository.TaskRepository) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	s := &Server{hosts: hosts, dbs: dbs, tasks: tasks}

	v1 := r.Group("/api/v1")
	v1.GET("/health", s.health)
	v1.GET("/hosts", s.listHosts)
	v1.POST("/hosts", s.createHost)
	v1.GET("/hosts/:id", s.getHost)
	v1.GET("/databases", s.listDatabases)
	v1.GET("/tasks", s.listTasks)
	v1.POST("/tasks", s.createTask)
	v1.GET("/tasks/:id", s.getTask)

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
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER"})
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
	out, err := s.tasks.Create(c.Request.Context(), in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": out})
}

func (s *Server) getTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER"})
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

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": "OK", "message": "success", "data": data})
}

func fail(c *gin.Context, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": err.Error()})
}
