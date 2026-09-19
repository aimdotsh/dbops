package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Listen    string `yaml:"listen"`
		PublicURL string `yaml:"public_url"`
		DataDir   string `yaml:"data_dir"`
	} `yaml:"server"`

	Storage struct {
		MetadataDB        string `yaml:"metadata_db"`
		MetricsDB         string `yaml:"metrics_db"`
		LogDir            string `yaml:"log_dir"`
		TaskWorkDir       string `yaml:"task_work_dir"`
		PlatformBackupDir string `yaml:"platform_backup_dir"`
		SoftwareDir       string `yaml:"software_dir"`
	} `yaml:"storage"`

	SQLite struct {
		BusyTimeoutMS        int `yaml:"busy_timeout_ms"`
		MetadataMaxOpenConns int `yaml:"metadata_max_open_conns"`
		MetricsMaxOpenConns  int `yaml:"metrics_max_open_conns"`
	} `yaml:"sqlite"`

	Task struct {
		Workers             int `yaml:"workers"`
		MaxPerHost          int `yaml:"max_per_host"`
		ScanIntervalSeconds int `yaml:"scan_interval_seconds"`
		LeaseSeconds        int `yaml:"lease_seconds"`
		LeaseRenewSeconds   int `yaml:"lease_renew_seconds"`
	} `yaml:"task"`

	Scheduler struct {
		Enabled             bool `yaml:"enabled"`
		ScanIntervalSeconds int  `yaml:"scan_interval_seconds"`
	} `yaml:"scheduler"`

	Alert struct {
		Enabled         bool `yaml:"enabled"`
		EvaluateSeconds int  `yaml:"evaluate_seconds"`
	} `yaml:"alert"`

	Security struct {
		MasterKeyEnv         string `yaml:"master_key_env"`
		PackageSigningKeyEnv string `yaml:"package_signing_key_env"`
		MasterKey            string `yaml:"-"`
		PackageSigningKey    string `yaml:"-"`
	} `yaml:"security"`

	Auth struct {
		Enabled                   bool   `yaml:"enabled"`
		JWTSecretEnv              string `yaml:"jwt_secret_env"`
		JWTSecret                 string `yaml:"-"`
		AccessMinutes             int    `yaml:"access_minutes"`
		RefreshHours              int    `yaml:"refresh_hours"`
		BootstrapAdminUsername    string `yaml:"bootstrap_admin_username"`
		BootstrapAdminPasswordEnv string `yaml:"bootstrap_admin_password_env"`
		BootstrapAdminPassword    string `yaml:"-"`
	} `yaml:"auth"`

	MySQLInstall struct {
		AllowProcessMode bool `yaml:"allow_process_mode"`
	} `yaml:"mysql_install"`

	AgentGateway struct {
		HeartbeatTimeoutSeconds   int    `yaml:"heartbeat_timeout_seconds"`
		WebsocketPath             string `yaml:"websocket_path"`
		BootstrapTokenEnv         string `yaml:"bootstrap_token_env"`
		AllowInsecureRegistration bool   `yaml:"allow_insecure_registration"`
		BootstrapToken            string `yaml:"-"`
	} `yaml:"agent_gateway"`
}

func Default() Config {
	var c Config
	c.Server.Listen = "0.0.0.0:8080"
	c.Server.PublicURL = "http://127.0.0.1:8080"
	c.Server.DataDir = "/data/dbops"
	c.Storage.MetadataDB = "/data/dbops/dbops.db"
	c.Storage.MetricsDB = "/data/dbops/metrics.db"
	c.Storage.LogDir = "/data/dbops/logs"
	c.Storage.TaskWorkDir = "/data/dbops/task-work"
	c.Storage.PlatformBackupDir = "/data/dbops/platform-backup"
	c.Storage.SoftwareDir = "/data/dbops/software"
	c.SQLite.BusyTimeoutMS = 5000
	c.SQLite.MetadataMaxOpenConns = 8
	c.SQLite.MetricsMaxOpenConns = 4
	c.Task.Workers = 4
	c.Task.MaxPerHost = 2
	c.Task.ScanIntervalSeconds = 2
	c.Task.LeaseSeconds = 60
	c.Task.LeaseRenewSeconds = 20
	c.Scheduler.Enabled = true
	c.Scheduler.ScanIntervalSeconds = 10
	c.Alert.Enabled = true
	c.Alert.EvaluateSeconds = 15
	c.Security.MasterKeyEnv = "DBOPS_MASTER_KEY"
	c.Security.PackageSigningKeyEnv = "DBOPS_PACKAGE_SIGNING_KEY"
	c.Auth.Enabled = true
	c.Auth.JWTSecretEnv = "DBOPS_JWT_SECRET"
	c.Auth.AccessMinutes = 15
	c.Auth.RefreshHours = 168
	c.Auth.BootstrapAdminUsername = "admin"
	c.Auth.BootstrapAdminPasswordEnv = "DBOPS_ADMIN_PASSWORD"
	c.AgentGateway.HeartbeatTimeoutSeconds = 90
	c.AgentGateway.WebsocketPath = "/api/v1/agent/ws"
	c.AgentGateway.BootstrapTokenEnv = "DBOPS_AGENT_BOOTSTRAP_TOKEN"
	return c
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = os.Getenv("DBOPS_CONFIG")
	}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, err
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return cfg, err
		}
	}

	if cfg.Server.DataDir == "" {
		return cfg, errors.New("server.data_dir is required")
	}
	if cfg.Server.PublicURL == "" {
		return cfg, errors.New("server.public_url is required")
	}
	cfg.Server.PublicURL = strings.TrimRight(cfg.Server.PublicURL, "/")

	if cfg.Storage.MetadataDB == "" {
		cfg.Storage.MetadataDB = filepath.Join(cfg.Server.DataDir, "dbops.db")
	}
	if cfg.Storage.MetricsDB == "" {
		cfg.Storage.MetricsDB = filepath.Join(cfg.Server.DataDir, "metrics.db")
	}
	if cfg.AgentGateway.HeartbeatTimeoutSeconds <= 0 {
		cfg.AgentGateway.HeartbeatTimeoutSeconds = 90
	}
	if cfg.AgentGateway.WebsocketPath == "" {
		cfg.AgentGateway.WebsocketPath = "/api/v1/agent/ws"
	}
	if cfg.AgentGateway.BootstrapTokenEnv == "" {
		cfg.AgentGateway.BootstrapTokenEnv = "DBOPS_AGENT_BOOTSTRAP_TOKEN"
	}
	cfg.AgentGateway.BootstrapToken = os.Getenv(cfg.AgentGateway.BootstrapTokenEnv)
	if cfg.AgentGateway.BootstrapToken == "" && !cfg.AgentGateway.AllowInsecureRegistration {
		return cfg, errors.New("agent bootstrap token is required; set the configured bootstrap token environment variable")
	}

	if cfg.Security.MasterKeyEnv == "" {
		cfg.Security.MasterKeyEnv = "DBOPS_MASTER_KEY"
	}
	if cfg.Security.PackageSigningKeyEnv == "" {
		cfg.Security.PackageSigningKeyEnv = "DBOPS_PACKAGE_SIGNING_KEY"
	}
	cfg.Security.MasterKey = os.Getenv(cfg.Security.MasterKeyEnv)
	if len(cfg.Security.MasterKey) < 16 {
		return cfg, errors.New("DBOps master key is required and must be at least 16 characters")
	}
	cfg.Security.PackageSigningKey = os.Getenv(cfg.Security.PackageSigningKeyEnv)
	if cfg.Security.PackageSigningKey == "" {
		cfg.Security.PackageSigningKey = cfg.Security.MasterKey
	}

	if cfg.Auth.Enabled {
		if cfg.Auth.JWTSecretEnv == "" {
			cfg.Auth.JWTSecretEnv = "DBOPS_JWT_SECRET"
		}
		if cfg.Auth.AccessMinutes <= 0 {
			cfg.Auth.AccessMinutes = 15
		}
		if cfg.Auth.RefreshHours <= 0 {
			cfg.Auth.RefreshHours = 168
		}
		if cfg.Auth.BootstrapAdminUsername == "" {
			cfg.Auth.BootstrapAdminUsername = "admin"
		}
		if cfg.Auth.BootstrapAdminPasswordEnv == "" {
			cfg.Auth.BootstrapAdminPasswordEnv = "DBOPS_ADMIN_PASSWORD"
		}
		cfg.Auth.JWTSecret = os.Getenv(cfg.Auth.JWTSecretEnv)
		if len(cfg.Auth.JWTSecret) < 32 {
			return cfg, errors.New("JWT secret is required and must be at least 32 characters when auth is enabled")
		}
		cfg.Auth.BootstrapAdminPassword = os.Getenv(cfg.Auth.BootstrapAdminPasswordEnv)
	}
	return cfg, nil
}
