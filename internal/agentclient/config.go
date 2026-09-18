package agentclient

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		URL string `yaml:"url"`
	} `yaml:"server"`
	Agent struct {
		ID               string `yaml:"id"`
		HeartbeatSeconds int    `yaml:"heartbeat_seconds"`
		WorkDir          string `yaml:"work_dir"`
		LogDir           string `yaml:"log_dir"`
	} `yaml:"agent"`
	Security struct {
		BootstrapTokenFile string `yaml:"bootstrap_token_file"`
		VerifyServerTLS    bool   `yaml:"verify_server_tls"`
	} `yaml:"security"`
	Executor struct {
		MaxConcurrentTasks int    `yaml:"max_concurrent_tasks"`
		AllowedActionsFile string `yaml:"allowed_actions_file"`
	} `yaml:"executor"`
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	cfg.Agent.ID = "auto"
	cfg.Agent.HeartbeatSeconds = 30
	cfg.Agent.WorkDir = "/var/lib/dbops-agent"
	cfg.Agent.LogDir = "/var/log/dbops-agent"
	cfg.Security.VerifyServerTLS = true
	cfg.Executor.MaxConcurrentTasks = 2
	cfg.Executor.AllowedActionsFile = "/etc/dbops-agent/actions.yaml"

	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Server.URL == "" {
		return cfg, fmt.Errorf("server.url is required")
	}
	if cfg.Agent.HeartbeatSeconds <= 0 {
		cfg.Agent.HeartbeatSeconds = 30
	}
	if cfg.Executor.MaxConcurrentTasks <= 0 {
		cfg.Executor.MaxConcurrentTasks = 2
	}
	return cfg, nil
}

func ResolveAgentID(cfg Config) (string, error) {
	if cfg.Agent.ID != "" && cfg.Agent.ID != "auto" {
		return cfg.Agent.ID, nil
	}
	if err := os.MkdirAll(cfg.Agent.WorkDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(cfg.Agent.WorkDir, "agent.id")
	if b, err := os.ReadFile(path); err == nil {
		if id := string(bytesTrimSpace(b)); id != "" {
			return id, nil
		}
	}
	id := uuid.NewString()
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func bytesTrimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\n' || b[start] == '\r' || b[start] == '\t') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\n' || b[end-1] == '\r' || b[end-1] == '\t') {
		end--
	}
	return b[start:end]
}
