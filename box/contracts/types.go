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

type SourceTransfer struct {
	URL          string    `json:"url"`
	ExpiresAt    time.Time `json:"expires_at"`
	SourceCommit string    `json:"source_commit"`
}

type ResultContract struct {
	Directory      string `json:"directory"`
	BundleFilename string `json:"bundle_filename"`
	ManifestSchema string `json:"manifest_schema"`
	MaxBundleBytes int64  `json:"max_bundle_bytes"`
}

type RunSpec struct {
	SchemaVersion       string                 `json:"schema_version"`
	ExperimentID        string                 `json:"experiment_id"`
	ProjectID           string                 `json:"project_id"`
	ExecutionEpoch      string                 `json:"execution_epoch"`
	SourceCommit        string                 `json:"source_commit"`
	SourceTransfer      SourceTransfer         `json:"source_transfer"`
	Entrypoint          string                 `json:"entrypoint"`
	Parameters          map[string]interface{} `json:"parameters"`
	Environment         map[string]string      `json:"environment"`
	Inputs              map[string]interface{} `json:"inputs"`
	Runtime             string                 `json:"runtime"`
	RuntimeVersion      string                 `json:"runtime_version"`
	Limits              ResourceLimits         `json:"limits"`
	ResultContract      ResultContract         `json:"result_contract"`
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
	BoxID           string                 `json:"box_id"`
	ExecutionEpoch  string                 `json:"execution_epoch"`
	Status          string                 `json:"status"`
	Attempt         int                    `json:"attempt"`
	CancelRequested bool                   `json:"cancel_requested"`
	RunSpec         map[string]interface{} `json:"run_spec"`
	ActualRuntime   string                 `json:"actual_runtime"`
	RuntimeVersion  string                 `json:"runtime_version"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

type LogEntry struct {
	Sequence   int64                  `json:"sequence"`
	Stream     string                 `json:"stream"`
	OccurredAt time.Time              `json:"occurred_at"`
	Message    string                 `json:"message"`
	Fields     map[string]interface{} `json:"fields,omitempty"`
}

type LogBatch struct {
	ExecutionEpoch string     `json:"execution_epoch"`
	FirstSequence  int64      `json:"first_sequence"`
	Entries        []LogEntry `json:"entries"`
	LogsTruncated  bool       `json:"logs_truncated"`
	TruncatedAt    *time.Time `json:"truncated_at,omitempty"`
}

type LogAcknowledgement struct {
	AcceptedThroughSequence int64 `json:"accepted_through_sequence"`
}

type Failure struct {
	Stage         string                 `json:"stage"`
	Code          string                 `json:"code"`
	Message       string                 `json:"message"`
	FailedAt      time.Time              `json:"failed_at"`
	BoxID         string                 `json:"box_id,omitempty"`
	Runtime       string                 `json:"runtime,omitempty"`
	Attempt       int                    `json:"attempt"`
	Retryable     bool                   `json:"retryable"`
	CleanupResult map[string]interface{} `json:"cleanup_result"`
}

type ResumeRequest struct {
	ExecutionEpoch        string   `json:"execution_epoch"`
	LocalPhase            string   `json:"local_phase"`
	LastLocalSequence     int64    `json:"last_local_sequence"`
	BundleState           string   `json:"bundle_state"`
	AcknowledgedCallbacks []string `json:"acknowledged_callbacks"`
}

type Resume struct {
	Action                  string `json:"action"`
	AcceptedPhase           string `json:"accepted_phase"`
	AcceptedThroughSequence int64  `json:"accepted_through_sequence"`
}

type Manifest struct {
	SchemaVersion string         `json:"schema_version"`
	ExperimentID  string         `json:"experiment_id"`
	SourceCommit  string         `json:"source_commit"`
	ResultDirectory string       `json:"result_directory"`
	Status        string         `json:"status"`
	StartedAt     time.Time      `json:"started_at"`
	FinishedAt    time.Time      `json:"finished_at"`
	Runtime       string         `json:"runtime"`
	RuntimeVersion string        `json:"runtime_version"`
	LogsTruncated bool           `json:"logs_truncated"`
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

type DeviceAuthorization struct {
	ClientKind              string    `json:"client_kind"`
	DeviceCode              string    `json:"device_code"`
	UserCode                string    `json:"user_code"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete"`
	ExpiresAt               time.Time `json:"expires_at"`
	Interval                int       `json:"interval"`
}

type BoxRegistrationGrant struct {
	RegistrationGrant string    `json:"registration_grant"`
	GrantExpiresAt    time.Time `json:"grant_expires_at"`
}
