package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
)

type Handler func(context.Context, domain.Task) (any, error)

type Engine struct {
	repo         repository.TaskRepository
	logger       *slog.Logger
	workers      int
	leaseSeconds int
	scanInterval time.Duration
	mu           sync.RWMutex
	handlers     map[string]Handler
	wg           sync.WaitGroup
}

func New(repo repository.TaskRepository, logger *slog.Logger, workers, leaseSeconds, scanSeconds int) *Engine {
	if workers <= 0 {
		workers = 4
	}
	if leaseSeconds <= 0 {
		leaseSeconds = 60
	}
	if scanSeconds <= 0 {
		scanSeconds = 2
	}
	return &Engine{
		repo: repo, logger: logger, workers: workers, leaseSeconds: leaseSeconds,
		scanInterval: time.Duration(scanSeconds) * time.Second, handlers: map[string]Handler{},
	}
}

func (e *Engine) Register(taskType string, h Handler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[taskType] = h
}

func (e *Engine) Start(ctx context.Context) error {
	n, err := e.repo.RecoverExpired(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		e.logger.Warn("recovered expired tasks", "count", n)
	}
	for i := 0; i < e.workers; i++ {
		e.wg.Add(1)
		go e.worker(ctx, i)
	}
	return nil
}

func (e *Engine) Wait() { e.wg.Wait() }

func (e *Engine) worker(ctx context.Context, index int) {
	defer e.wg.Done()
	owner := fmt.Sprintf("worker-%d", index)
	ticker := time.NewTicker(e.scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t, err := e.repo.ClaimNext(ctx, owner, e.leaseSeconds)
			if err != nil {
				e.logger.Error("claim task", "error", err)
				continue
			}
			if t != nil {
				e.execute(ctx, *t)
			}
		}
	}
}

func (e *Engine) execute(ctx context.Context, t domain.Task) {
	e.mu.RLock()
	h := e.handlers[t.TaskType]
	e.mu.RUnlock()
	if h == nil {
		_ = e.repo.UpdateStatus(ctx, t.ID, "failed", t.Progress, "{}", "no handler registered")
		return
	}
	result, err := h(ctx, t)
	if err != nil {
		_ = e.repo.UpdateStatus(ctx, t.ID, "failed", t.Progress, "{}", err.Error())
		return
	}
	b, _ := json.Marshal(result)
	_ = e.repo.UpdateStatus(ctx, t.ID, "success", 100, string(b), "")
}
