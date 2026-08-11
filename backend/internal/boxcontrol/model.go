// Package boxcontrol owns Box identities, capabilities, bindings, leases, and
// the Core-to-Box task boundary.
package boxcontrol

import "time"

const (
	StatusRegistering = "registering"
	StatusOnline      = "online"
	StatusOffline     = "offline"
	StatusRevoked     = "revoked"

	TaskQueued    = "queued"
	TaskPreparing = "preparing"
	TaskRunning   = "running"
	TaskSucceeded = "succeeded"
	TaskFailed    = "failed"
	TaskCanceled  = "canceled"
	TaskTimedOut  = "timed_out"
)

type ResourceLimits struct {
	CPUMillis     int64  `json:"cpu_millis"`
	MemoryBytes   int64  `json:"memory_bytes"`
	TimeoutSecond int    `json:"timeout_seconds"`
	DiskBytes     int64  `json:"disk_bytes"`
	PIDs          int    `json:"pids"`
	Network       string `json:"network"`
}

type Capability struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Features []string `json:"features,omitempty"`
}

type Runtime struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Image   string `json:"image,omitempty"`
}

type Load struct {
	RunningTasks int   `json:"running_tasks"`
	Capacity     int   `json:"capacity"`
	CPUMillis    int64 `json:"cpu_millis"`
	MemoryBytes  int64 `json:"memory_bytes"`
}

type Box struct {
	ID              string         `json:"box_id"`
	ProjectID       string         `json:"project_id"`
	Name            string         `json:"name"`
	Status          string         `json:"status"`
	Version         string         `json:"version"`
	Capabilities    []Capability   `json:"capabilities"`
	Runtimes        []Runtime      `json:"runtimes"`
	Limits          ResourceLimits `json:"limits"`
	Load            Load           `json:"load"`
	TokenID         string         `json:"-"`
	CreatedBy       string         `json:"-"`
	LastHeartbeatAt *time.Time     `json:"last_heartbeat_at,omitempty"`
	DisconnectedAt  *time.Time     `json:"disconnected_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type Task struct {
	ID              string                 `json:"task_id"`
	ExperimentID    string                 `json:"experiment_id"`
	ProjectID       string                 `json:"project_id"`
	BoxID           string                 `json:"box_id,omitempty"`
	Status          string                 `json:"status"`
	Attempt         int                    `json:"attempt"`
	MaxAttempts     int                    `json:"max_attempts"`
	LeaseExpiresAt  *time.Time             `json:"lease_expires_at,omitempty"`
	CancelRequested bool                   `json:"cancel_requested"`
	RunSpec         map[string]interface{} `json:"run_spec"`
	ExitCode        *int                   `json:"exit_code,omitempty"`
	ErrorCode       string                 `json:"error_code,omitempty"`
	ErrorMessage    string                 `json:"error_message,omitempty"`
	ResourceUsage   map[string]interface{} `json:"resource_usage,omitempty"`
	Summary         string                 `json:"summary,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	StartedAt       *time.Time             `json:"started_at,omitempty"`
	FinishedAt      *time.Time             `json:"finished_at,omitempty"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

type Log struct {
	ID           string                 `json:"log_id"`
	TaskID       string                 `json:"task_id"`
	ExperimentID string                 `json:"experiment_id"`
	Level        string                 `json:"level"`
	Message      string                 `json:"message"`
	Fields       map[string]interface{} `json:"fields"`
	OccurredAt   time.Time              `json:"occurred_at"`
}

type Result struct {
	Manifest map[string]interface{} `json:"manifest"`
	Artifact map[string]interface{} `json:"artifact"`
}

type BoxRegistration struct {
	Box   Box    `json:"box"`
	Token string `json:"token"`
}

type Heartbeat struct {
	Box           Box      `json:"box"`
	CancelTaskIDs []string `json:"cancel_task_ids"`
}

type TaskLease struct {
	TaskID          string    `json:"task_id"`
	LeaseExpiresAt  time.Time `json:"lease_expires_at"`
	CancelRequested bool      `json:"cancel_requested"`
}
