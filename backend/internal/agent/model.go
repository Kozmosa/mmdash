// Package agent owns Runtime-independent Agent instances, Project grants,
// SessionRecord/RunRecord indexes, prompts, and product token orchestration.
package agent

import "time"

// A Hermes approval request is bounded by the instance request timeout (at
// most five minutes). A longer lease lets a new Core process recover a claim
// abandoned by a crash without allowing two live requests to reach Hermes.
const runApprovalClaimLease = 10 * time.Minute

const (
	AdapterHermes = "hermes"

	ManagementManual = "manual"
	ManagementAuto   = "auto"

	InstanceSetupPending = "setup_pending"
	InstanceConfiguring  = "configuring"
	InstanceActive       = "active"
	InstanceDegraded     = "degraded"
	InstanceDisabled     = "disabled"

	SessionMain       = "main"
	SessionProgress   = "progress"
	SessionExperiment = "experiment"

	SessionActive = "active"
	SessionEnded  = "ended"

	RunRecordQueued             = "queued"
	RunRecordRunning            = "running"
	RunRecordWaitingForApproval = "waiting_for_approval"
	RunRecordStopping           = "stopping"
	RunRecordCompleted          = "completed"
	RunRecordFailed             = "failed"
	RunRecordStopped            = "stopped"
)

var DefaultAllowedTools = []string{
	"project.get",
	"data.list",
	"data.read",
	"context.promote",
}

// CheckSnapshot deliberately stores only normalized status and safe codes.
type CheckSnapshot struct {
	CheckedAt time.Time `json:"checked_at,omitempty"`
	Code      string    `json:"code,omitempty"`
	Status    string    `json:"status,omitempty"`
}

type Instance struct {
	AdapterType           string                 `json:"adapter_type"`
	Capabilities          map[string]interface{} `json:"capabilities"`
	CreatedAt             time.Time              `json:"created_at"`
	CreatedBy             string                 `json:"created_by"`
	DashboardURL          string                 `json:"dashboard_url,omitempty"`
	DisabledAt            *time.Time             `json:"disabled_at,omitempty"`
	DisplayName           string                 `json:"display_name"`
	Grant                 *ProjectGrant          `json:"grant,omitempty"`
	ID                    string                 `json:"agent_instance_id"`
	ManagementCheck       CheckSnapshot          `json:"management_check"`
	ManagementMode        string                 `json:"management_mode"`
	ManagementPath        string                 `json:"management_path"`
	Profile               string                 `json:"profile,omitempty"`
	ProjectAccessCheck    CheckSnapshot          `json:"project_access_check"`
	RequestTimeoutSeconds int                    `json:"request_timeout_seconds"`
	RuntimeCheck          CheckSnapshot          `json:"runtime_check"`
	RuntimeURL            string                 `json:"runtime_url"`
	SecretStatus          SecretStatus           `json:"secret_status"`
	Status                string                 `json:"status"`
	UpdatedAt             time.Time              `json:"updated_at"`
	Version               int64                  `json:"version"`
}

type ProjectGrant struct {
	AgentInstanceID  string     `json:"agent_instance_id"`
	AllowedTools     []string   `json:"allowed_tools"`
	CreatedAt        time.Time  `json:"created_at"`
	CreatedBy        string     `json:"created_by"`
	DefaultSessionID string     `json:"default_session_id,omitempty"`
	GrantID          string     `json:"grant_id"`
	LastAccessAt     *time.Time `json:"last_access_at,omitempty"`
	ProjectID        string     `json:"project_id"`
	RemoteAccessID   string     `json:"-"`
	Role             string     `json:"role"`
	Status           string     `json:"status"`
	UpdatedAt        time.Time  `json:"updated_at"`
	Version          int64      `json:"version"`
}

type SecretStatus struct {
	CloudflareClientIDConfigured     bool `json:"cloudflare_client_id_configured"`
	CloudflareClientSecretConfigured bool `json:"cloudflare_client_secret_configured"`
	DashboardTokenConfigured         bool `json:"dashboard_token_configured"`
	HermesAPIKeyConfigured           bool `json:"hermes_api_key_configured"`
}

type SessionRecord struct {
	AgentInstanceID string     `json:"agent_instance_id"`
	CreatedAt       time.Time  `json:"created_at"`
	CreatedBy       string     `json:"created_by"`
	EndReason       string     `json:"end_reason,omitempty"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	GrantID         string     `json:"grant_id"`
	ID              string     `json:"session_id"`
	LastMessageAt   *time.Time `json:"last_message_at,omitempty"`
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	ParentSessionID string     `json:"parent_session_id,omitempty"`
	ProjectID       string     `json:"project_id"`
	RemoteSessionID string     `json:"remote_session_id"`
	SessionType     string     `json:"session_type"`
	Status          string     `json:"status"`
	Title           string     `json:"title"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Version         int64      `json:"version"`
}

type RunRecord struct {
	CompletedAt        *time.Time       `json:"completed_at,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
	CreatedBy          string           `json:"created_by"`
	ID                 string           `json:"run_id"`
	PendingApprovalIDs []string         `json:"-"`
	RemoteRunID        string           `json:"remote_run_id"`
	SafeErrorCode      string           `json:"safe_error_code,omitempty"`
	SessionID          string           `json:"session_id"`
	Source             string           `json:"source"`
	SourceRunID        string           `json:"source_run_id,omitempty"`
	StartedAt          *time.Time       `json:"started_at,omitempty"`
	Status             string           `json:"status"`
	ToolCalls          []ToolCallRecord `json:"tool_calls"`
	UpdatedAt          time.Time        `json:"updated_at"`
	Version            int64            `json:"version"`
}

type ToolCallRecord struct {
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	ID               string     `json:"tool_call_id"`
	RemoteToolCallID string     `json:"remote_tool_call_id"`
	RunID            string     `json:"run_id"`
	SafePreview      string     `json:"safe_preview,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	Status           string     `json:"status"`
	ToolName         string     `json:"tool_name"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type TokenRotation struct {
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	CreatedBy      string     `json:"created_by"`
	GrantID        string     `json:"grant_id"`
	ManagementMode string     `json:"management_mode"`
	NewTokenID     string     `json:"new_token_id"`
	OldTokenID     string     `json:"old_token_id,omitempty"`
	RotationID     string     `json:"rotation_id"`
	SafeErrorCode  string     `json:"safe_error_code,omitempty"`
	Status         string     `json:"status"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Prompt struct {
	Default   string    `json:"default_prompt"`
	Effective string    `json:"effective_prompt"`
	Override  string    `json:"override,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	Version   int64     `json:"version"`
}

type CreateInstanceInput struct {
	APIKey                 string
	AllowedTools           []string
	CloudflareClientID     string
	CloudflareClientSecret string
	DashboardToken         string
	DashboardURL           string
	DefaultSessionPolicy   string
	DisplayName            string
	ManagementMode         string
	Profile                string
	RequestTimeoutSeconds  int
	RuntimeURL             string
}

type UpdateInstanceInput struct {
	APIKey                 *string
	AllowedTools           *[]string
	CloudflareClientID     *string
	CloudflareClientSecret *string
	DashboardToken         *string
	DashboardURL           *string
	DisplayName            *string
	ManagementMode         *string
	Profile                *string
	RequestTimeoutSeconds  *int
	RuntimeURL             *string
}

type RotateTokenInput struct {
	ExpiresAt *time.Time
	Name      string
}

type CreateSessionInput struct {
	SessionType string
	Title       string
}

type StartRunInput struct {
	Input        string
	Instructions string
}

// OneTimeTokenMaterial is returned only for manual initial configuration or
// manual rotation. Auto mode consumes the secret server-side and never places
// it in a browser response.
type OneTimeTokenMaterial struct {
	AllowedTools []string `json:"allowed_tools"`
	GatewayURL   string   `json:"gateway_url"`
	Token        string   `json:"token"`
	TokenID      string   `json:"token_id"`
}

type InstanceResult struct {
	Instance     Instance              `json:"instance"`
	OneTimeToken *OneTimeTokenMaterial `json:"one_time_token,omitempty"`
	Rotation     *TokenRotation        `json:"-"`
}

type ReplayResult struct {
	Run     RunRecord     `json:"run"`
	Session SessionRecord `json:"session"`
}

var (
	ErrConflict      = errorString("agent conflict")
	ErrForbidden     = errorString("agent forbidden")
	ErrInvalid       = errorString("invalid agent input")
	ErrNotConfigured = errorString("agent not configured")
	ErrNotFound      = errorString("agent not found")
	ErrRuntime       = errorString("agent runtime failed")
)

type errorString string

func (err errorString) Error() string { return string(err) }
