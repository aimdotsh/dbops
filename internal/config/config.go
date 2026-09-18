package config

import (
	"errors"
	"os"
	"path/filepath"

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
}

func Default() Config {
	var c Config
	c.Server.Listen = "0.0.0.0:8080"
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
	if cfg.Storage.MetadataDB == "" {
		cfg.Storage.MetadataDB = filepath.Join(cfg.Server.DataDir, "dbops.db")
	}
	if cfg.Storage.MetricsDB == "" {
		cfg.Storage.MetricsDB = filepath.Join(cfg.Server.DataDir, "metrics.db")
	}
	return cfg, nil
}
