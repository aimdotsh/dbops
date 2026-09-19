package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/aimdotsh/dbops/internal/agentclient"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "/etc/dbops-agent/agent.yaml", "path to agent config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := agentclient.LoadConfig(configPath)
	if err != nil {
		slog.Error("load agent config", "error", err)
		os.Exit(1)
	}
	agentID, err := agentclient.ResolveAgentID(cfg)
	if err != nil {
		slog.Error("resolve agent id", "error", err)
		os.Exit(1)
	}
	executor, err := agentclient.LoadExecutor(cfg.Executor.AllowedActionsFile)
	if err != nil {
		slog.Error("load action allowlist", "error", err)
		os.Exit(1)
	}
	executor.SetWorkDir(cfg.Agent.WorkDir)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := agentclient.New(cfg, agentID, executor, logger)
	if err := client.Run(ctx); err != nil && ctx.Err() == nil {
		slog.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}
