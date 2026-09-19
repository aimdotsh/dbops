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
	GetByHostID(context.Context, int64) (domain.Agent, error)
	List(context.Context) ([]domain.Agent, error)
	BindHostByIdentity(context.Context, string, string, string) (*int64, error)
	GetCredentialHash(context.Context, string) (string, error)
	SetCredentialHash(context.Context, string, string) error
}

type DatabaseRepository interface {
	List(context.Context) ([]domain.DatabaseInstance, error)
	Get(context.Context, int64) (domain.DatabaseInstance, error)
	CreateInstalled(context.Context, domain.DatabaseInstance) (domain.DatabaseInstance, error)
	UpdateStatus(context.Context, int64, string) error
}

type SoftwarePackageRepository interface {
	Create(context.Context, domain.SoftwarePackage) (domain.SoftwarePackage, error)
	Get(context.Context, int64) (domain.SoftwarePackage, error)
	List(context.Context) ([]domain.SoftwarePackage, error)
}

type CredentialRepository interface {
	Create(context.Context, domain.Credential) (domain.Credential, error)
	Get(context.Context, int64) (domain.Credential, error)
	Delete(context.Context, int64) error
}

type ServerIDRepository interface {
	Reserve(context.Context, int64, int64, int) (domain.ServerIDReservation, error)
	BindInstance(context.Context, int64, int64) error
	MarkFailed(context.Context, int64) error
}

type MySQLReplicationRepository interface {
	Create(context.Context, domain.MySQLReplication) (domain.MySQLReplication, error)
	Get(context.Context, int64) (domain.MySQLReplication, error)
	List(context.Context) ([]domain.MySQLReplication, error)
	UpdateStatus(context.Context, int64, domain.MySQLReplication) error
}

type ArchivePolicyRepository interface {
	Create(context.Context, domain.ArchivePolicy) (domain.ArchivePolicy, error)
	Get(context.Context, int64) (domain.ArchivePolicy, error)
	List(context.Context) ([]domain.ArchivePolicy, error)
}

type ArchiveJobRepository interface {
	Create(context.Context, domain.ArchiveJob) (domain.ArchiveJob, error)
	Get(context.Context, int64) (domain.ArchiveJob, error)
	List(context.Context, int64) ([]domain.ArchiveJob, error)
	AttachTask(context.Context, int64, int64) error
	UpdateState(context.Context, int64, string, int64, int64, int64, int64, int64, string, string, string) error
}

type BackupJobRepository interface {
	Create(context.Context, domain.BackupJob) (domain.BackupJob, error)
	Get(context.Context, int64) (domain.BackupJob, error)
	List(context.Context, int64) ([]domain.BackupJob, error)
	MarkRunning(context.Context, int64) error
	MarkSuccess(context.Context, int64, int64, string, string, string) error
	MarkFailed(context.Context, int64, string) error
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
