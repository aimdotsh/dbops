package repository

import (
	"context"

	"github.com/aimdotsh/dbops/internal/domain"
)

type HostRepository interface {
	List(context.Context) ([]domain.Host, error)
	Create(context.Context, domain.Host) (domain.Host, error)
	Get(context.Context, int64) (domain.Host, error)
}

type DatabaseRepository interface {
	List(context.Context) ([]domain.DatabaseInstance, error)
}

type TaskRepository interface {
	Create(context.Context, domain.Task) (domain.Task, error)
	Get(context.Context, int64) (domain.Task, error)
	List(context.Context, int) ([]domain.Task, error)
	ClaimNext(context.Context, string, int) (*domain.Task, error)
	UpdateStatus(context.Context, int64, string, int, string, string) error
	RecoverExpired(context.Context) (int64, error)
}

type LockRepository interface {
	Acquire(context.Context, string, int64, int) (bool, error)
	Release(context.Context, string, int64) error
}
