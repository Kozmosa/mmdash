// Package experiment owns frozen experiment specifications, lifecycle state,
// result pointers, and the human/API read surface.
package experiment

import "time"

const (
	StatusCreated   = "created"
	StatusQueued    = "queued"
	StatusPreparing = "preparing"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
	StatusArchived  = "archived"
)

type ResourceLimits struct {
	CPUMillis     int64  `json:"cpu_millis"`
	MemoryBytes   int64  `json:"memory_bytes"`
	TimeoutSecond int    `json:"timeout_seconds"`
	DiskBytes     int64  `json:"disk_bytes"`
	PIDs          int    `json:"pids"`
	Network       string `json:"network"`
}

type ResultBundle struct {
	Manifest map[string]interface{} `json:"manifest"`
	Artifact map[string]interface{} `json:"artifact"`
	Summary  string                 `json:"summary,omitempty"`
	Analysis string                 `json:"analysis,omitempty"`
}

type Experiment struct {
	ID             string                 `json:"experiment_id"`
	ProjectID      string                 `json:"project_id"`
	Name           string                 `json:"name"`
	Status         string                 `json:"status"`
	CreatedBy      string                 `json:"created_by"`
	SourceCommit   string                 `json:"source_commit"`
	Entrypoint     string                 `json:"entrypoint"`
	Parameters     map[string]interface{} `json:"parameters"`
	Environment    map[string]string      `json:"environment"`
	Inputs         map[string]interface{} `json:"inputs"`
	Runtime        string                 `json:"runtime"`
	Limits         ResourceLimits         `json:"limits"`
	IdempotencyKey string                 `json:"-"`
	MaxAttempts    int                    `json:"-"`
	BoxID          string                 `json:"box_id,omitempty"`
	TaskID         string                 `json:"task_id,omitempty"`
	ExitCode       *int                   `json:"exit_code,omitempty"`
	FailureCode    string                 `json:"failure_code,omitempty"`
	FailureMessage string                 `json:"failure_message,omitempty"`
	ResourceUsage  map[string]interface{} `json:"resource_usage,omitempty"`
	Summary        string                 `json:"summary,omitempty"`
	Result         *ResultBundle          `json:"result,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	StartedAt      *time.Time             `json:"started_at,omitempty"`
	FinishedAt     *time.Time             `json:"finished_at,omitempty"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

type Page struct {
	Items      []Experiment `json:"items"`
	HasMore    bool         `json:"has_more"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type Comparison struct {
	Items []Experiment `json:"items"`
}
