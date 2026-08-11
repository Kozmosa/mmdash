// Package contracts contains the stable Core-to-Box wire and runtime types.
package contracts

import "time"

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

type RunSpec struct {
	SchemaVersion       string                 `json:"schema_version"`
	ExperimentID        string                 `json:"experiment_id"`
	ProjectID           string                 `json:"project_id"`
	SourceCommit        string                 `json:"source_commit"`
	Entrypoint          string                 `json:"entrypoint"`
	Parameters          map[string]interface{} `json:"parameters"`
	Environment         map[string]string      `json:"environment"`
	Inputs              map[string]interface{} `json:"inputs"`
	Runtime             string                 `json:"runtime"`
	Limits              ResourceLimits         `json:"limits"`
	ReadonlyCredentials []CredentialRef        `json:"readonly_credentials,omitempty"`
}

type CredentialRef struct {
	Name     string `json:"name"`
	ValueRef string `json:"value_ref"`
}

type Task struct {
	TaskID          string                 `json:"task_id"`
	ExperimentID    string                 `json:"experiment_id"`
	ProjectID       string                 `json:"project_id"`
	Status          string                 `json:"status"`
	Attempt         int                    `json:"attempt"`
	MaxAttempts     int                    `json:"max_attempts"`
	LeaseExpiresAt  *time.Time             `json:"lease_expires_at,omitempty"`
	CancelRequested bool                   `json:"cancel_requested"`
	RunSpec         map[string]interface{} `json:"run_spec"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

type Log struct {
	Level   string                 `json:"level"`
	Message string                 `json:"message"`
	Fields  map[string]interface{} `json:"fields,omitempty"`
}

type Manifest struct {
	SchemaVersion string         `json:"schema_version"`
	ExperimentID  string         `json:"experiment_id"`
	Status        string         `json:"status"`
	Summary       string         `json:"summary,omitempty"`
	ExitCode      *int           `json:"exit_code,omitempty"`
	Files         []ManifestFile `json:"files"`
}

type ManifestFile struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size_bytes"`
	Kind     string `json:"kind"`
	MIMEType string `json:"mime_type,omitempty"`
}

type ArtifactPointer struct {
	ArtifactID string `json:"artifact_id"`
	VersionID  string `json:"version_id"`
	Filename   string `json:"filename"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size_bytes"`
}
