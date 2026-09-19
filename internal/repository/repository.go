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

type AgentRepository interface {
	Upsert(context.Context, domain.Agent) (domain.Agent, error)
	Heartbeat(context.Context, string, []string) error
	MarkOffline(context.Context, string) error
	Get(context.Context, int64) (domain.Agent, error)
	GetByUUID(context.Context, string) (domain.Agent, error)
	List(context.Context) ([]domain.Agent, error)
	BindHostByIdentity(context.Context, string, string, string) (*int64, error)
	GetCredentialHash(context.Context, string) (string, error)
	SetCredentialHash(context.Context, string, string) error
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
	UpdateProgress(context.Context, int64, int) error
	RecoverExpired(context.Context) (int64, error)
	AddEvent(context.Context, domain.TaskEvent) error
	ListEvents(context.Context, int64) ([]domain.TaskEvent, error)
	UpsertStep(context.Context, domain.TaskStep) error
	ListSteps(context.Context, int64) ([]domain.TaskStep, error)
}

type LockRepository interface {
	Acquire(context.Context, string, int64, int) (bool, error)
	Release(context.Context, string, int64) error
}
