package adapter

import "context"

type InstanceStatus struct {
	Up      bool
	Message string
	Meta    map[string]string
}

type Metric struct {
	Name  string
	Value float64
}

type DatabaseAdapter interface {
	TestConnection(context.Context) error
	Status(context.Context) (InstanceStatus, error)
	CollectMetrics(context.Context) ([]Metric, error)
}

type Registry struct{ items map[string]DatabaseAdapter }

func NewRegistry() *Registry { return &Registry{items: map[string]DatabaseAdapter{}} }

func (r *Registry) Register(dbType string, a DatabaseAdapter) { r.items[dbType] = a }

func (r *Registry) Get(dbType string) (DatabaseAdapter, bool) {
	a, ok := r.items[dbType]
	return a, ok
}
