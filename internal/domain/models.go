package domain

import "time"

type Host struct {
	ID          int64     `json:"id"`
	Hostname    string    `json:"hostname"`
	IPAddress   string    `json:"ip_address"`
	Status      string    `json:"status"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Agent struct {
	ID              int64      `json:"id"`
	AgentUUID       string     `json:"agent_uuid"`
	HostID          *int64     `json:"host_id,omitempty"`
	Version         string     `json:"version,omitempty"`
	Architecture    string     `json:"architecture,omitempty"`
	Status          string     `json:"status"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	Capabilities    []string   `json:"capabilities,omitempty"`
}

type DatabaseInstance struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DBType      string `json:"db_type"`
	Version     string `json:"version,omitempty"`
	HostID      int64  `json:"host_id"`
	Port        int    `json:"port"`
	Role        string `json:"role,omitempty"`
	Status      string `json:"status"`
	ManagedMode string `json:"managed_mode"`
}

type Task struct {
	ID             int64      `json:"id"`
	TaskNo         string     `json:"task_no"`
	TaskType       string     `json:"task_type"`
	TargetType     string     `json:"target_type,omitempty"`
	TargetID       *int64     `json:"target_id,omitempty"`
	Status         string     `json:"status"`
	Progress       int        `json:"progress"`
	ParametersJSON string     `json:"parameters_json"`
	ResultJSON     string     `json:"result_json"`
	AgentID        *int64     `json:"agent_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	QueuedAt       *time.Time `json:"queued_at,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	LeaseOwner     *string    `json:"lease_owner,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	ErrorCode      *string    `json:"error_code,omitempty"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
}

type TaskStep struct {
	ID             int64      `json:"id"`
	TaskID         int64      `json:"task_id"`
	StepNo         int        `json:"step_no"`
	StepCode       string     `json:"step_code"`
	StepName       string     `json:"step_name,omitempty"`
	Status         string     `json:"status"`
	Progress       int        `json:"progress"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	OutputJSON     string     `json:"output_json,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	RecoveryPolicy string     `json:"recovery_policy,omitempty"`
}

type TaskEvent struct {
	ID          int64     `json:"id"`
	TaskID      int64     `json:"task_id"`
	EventTime   time.Time `json:"event_time"`
	EventType   string    `json:"event_type"`
	StepCode    string    `json:"step_code,omitempty"`
	Level       string    `json:"level"`
	Message     string    `json:"message,omitempty"`
	PayloadJSON string    `json:"payload_json"`
}
