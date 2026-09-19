package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aimdotsh/dbops/internal/security"
	"github.com/google/uuid"
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
	owner := fmt.Sprintf("%s-worker-%d", uuid.NewString(), index)
	ticker := time.NewTicker(e.scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := e.repo.RecoverExpired(ctx); err != nil {
				e.logger.Error("recover expired tasks", "error", err)
				continue
			}
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

func (e *Engine) execute(parent context.Context, t domain.Task) {
	ctx, cancel := context.WithTimeout(parent, 24*time.Hour)
	defer cancel()
	owner := ""
	if t.LeaseOwner != nil {
		owner = *t.LeaseOwner
	}
	stopped := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Duration(e.leaseSeconds) * time.Second / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := e.repo.RenewLease(ctx, t.ID, owner, e.leaseSeconds); err != nil {
					e.logger.Error("renew task lease", "task_id", t.ID, "error", err)
					cancel()
					return
				}
			}
		}
	}()
	result, err := e.invoke(ctx, t)
	close(stopped)
	<-done
	status, message, raw := "success", "", "{}"
	if err != nil {
		status, message = "failed", err.Error()
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status, message = "timeout", "task deadline exceeded"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		status, message = "interrupted", "execution interrupted; verify agent state before retry"
	}
	if err == nil && ctx.Err() == nil {
		b, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			status, message = "failed", marshalErr.Error()
		} else {
			raw = security.RedactJSON(string(b))
		}
	}
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if err := e.repo.FinishOwned(finishCtx, t.ID, owner, status, raw, message); err != nil {
		e.logger.Error("persist task completion", "task_id", t.ID, "error", err)
	}
}

func (e *Engine) invoke(ctx context.Context, t domain.Task) (result any, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("task handler panic: %v", v)
		}
	}()
	e.mu.RLock()
	h := e.handlers[t.TaskType]
	e.mu.RUnlock()
	if h == nil {
		return nil, errors.New("no handler registered")
	}
	return h(ctx, t)
}
