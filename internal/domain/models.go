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
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	DBType       string `json:"db_type"`
	Version      string `json:"version,omitempty"`
	HostID       int64  `json:"host_id"`
	Port         int    `json:"port"`
	Role         string `json:"role,omitempty"`
	DataDir      string `json:"data_dir,omitempty"`
	ConfigPath   string `json:"config_path,omitempty"`
	CredentialID *int64 `json:"credential_id,omitempty"`
	Status       string `json:"status"`
	ManagedMode  string `json:"managed_mode"`
	MetadataJSON string `json:"metadata_json,omitempty"`
}

type SoftwarePackage struct {
	ID                int64     `json:"id"`
	SoftwareName      string    `json:"software_name"`
	Version           string    `json:"version"`
	OSFamily          string    `json:"os_family,omitempty"`
	Architecture      string    `json:"architecture,omitempty"`
	PackageType       string    `json:"package_type,omitempty"`
	FileName          string    `json:"file_name"`
	StoragePath       string    `json:"-"`
	DownloadURL       string    `json:"download_url,omitempty"`
	SHA256            string    `json:"sha256"`
	SizeBytes         int64     `json:"size_bytes"`
	CompatibilityJSON string    `json:"compatibility_json,omitempty"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}

type Credential struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	CredentialType  string    `json:"credential_type"`
	Username        string    `json:"username,omitempty"`
	EncryptedSecret string    `json:"-"`
	MetadataJSON    string    `json:"metadata_json,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ServerIDReservation struct {
	ID         int64     `json:"id"`
	ServerID   int       `json:"server_id"`
	HostID     int64     `json:"host_id"`
	Port       int       `json:"port"`
	TaskID     int64     `json:"task_id"`
	InstanceID *int64    `json:"instance_id,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
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
	IdempotencyKey *string    `json:"idempotency_key,omitempty"`
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

type MySQLReplication struct {
	ID                      int64      `json:"id"`
	PrimaryInstanceID       int64      `json:"primary_instance_id"`
	ReplicaInstanceID       int64      `json:"replica_instance_id"`
	ReplicationCredentialID *int64     `json:"replication_credential_id,omitempty"`
	GTIDEnabled             bool       `json:"gtid_enabled"`
	IOThreadStatus          string     `json:"io_thread_status,omitempty"`
	SQLThreadStatus         string     `json:"sql_thread_status,omitempty"`
	ReplicationLagSeconds   *int64     `json:"replication_lag_seconds,omitempty"`
	SourceUUID              string     `json:"source_uuid,omitempty"`
	LastIOError             string     `json:"last_io_error,omitempty"`
	LastSQLError            string     `json:"last_sql_error,omitempty"`
	LastCheckedAt           *time.Time `json:"last_checked_at,omitempty"`
	Status                  string     `json:"status"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type BackupJob struct {
	ID                 int64      `json:"id"`
	TaskID             int64      `json:"task_id"`
	DatabaseInstanceID int64      `json:"database_instance_id"`
	BackupEngine       string     `json:"backup_engine"`
	BackupType         string     `json:"backup_type"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	SizeBytes          int64      `json:"size_bytes"`
	Status             string     `json:"status"`
	StoragePath        string     `json:"storage_path,omitempty"`
	Checksum           string     `json:"checksum,omitempty"`
	MetadataJSON       string     `json:"metadata_json,omitempty"`
	ErrorMessage       string     `json:"error_message,omitempty"`
}

type ArchiveJob struct {
	ID                  int64      `json:"id"`
	TaskID              int64      `json:"task_id"`
	SourceInstanceID    int64      `json:"source_instance_id"`
	SourceDatabase      string     `json:"source_database"`
	SourceTable         string     `json:"source_table"`
	DestinationDatabase string     `json:"destination_database,omitempty"`
	DestinationTable    string     `json:"destination_table,omitempty"`
	Status              string     `json:"status"`
	ScannedRows         int64      `json:"scanned_rows"`
	ArchivedRows        int64      `json:"archived_rows"`
	DeletedRows         int64      `json:"deleted_rows"`
	FailedRows          int64      `json:"failed_rows"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
	VerificationStatus  string     `json:"verification_status,omitempty"`
	ErrorMessage        string     `json:"error_message,omitempty"`
}
