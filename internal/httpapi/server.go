package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/aimdotsh/dbops/internal/alert"
	authsvc "github.com/aimdotsh/dbops/internal/auth"
	"github.com/aimdotsh/dbops/internal/domain"
	dorissvc "github.com/aimdotsh/dbops/internal/doris"
	metricstore "github.com/aimdotsh/dbops/internal/metrics"
	"github.com/aimdotsh/dbops/internal/mysqlarchive"
	"github.com/aimdotsh/dbops/internal/mysqlbackup"
	"github.com/aimdotsh/dbops/internal/mysqlinstall"
	"github.com/aimdotsh/dbops/internal/mysqlreplication"
	"github.com/aimdotsh/dbops/internal/mysqlservice"
	oraclesvc "github.com/aimdotsh/dbops/internal/oracle"
	pgsvc "github.com/aimdotsh/dbops/internal/postgres"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/aimdotsh/dbops/internal/software"
	"github.com/gin-gonic/gin"
)

type Server struct {
	http             *http.Server
	auth             *authsvc.Service
	hosts            repository.HostRepository
	agents           repository.AgentRepository
	dbs              repository.DatabaseRepository
	tasks            repository.TaskRepository
	metrics          *metricstore.Store
	alerts           *alert.Engine
	software         *software.Service
	mysqlInstaller   *mysqlinstall.Service
	mysqlBackup      *mysqlbackup.Service
	mysqlArchive     *mysqlarchive.Service
	mysqlReplication *mysqlreplication.Service
	mysqlService     *mysqlservice.Service
	oracle           *oraclesvc.Service
	postgres         *pgsvc.Service
	doris            *dorissvc.Service
}

func New(
	addr string,
	authService *authsvc.Service,
	hosts repository.HostRepository,
	agents repository.AgentRepository,
	dbs repository.DatabaseRepository,
	tasks repository.TaskRepository,
	metricsStore *metricstore.Store,
	alertEngine *alert.Engine,
	softwareService *software.Service,
	mysqlInstaller *mysqlinstall.Service,
	mysqlBackup *mysqlbackup.Service,
	mysqlArchive *mysqlarchive.Service,
	mysqlReplication *mysqlreplication.Service,
	mysqlService *mysqlservice.Service,
	oracleService *oraclesvc.Service,
	postgresService *pgsvc.Service,
	dorisService *dorissvc.Service,
	agentWS http.Handler,
	websocketPath string,
) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	s := &Server{
		auth: authService, hosts: hosts, agents: agents, dbs: dbs, tasks: tasks,
		metrics: metricsStore, alerts: alertEngine, software: softwareService,
		mysqlInstaller: mysqlInstaller, mysqlBackup: mysqlBackup, mysqlArchive: mysqlArchive,
		mysqlReplication: mysqlReplication, mysqlService: mysqlService, oracle: oracleService, postgres: postgresService, doris: dorisService,
	}

	v1 := r.Group("/api/v1")
	v1.GET("/health", s.health)
	v1.POST("/auth/login", s.login)
	v1.POST("/auth/refresh", s.refreshToken)
	v1.GET("/software/packages/:id/download", s.downloadSoftwarePackage)

	protected := v1.Group("")
	protected.Use(s.authenticate)
	protected.GET("/auth/me", s.me)

	readRoles := []string{
		authsvc.RoleSuperAdmin, authsvc.RoleDBA, authsvc.RoleOperator,
		authsvc.RoleViewer, authsvc.RoleAuditor,
	}
	read := s.requireRoles(readRoles...)
	adminDBA := s.requireRoles(authsvc.RoleSuperAdmin, authsvc.RoleDBA)
	ops := s.requireRoles(authsvc.RoleSuperAdmin, authsvc.RoleDBA, authsvc.RoleOperator)
	superAdmin := s.requireRoles(authsvc.RoleSuperAdmin)

	protected.GET("/users", superAdmin, s.listUsers)
	protected.POST("/users", superAdmin, s.createUser)

	protected.GET("/hosts", read, s.listHosts)
	protected.POST("/hosts", adminDBA, s.createHost)
	protected.GET("/hosts/:id", read, s.getHost)
	protected.GET("/agents", read, s.listAgents)
	protected.GET("/agents/:id", read, s.getAgent)
	protected.GET("/databases", read, s.listDatabases)
	protected.GET("/metrics/latest", read, s.getLatestMetric)
	protected.GET("/metrics/range", read, s.getMetricRange)
	protected.GET("/alerts", read, s.listAlerts)
	protected.POST("/alerts/:id/ack", ops, s.acknowledgeAlert)

	protected.GET("/software/packages", read, s.listSoftwarePackages)
	protected.POST("/software/packages", adminDBA, s.uploadSoftwarePackage)

	protected.POST("/mysql/install", adminDBA, s.createMySQLInstall)
	protected.GET("/mysql/backups", read, s.listMySQLBackups)
	protected.POST("/mysql/instances/:id/backups", ops, s.createMySQLBackup)

	protected.GET("/mysql/archive/policies", read, s.listMySQLArchivePolicies)
	protected.POST("/mysql/archive/policies", adminDBA, s.createMySQLArchivePolicy)
	protected.POST("/mysql/archive/policies/:id/precheck", adminDBA, s.precheckMySQLArchivePolicy)
	protected.POST("/mysql/archive/policies/:id/start", adminDBA, s.startMySQLArchivePolicy)
	protected.GET("/mysql/archive/jobs", read, s.listMySQLArchiveJobs)
	protected.POST("/mysql/archive/jobs/:id/pause", ops, s.pauseMySQLArchiveJob)
	protected.POST("/mysql/archive/jobs/:id/resume", ops, s.resumeMySQLArchiveJob)
	protected.POST("/mysql/archive/jobs/:id/stop", adminDBA, s.stopMySQLArchiveJob)

	protected.GET("/mysql/replications", read, s.listMySQLReplications)
	protected.POST("/mysql/replications", adminDBA, s.createMySQLReplication)
	protected.POST("/mysql/replications/:id/refresh", ops, s.refreshMySQLReplication)
	protected.POST("/mysql/instances/:id/start", ops, s.startMySQLInstance)
	protected.POST("/mysql/instances/:id/stop", ops, s.stopMySQLInstance)
	protected.POST("/mysql/instances/:id/restart", ops, s.restartMySQLInstance)

	protected.POST("/oracle/instances", adminDBA, s.onboardOracle)
	protected.GET("/oracle/instances/:id/status", read, s.oracleStatus)
	protected.GET("/oracle/instances/:id/dataguard", read, s.oracleDataGuardStatus)
	protected.GET("/oracle/instances/:id/tablespaces", read, s.oracleTablespaces)
	protected.GET("/oracle/instances/:id/datafiles", read, s.oracleDatafiles)
	protected.POST("/oracle/instances/:id/datafiles", adminDBA, s.addOracleDatafile)
	protected.POST("/oracle/instances/:id/datafiles/resize", adminDBA, s.resizeOracleDatafile)
	protected.POST("/oracle/instances/:id/backups/rman", ops, s.createOracleRMANBackup)

	protected.POST("/postgres/instances", adminDBA, s.onboardPostgres)
	protected.GET("/postgres/instances/:id/status", read, s.postgresStatus)
	protected.GET("/postgres/instances/:id/replication", read, s.postgresReplicationStatus)
	protected.GET("/postgres/backups", read, s.listPostgresBackups)
	protected.POST("/postgres/instances/:id/backups", ops, s.createPostgresBackup)

	protected.POST("/doris/instances", adminDBA, s.onboardDoris)
	protected.GET("/doris/instances/:id/status", read, s.dorisClusterStatus)
	protected.GET("/doris/backups", read, s.listDorisBackups)
	protected.POST("/doris/instances/:id/backups", ops, s.createDorisBackup)

	protected.GET("/tasks", read, s.listTasks)
	protected.POST("/tasks", superAdmin, s.createTask)
	protected.GET("/tasks/:id", read, s.getTask)
	protected.GET("/tasks/:id/steps", read, s.listTaskSteps)
	protected.GET("/tasks/:id/events", read, s.listTaskEvents)

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
