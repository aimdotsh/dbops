package app

import (
	"context"
	"log/slog"
	"os"

	"github.com/aimdotsh/dbops/internal/agentgateway"
	"github.com/aimdotsh/dbops/internal/alert"
	"github.com/aimdotsh/dbops/internal/config"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/httpapi"
	"github.com/aimdotsh/dbops/internal/mysqlinstall"
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

	hostRepo := reposqlite.HostRepo{DB: stores.Metadata}
	agentRepo := reposqlite.AgentRepo{DB: stores.Metadata}
	dbRepo := reposqlite.DatabaseRepo{DB: stores.Metadata}
	taskRepo := reposqlite.TaskRepo{DB: stores.Metadata}
	packageRepo := reposqlite.SoftwarePackageRepo{DB: stores.Metadata}
	credentialRepo := reposqlite.CredentialRepo{DB: stores.Metadata}
	serverIDRepo := reposqlite.ServerIDRepo{DB: stores.Metadata}

	gateway := agentgateway.New(
		agentRepo,
		taskRepo,
		logger,
		cfg.AgentGateway.HeartbeatTimeoutSeconds,
		cfg.AgentGateway.BootstrapToken,
		cfg.AgentGateway.AllowInsecureRegistration,
	)

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

	taskEngine := task.New(
		taskRepo,
		logger,
		cfg.Task.Workers,
		cfg.Task.LeaseSeconds,
		cfg.Task.ScanIntervalSeconds,
	)
	taskEngine.Register("system.echo", func(ctx context.Context, t domain.Task) (any, error) {
		return map[string]any{"echo": t.ParametersJSON}, nil
	})
	taskEngine.Register("agent.action", task.AgentActionHandler(gateway, taskRepo))
	taskEngine.Register("mysql.install", mysqlInstaller.Handler())

	return &App{
		cfg:    cfg,
		logger: logger,
		stores: stores,
		http: httpapi.New(
			cfg.Server.Listen,
			hostRepo,
			agentRepo,
			dbRepo,
			taskRepo,
			softwareService,
			mysqlInstaller,
			gateway,
			cfg.AgentGateway.WebsocketPath,
		),
		tasks:     taskEngine,
		gateway:   gateway,
		scheduler: scheduler.New(logger, cfg.Scheduler.Enabled, cfg.Scheduler.ScanIntervalSeconds),
		alert:     alert.New(logger, cfg.Alert.Enabled, cfg.Alert.EvaluateSeconds),
	}, nil
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
	return a.http.Shutdown(ctx)
}

func (a *App) Close() error {
	return a.stores.Close()
}
