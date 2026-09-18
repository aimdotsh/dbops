package alert

import (
	"context"
	"log/slog"
	"time"
)

type Engine struct {
	logger   *slog.Logger
	enabled  bool
	interval time.Duration
}

func New(logger *slog.Logger, enabled bool, seconds int) *Engine {
	if seconds <= 0 {
		seconds = 15
	}
	return &Engine{logger: logger, enabled: enabled, interval: time.Duration(seconds) * time.Second}
}

func (e *Engine) Start(ctx context.Context) {
	if !e.enabled {
		return
	}
	go func() {
		ticker := time.NewTicker(e.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.logger.Debug("alert evaluation tick")
			}
		}
	}()
}
