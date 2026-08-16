// Package boxcontrol owns account-level Box identities, Project assignments,
// outbound Gateway connectivity, and the Core-to-Box task protocol.
package boxcontrol

import "time"

const (
	StatusRegistering = "registering"
	StatusOnline      = "online"
	StatusOffline     = "offline"
	StatusDraining    = "draining"
	StatusRevoked     = "revoked"

	TaskQueued           = "queued"
	TaskPreparing        = "preparing"
	TaskRunning          = "running"
	TaskUploading        = "uploading"
	TaskProcessingResult = "processing_result"
	TaskSucceeded        = "succeeded"
	TaskFailed           = "failed"
	TaskCanceled         = "canceled"
	TaskTimedOut         = "timed_out"
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

type ProjectBinding struct {
	AssignedAt     time.Time  `json:"assigned_at"`
	AssignedBy     string     `json:"assigned_by"`
	BoxID          string     `json:"box_id"`
	ForceUnboundAt *time.Time `json:"force_unbound_at,omitempty"`
	ProjectID      string     `json:"project_id"`
}

type Box struct {
	ID                            string           `json:"box_id"`
	OwnerUserID                   string           `json:"owner_user_id"`
	Name                          string           `json:"name"`
	Status                        string           `json:"status"`
	Version                       string           `json:"version"`
	Capabilities                  []Capability     `json:"capabilities"`
	Runtimes                      []Runtime        `json:"runtimes"`
	Limits                        ResourceLimits   `json:"limits"`
	Load                          Load             `json:"load"`
	ProjectAssignments            []ProjectBinding `json:"project_assignments"`
	LastHeartbeatAt               *time.Time       `json:"last_heartbeat_at,omitempty"`
	OfflineSince                  *time.Time       `json:"offline_since,omitempty"`
	DrainRequestedAt              *time.Time       `json:"drain_requested_at,omitempty"`
	RevokedAt                     *time.Time       `json:"revoked_at,omitempty"`
	LegacyReauthorizationRequired bool             `json:"legacy_reauthorization_required"`
	CreatedAt                     time.Time        `json:"created_at"`
	UpdatedAt                     time.Time        `json:"updated_at"`
	InstallationID                string           `json:"-"`
	TokenID                       string           `json:"-"`
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

type Task struct {
	ID                        string                 `json:"task_id"`
	ExperimentID              string                 `json:"experiment_id"`
	ProjectID                 string                 `json:"project_id"`
	BoxID                     string                 `json:"box_id,omitempty"`
	ExecutionEpoch            string                 `json:"execution_epoch,omitempty"`
	Status                    string                 `json:"status"`
	Attempt                   int                    `json:"attempt"`
	MaxAttempts               int                    `json:"max_attempts"`
	CancelRequested           bool                   `json:"cancel_requested"`
	RunSpec                   map[string]interface{} `json:"run_spec"`
	ActualRuntime             string                 `json:"actual_runtime,omitempty"`
	RuntimeVersion            string                 `json:"runtime_version,omitempty"`
	LastLogSequence           int64                  `json:"last_log_sequence"`
	LogsTruncated             bool                   `json:"logs_truncated"`
	LogsTruncatedAt           *time.Time             `json:"logs_truncated_at,omitempty"`
	ExitCode                  *int                   `json:"exit_code,omitempty"`
	Failure                   *Failure               `json:"failure,omitempty"`
	ResourceUsage             map[string]interface{} `json:"resource_usage,omitempty"`
	Summary                   string                 `json:"summary,omitempty"`
	ExecutionBundleArtifactID string                 `json:"-"`
	ExecutionBundleVersionID  string                 `json:"-"`
	ResultManifestSHA256      string                 `json:"-"`
	CreatedAt                 time.Time              `json:"created_at"`
	StartedAt                 *time.Time             `json:"started_at,omitempty"`
	FinishedAt                *time.Time             `json:"finished_at,omitempty"`
	UpdatedAt                 time.Time              `json:"updated_at"`
}

type Log struct {
	ID               string                 `json:"log_id"`
	TaskID           string                 `json:"task_id"`
	ExperimentID     string                 `json:"experiment_id"`
	ExecutionEpoch   string                 `json:"-"`
	Sequence         int64                  `json:"sequence"`
	Stream           string                 `json:"stream"`
	Message          string                 `json:"message"`
	Fields           map[string]interface{} `json:"fields,omitempty"`
	OccurredAt       time.Time              `json:"occurred_at"`
	ReceivedAt       time.Time              `json:"received_at"`
	LateAfterFailure bool                   `json:"late_after_failure"`
}

type ArtifactPointer struct {
	ArtifactID string `json:"artifact_id"`
	VersionID  string `json:"version_id"`
	Filename   string `json:"filename"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"size_bytes"`
}

type Result struct {
	ExecutionEpoch  string          `json:"execution_epoch"`
	ManifestSHA256  string          `json:"manifest_sha256"`
	ExecutionBundle ArtifactPointer `json:"execution_bundle"`
}

type BoxRegistration struct {
	Box            Box        `json:"box"`
	Token          string     `json:"box_token"`
	TokenExpiresAt *time.Time `json:"token_expires_at,omitempty"`
}

type Heartbeat struct {
	Box           Box      `json:"box"`
	CancelTaskIDs []string `json:"cancel_task_ids"`
}

type Revocation struct {
	Box               Box    `json:"box"`
	Mode              string `json:"mode"`
	ActiveExperiments int    `json:"active_experiments"`
}

type ResumeRequest struct {
	ExecutionEpoch        string
	LocalPhase            string
	LastLocalSequence     int64
	BundleState           string
	AcknowledgedCallbacks []string
}

type Resume struct {
	Action                  string `json:"action"`
	AcceptedPhase           string `json:"accepted_phase"`
	AcceptedThroughSequence int64  `json:"accepted_through_sequence"`
}

type LogBatch struct {
	ExecutionEpoch string
	FirstSequence  int64
	Entries        []Log
	LogsTruncated  bool
	TruncatedAt    *time.Time
}

type LogAcknowledgement struct {
	AcceptedThroughSequence int64 `json:"accepted_through_sequence"`
}
