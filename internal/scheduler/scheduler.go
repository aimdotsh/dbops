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
	jobs     []func(context.Context) error
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
				for _, job := range s.jobs {
					if err := job(ctx); err != nil {
						s.logger.Error("scheduled maintenance failed", "error", err)
					}
				}
			}
		}
	}()
}

func (s *Scheduler) Add(job func(context.Context) error) { s.jobs = append(s.jobs, job) }
