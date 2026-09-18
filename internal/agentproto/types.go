package agentproto

import "encoding/json"

const ProtocolVersion = "1.0"

type Envelope struct {
	Type string `json:"type"`
	Data json.RawMessage `json:"data"`
}

type Hello struct {
	AgentUUID       string `json:"agent_uuid"`
	Version         string `json:"version"`
	Hostname        string `json:"hostname"`
	IPAddress       string `json:"ip_address,omitempty"`
	Architecture    string `json:"architecture,omitempty"`
	ProtocolVersion string `json:"protocol_version"`
}

type Heartbeat struct {
	AgentUUID    string   `json:"agent_uuid"`
	RunningTasks int      `json:"running_tasks"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type ActionRequest struct {
	RequestID       string         `json:"request_id"`
	TaskID          int64          `json:"task_id"`
	Action          string         `json:"action"`
	ProtocolVersion string         `json:"protocol_version"`
	TimeoutSeconds  int            `json:"timeout_seconds"`
	Params          map[string]any `json:"params,omitempty"`
}

type Step struct {
	Code string `json:"code,omitempty"`
	Name string `json:"name,omitempty"`
}

type ActionResponse struct {
	RequestID string `json:"request_id"`
	TaskID    int64  `json:"task_id"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	Step      Step   `json:"step,omitempty"`
	Message   string `json:"message,omitempty"`
	Result    any    `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

type ActionSpec struct {
	Name    string   `yaml:"name"`
	Timeout int      `yaml:"timeout"`
	Risk    string   `yaml:"risk"`
	Params  []string `yaml:"params"`
}

type ActionFile struct {
	Version int          `yaml:"version"`
	Actions []ActionSpec `yaml:"actions"`
}
