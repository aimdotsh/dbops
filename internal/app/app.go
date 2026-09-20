package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"time"

	"github.com/aimdotsh/dbops/internal/agentgateway"
	"github.com/aimdotsh/dbops/internal/alert"
	authsvc "github.com/aimdotsh/dbops/internal/auth"
	"github.com/aimdotsh/dbops/internal/config"
	"github.com/aimdotsh/dbops/internal/domain"
	dorissvc "github.com/aimdotsh/dbops/internal/doris"
	"github.com/aimdotsh/dbops/internal/httpapi"
	metricstore "github.com/aimdotsh/dbops/internal/metrics"
	"github.com/aimdotsh/dbops/internal/monitor"
	"github.com/aimdotsh/dbops/internal/mysqlarchive"
	"github.com/aimdotsh/dbops/internal/mysqlbackup"
	"github.com/aimdotsh/dbops/internal/mysqlinstall"
	"github.com/aimdotsh/dbops/internal/mysqlreplication"
	"github.com/aimdotsh/dbops/internal/mysqlservice"
	oraclesvc "github.com/aimdotsh/dbops/internal/oracle"
	"github.com/aimdotsh/dbops/internal/platformbackup"
	pgsvc "github.com/aimdotsh/dbops/internal/postgres"
	reposqlite "github.com/aimdotsh/dbops/internal/repository/sqlite"
	"github.com/aimdotsh/dbops/internal/scheduler"
	"github.com/aimdotsh/dbops/internal/security"
	"github.com/aimdotsh/dbops/internal/software"
	"github.com/aimdotsh/dbops/internal/storage"
	"github.com/aimdotsh/dbops/internal/task"
)

type App struct {
	cfg       config.Config
	logger    *slog.Logger
	stores    *storage.Stores
	http      *httpapi.Server
	tasks     *task.Engine
	gateway   *agentgateway.Gateway
	scheduler *scheduler.Scheduler
	alert     *alert.Engine
	cancel    context.CancelFunc
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	for _, dir := range []string{
		cfg.Server.DataDir,
		cfg.Storage.LogDir,
		cfg.Storage.TaskWorkDir,
		cfg.Storage.PlatformBackupDir,
		cfg.Storage.SoftwareDir,
	} {
		if dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}

	stores, err := storage.Open(
		cfg.Storage.MetadataDB,
		cfg.Storage.MetricsDB,
		cfg.SQLite.MetadataMaxOpenConns,
		cfg.SQLite.MetricsMaxOpenConns,
	)
	if err != nil {
		return nil, err
	}

	if err := storage.Migrate(stores.Metadata, stores.Metrics); err != nil {
		stores.Close()
		return nil, err
	}

	cipher, err := security.NewCipher(cfg.Security.MasterKey)
	if err != nil {
		stores.Close()
		return nil, err
	}

	authRepo := reposqlite.AuthRepo{DB: stores.Metadata}
	authService, err := authsvc.New(authRepo, authsvc.Config{
		Enabled:                cfg.Auth.Enabled,
		JWTSecret:              cfg.Auth.JWTSecret,
		AccessTTL:              time.Duration(cfg.Auth.AccessMinutes) * time.Minute,
		RefreshTTL:             time.Duration(cfg.Auth.RefreshHours) * time.Hour,
		BootstrapAdminUsername: cfg.Auth.BootstrapAdminUsername,
		BootstrapAdminPassword: cfg.Auth.BootstrapAdminPassword,
	})
	if err != nil {
		stores.Close()
		return nil, err
	}
	if err := authService.Bootstrap(context.Background()); err != nil {
		stores.Close()
		return nil, err
	}

	hostRepo := reposqlite.HostRepo{DB: stores.Metadata}
	agentRepo := reposqlite.AgentRepo{DB: stores.Metadata}
	dbRepo := reposqlite.DatabaseRepo{DB: stores.Metadata}
	taskRepo := reposqlite.TaskRepo{DB: stores.Metadata}
	packageRepo := reposqlite.SoftwarePackageRepo{DB: stores.Metadata}
	credentialRepo := reposqlite.CredentialRepo{DB: stores.Metadata}
	serverIDRepo := reposqlite.ServerIDRepo{DB: stores.Metadata}
	replicationRepo := reposqlite.MySQLReplicationRepo{DB: stores.Metadata}
	backupRepo := reposqlite.BackupJobRepo{DB: stores.Metadata}
	archivePolicyRepo := reposqlite.ArchivePolicyRepo{DB: stores.Metadata}
	archiveRepo := reposqlite.ArchiveJobRepo{DB: stores.Metadata}
	metricsStore := metricstore.NewStore(stores.Metrics)
	alertEngine := alert.New(logger, cfg.Alert.Enabled, cfg.Alert.EvaluateSeconds, stores.Metadata, metricsStore)
	cfg.Notifications.WebhookToken = os.Getenv(cfg.Notifications.WebhookTokenEnv)
	cfg.Notifications.SMTPPassword = os.Getenv(cfg.Notifications.SMTPPasswordEnv)
	alertEngine.ConfigureNotifications(cfg.Notifications)

	gateway := agentgateway.New(
		agentRepo,
		taskRepo,
		metricsStore,
		logger,
		cfg.AgentGateway.HeartbeatTimeoutSeconds,
		cfg.AgentGateway.BootstrapToken,
		cfg.AgentGateway.AllowInsecureRegistration,
	)

	gateway.RequireMTLS(cfg.AgentGateway.RequireMTLS)

	softwareService := software.New(
		packageRepo,
		cfg.Storage.SoftwareDir,
		cfg.Server.PublicURL,
		cfg.Security.PackageSigningKey,
	)

	mysqlInstaller := mysqlinstall.New(
		agentRepo,
		dbRepo,
		packageRepo,
		credentialRepo,
		serverIDRepo,
		taskRepo,
		softwareService,
		cipher,
		gateway,
		cfg.MySQLInstall.AllowProcessMode,
	)

	mysqlBackup := mysqlbackup.New(agentRepo, dbRepo, credentialRepo, backupRepo, taskRepo, cipher, gateway)
	mysqlBackup.SetInstaller(mysqlInstaller)
	mysqlArchive := mysqlarchive.New(agentRepo, dbRepo, credentialRepo, archivePolicyRepo, archiveRepo, taskRepo, cipher, gateway)
	mysqlArchive.SetReplications(replicationRepo)

	mysqlReplication := mysqlreplication.New(
		hostRepo,
		agentRepo,
		dbRepo,
		credentialRepo,
		replicationRepo,
		taskRepo,
		cipher,
		gateway,
	)

	mysqlService := mysqlservice.New(agentRepo, dbRepo, taskRepo, gateway)
	oracleService := oraclesvc.New(agentRepo, dbRepo, credentialRepo, backupRepo, taskRepo, cipher, gateway)
	postgresService := pgsvc.New(agentRepo, dbRepo, credentialRepo, backupRepo, taskRepo, cipher, gateway)
	dorisService := dorissvc.New(agentRepo, dbRepo, credentialRepo, backupRepo, taskRepo, cipher, gateway)

	taskEngine := task.New(
		taskRepo,
		logger,
		cfg.Task.Workers,
		cfg.Task.LeaseSeconds,
		cfg.Task.ScanIntervalSeconds,
	)
	platformBackup := &platformbackup.Service{Metadata: stores.Metadata, Metrics: stores.Metrics, Directory: cfg.Storage.PlatformBackupDir, Tasks: taskRepo}
	taskEngine.Register("platform.backup", platformBackup.Handler())
	taskEngine.Register("system.echo", func(ctx context.Context, t domain.Task) (any, error) {
		return map[string]any{"echo": t.ParametersJSON}, nil
	})
	taskEngine.Register("agent.action", task.AgentActionHandler(gateway, taskRepo))
	taskEngine.Register("mysql.install", mysqlInstaller.Handler())
	taskEngine.Register("mysql.backup", mysqlBackup.Handler())
	taskEngine.Register("mysql.restore", mysqlBackup.RestoreHandler())
	taskEngine.Register("mysql.restore_new", mysqlBackup.NewHostHandler())
	taskEngine.Register("mysql.archive.run", mysqlArchive.RunHandler())
	taskEngine.Register("mysql.archive.control", mysqlArchive.ControlHandler())
	taskEngine.Register("mysql.replication.create", mysqlReplication.Handler())
	taskEngine.Register("mysql.service", mysqlService.Handler())
	taskEngine.Register("oracle.datafile.add", oracleService.AddHandler())
	taskEngine.Register("oracle.datafile.resize", oracleService.ResizeHandler())
	taskEngine.Register("oracle.rman.backup", oracleService.RMANBackupHandler())
	taskEngine.Register("postgres.backup", postgresService.BackupHandler())
	taskEngine.Register("doris.backup", dorisService.BackupHandler())

	policies := &scheduler.Policies{DB: stores.Metadata, Handlers: map[string]func(context.Context, json.RawMessage) (domain.Task, error){
		"platform.backup": func(ctx context.Context, raw json.RawMessage) (domain.Task, error) {
			return platformBackup.CreateTask(ctx)
		},
		"mysql.backup": func(ctx context.Context, raw json.RawMessage) (domain.Task, error) {
			var req mysqlbackup.CreateRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				return domain.Task{}, err
			}
			return mysqlBackup.CreateTask(ctx, req)
		},
		"postgres.backup": func(ctx context.Context, raw json.RawMessage) (domain.Task, error) {
			var req struct {
				InstanceID int64 `json:"instance_id"`
				pgsvc.BackupRequest
			}
			if err := json.Unmarshal(raw, &req); err != nil {
				return domain.Task{}, err
			}
			return postgresService.CreateBackupTask(ctx, req.InstanceID, req.BackupRequest)
		},
		"oracle.rman.backup": func(ctx context.Context, raw json.RawMessage) (domain.Task, error) {
			var req struct {
				InstanceID int64 `json:"instance_id"`
				oraclesvc.RMANBackupRequest
			}
			if err := json.Unmarshal(raw, &req); err != nil {
				return domain.Task{}, err
			}
			return oracleService.CreateRMANBackupTask(ctx, req.InstanceID, req.RMANBackupRequest)
		},
		"doris.backup": func(ctx context.Context, raw json.RawMessage) (domain.Task, error) {
			var req struct {
				InstanceID int64 `json:"instance_id"`
				dorissvc.BackupRequest
			}
			if err := json.Unmarshal(raw, &req); err != nil {
				return domain.Task{}, err
			}
			return dorisService.CreateBackupTask(ctx, req.InstanceID, req.BackupRequest)
		},
	}}
	maintenance := scheduler.New(logger, cfg.Scheduler.Enabled, cfg.Scheduler.ScanIntervalSeconds)
	databaseMonitor := &monitor.Monitor{Databases: dbRepo, Store: metricsStore, Collectors: map[string]monitor.Collector{
		"mysql":      mysqlBackup.Metrics,
		"oracle":     func(ctx context.Context, id int64) (any, error) { return oracleService.Status(ctx, id) },
		"postgres":   func(ctx context.Context, id int64) (any, error) { return postgresService.Status(ctx, id) },
		"postgresql": func(ctx context.Context, id int64) (any, error) { return postgresService.Status(ctx, id) },
		"doris":      func(ctx context.Context, id int64) (any, error) { return dorisService.ClusterStatus(ctx, id) },
	}}
	maintenance.Add(databaseMonitor.Tick)
	maintenance.Add(policies.Tick)
	maintenance.Add(alertEngine.DeliverPending)
	var lastRetention time.Time
	maintenance.Add(func(ctx context.Context) error {
		if time.Since(lastRetention) < time.Hour {
			return nil
		}
		if err := metricsStore.Retain(ctx, time.Now(), cfg.MetricsRetention.Days5m, cfg.MetricsRetention.Days1h, cfg.MetricsRetention.Days1d); err != nil {
			return err
		}
		lastRetention = time.Now()
		return nil
	})
	a := &App{
		cfg:    cfg,
		logger: logger,
		stores: stores,
		http: httpapi.New(
			cfg.Server.Listen,
			platformBackup,
			policies,
			authService,
			hostRepo,
			agentRepo,
			dbRepo,
			taskRepo,
			metricsStore,
			alertEngine,
			softwareService,
			mysqlInstaller,
			mysqlBackup,
			mysqlArchive,
			mysqlReplication,
			mysqlService,
			oracleService,
			postgresService,
			dorisService,
			gateway,
			cfg.AgentGateway.WebsocketPath,
		),
		tasks:     taskEngine,
		gateway:   gateway,
		scheduler: maintenance,
		alert:     alertEngine,
	}
	if err := a.http.ConfigureTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile, cfg.Server.AgentCAFile); err != nil {
		stores.Close()
		return nil, err
	}
	return a, nil
}

func (a *App) Start(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel

	a.gateway.Start(ctx)
	if err := a.tasks.Start(ctx); err != nil {
		return err
	}
	a.scheduler.Start(ctx)
	a.alert.Start(ctx)
	if err := a.http.Start(); err != nil {
		return err
	}

	a.logger.Info("dbops server started", "listen", a.cfg.Server.Listen)
	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	err := a.http.Shutdown(ctx)
	a.tasks.Wait()
	return err
}

func (a *App) Close() error {
	return a.stores.Close()
}
