package scheduler

import (
	"context"
	"log/slog"
	"time"
)

type Scheduler struct {
	logger   *slog.Logger
	enabled  bool
	interval time.Duration
}

func New(logger *slog.Logger, enabled bool, seconds int) *Scheduler {
	if seconds <= 0 {
		seconds = 10
	}
	return &Scheduler{logger: logger, enabled: enabled, interval: time.Duration(seconds) * time.Second}
}

func (s *Scheduler) Start(ctx context.Context) {
	if !s.enabled {
		return
	}
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.logger.Debug("scheduler tick")
			}
		}
	}()
}
