package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aimdotsh/dbops/internal/app"
	"github.com/aimdotsh/dbops/internal/config"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to server config")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, err := app.New(cfg, logger)
	if err != nil {
		slog.Error("initialize app", "error", err)
		os.Exit(1)
	}
	defer a.Close()

	if err := a.Start(ctx); err != nil {
		slog.Error("start app", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "error", err)
	}
}
