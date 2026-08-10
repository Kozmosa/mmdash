// Package agent defines the runtime-neutral boundary used by the Agent domain.
package agent

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid agent adapter argument")
	ErrUnsupported     = errors.New("agent adapter operation unsupported")
)

// ErrorCode is a stable, credential-free error category returned by adapters.
type ErrorCode string

const (
	ErrorInvalid        ErrorCode = "invalid_request"
	ErrorAuthentication ErrorCode = "authentication_failed"
	ErrorPermission     ErrorCode = "permission_denied"
	ErrorNotFound       ErrorCode = "not_found"
	ErrorConflict       ErrorCode = "conflict"
	ErrorRateLimited    ErrorCode = "rate_limited"
	ErrorUnavailable    ErrorCode = "unavailable"
	ErrorTimeout        ErrorCode = "timeout"
	ErrorProtocol       ErrorCode = "protocol_error"
	ErrorUnsupported    ErrorCode = "unsupported"
)

// AdapterError deliberately contains only safe, normalized information. An
// adapter must not put upstream response bodies, credentials, or target URLs in
// Message because this error can be written to Audit and application logs.
type AdapterError struct {
	Code       ErrorCode
	Operation  string
	Message    string
	StatusCode int
	Retryable  bool
	RetryAfter time.Duration
}

func (err *AdapterError) Error() string {
	if err == nil {
		return ""
	}
	if err.Operation == "" {
		return string(err.Code)
	}
	return fmt.Sprintf("%s: %s", err.Operation, err.Code)
}

// AdapterConfig is intentionally opaque to the Agent domain. Factories for a
// concrete runtime interpret Values and must construct a new, instance-scoped
// adapter so credentials and HTTP connection pools are never shared between
// Agent instances.
type AdapterConfig struct {
	InstanceID string
	Values     map[string]string
}

// ProjectAccessCapabilities advertises the three independently selectable
// project-access operations. Auto management is available only when all three
// values are true.
type ProjectAccessCapabilities struct {
	Verify    bool
	Configure bool
	Rotate    bool
}

// DeclaredCapabilities is static registration metadata. Instance Probe results
// may narrow these capabilities when a runtime version or deployment does not
// support one of them.
type DeclaredCapabilities struct {
	ProjectAccess ProjectAccessCapabilities
}

// RuntimeCapabilities is the normalized result of a live capability probe.
// EventReplay is distinct from event streaming: a runtime may accept a
// Last-Event-ID header while being unable to replay missed events.
type RuntimeCapabilities struct {
	Sessions         bool
	SessionFork      bool
	SessionChat      bool
	SessionStreaming bool
	Runs             bool
	RunStreaming     bool
	RunStop          bool
	RunApproval      bool
	Jobs             bool
	ToolProgress     bool
	ProjectAccess    ProjectAccessCapabilities
	EventReplay      bool
}

type ProbeResult struct {
	Healthy       bool
	Authenticated bool
	Platform      string
	Version       string
	Model         string
	CheckedAt     time.Time
	Capabilities  RuntimeCapabilities
}

type PageRequest struct {
	Limit  int
	Offset int
}

type SessionFilter struct {
	Page            PageRequest
	Source          string
	IncludeChildren bool
}

type SessionPage struct {
	Sessions []Session
	Limit    int
	Offset   int
	HasMore  bool
}

type Session struct {
	RemoteID         string
	ParentRemoteID   string
	Source           string
	Model            string
	Title            string
	StartedAt        time.Time
	EndedAt          *time.Time
	EndReason        string
	LastActiveAt     *time.Time
	Preview          string
	MessageCount     int64
	ToolCallCount    int64
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64
	EstimatedCostUSD float64
	ActualCostUSD    float64
	APICallCount     int64
	HasSystemPrompt  bool
	HasModelConfig   bool
}

type CreateSessionRequest struct {
	RemoteID     string
	Source       string
	Model        string
	Title        string
	SystemPrompt string
}

type UpdateSessionRequest struct {
	Title     *string
	EndReason *string
}

type ForkSessionRequest struct {
	RemoteID string
	Title    string
}

type Message struct {
	RemoteID        string
	SessionRemoteID string
	Role            string
	Content         string
	ToolCallID      string
	ToolName        string
	ToolCalls       []ToolCall
	Timestamp       *time.Time
	TokenCount      int64
	FinishReason    string
}

// ToolCall is a UI-safe summary. Runtime arguments, results, commands, and
// reasoning are intentionally absent from the cross-process model.
type ToolCall struct {
	RemoteID string
	Name     string
	Status   string
}

type Usage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

type ChatRequest struct {
	Message      string
	Instructions string
}

type ChatResponse struct {
	SessionRemoteID string
	Message         Message
	Usage           Usage
	Runtime         RuntimeSelection
}

type RuntimeSelection struct {
	Provider    string
	Model       string
	RouteSource string
	ModelLock   string
}

type ConversationMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type StartRunRequest struct {
	Input               string
	Instructions        string
	SessionRemoteID     string
	ConversationHistory []ConversationMessage
	Model               string
	Provider            string
}

type RunStatus string

const (
	RunQueued             RunStatus = "queued"
	RunRunning            RunStatus = "running"
	RunWaitingForApproval RunStatus = "waiting_for_approval"
	RunStopping           RunStatus = "stopping"
	RunCompleted          RunStatus = "completed"
	RunFailed             RunStatus = "failed"
	RunCancelled          RunStatus = "cancelled"
)

type Run struct {
	RemoteID        string
	SessionRemoteID string
	Status          RunStatus
	Model           string
	Output          string
	Usage           Usage
	Error           *SafeError
	LastEvent       string
	CreatedAt       *time.Time
	UpdatedAt       *time.Time
}

type SafeError struct {
	Code    string
	Message string
}

type ApprovalChoice string

const (
	ApprovalOnce    ApprovalChoice = "once"
	ApprovalSession ApprovalChoice = "session"
	ApprovalAlways  ApprovalChoice = "always"
	ApprovalDeny    ApprovalChoice = "deny"
)

type ApprovalRequest struct {
	// RemoteID is the stable provider-neutral ID emitted to the browser. A
	// Runtime without targetable approval IDs must map it through its verified
	// ordering semantics rather than silently accepting an arbitrary ID.
	RemoteID   string
	Choice     ApprovalChoice
	ResolveAll bool
}

type ApprovalResult struct {
	// RemoteID confirms which normalized approval the Adapter mapped.
	RemoteID    string
	RunRemoteID string
	Choice      ApprovalChoice
	Resolved    int
}

type StreamOptions struct {
	LastEventID string
}

type EventType string

const (
	EventRunStarted        EventType = "run.started"
	EventRunCompleted      EventType = "run.completed"
	EventRunFailed         EventType = "run.failed"
	EventRunCancelled      EventType = "run.stopped"
	EventMessageStarted    EventType = "message.started"
	EventMessageDelta      EventType = "message.delta"
	EventMessageCompleted  EventType = "message.completed"
	EventToolProgress      EventType = "tool.progress"
	EventToolStarted       EventType = "tool.started"
	EventToolCompleted     EventType = "tool.completed"
	EventToolFailed        EventType = "tool.failed"
	EventApprovalRequested EventType = "approval.required"
	EventApprovalResponded EventType = "approval.responded"
	EventSubagentStarted   EventType = "subagent.started"
	EventSubagentCompleted EventType = "subagent.completed"
	EventHeartbeat         EventType = "heartbeat"
	EventDone              EventType = "done"
	EventError             EventType = "error"
)

// Event contains only fields required to render a safe lifecycle view. Text is
// used for assistant output deltas, never for reasoning or tool results.
type Event struct {
	ID              string
	Type            EventType
	Sequence        int64
	RunRemoteID     string
	SessionRemoteID string
	MessageRemoteID string
	Text            string
	Tool            *ToolCall
	Status          string
	Timestamp       *time.Time
	Usage           Usage
	Approval        *ApprovalEvent
	Error           *SafeError
}

type ApprovalEvent struct {
	RemoteID string
	Choices  []ApprovalChoice
	Choice   ApprovalChoice
	Resolved int
}

type EventHandler func(context.Context, Event) error

type Job struct {
	RemoteID         string
	Name             string
	Prompt           string
	Skills           []string
	Schedule         string
	ScheduleDisplay  string
	RepeatTimes      int
	RepeatCompleted  int
	Enabled          bool
	State            string
	Deliver          string
	Origin           string
	CreatedAt        *time.Time
	NextRunAt        *time.Time
	LastRunAt        *time.Time
	LastStatus       string
	HasLastError     bool
	HasDeliveryError bool
}

type CreateJobRequest struct {
	Name     string
	Schedule string
	Prompt   string
	Deliver  string
	Skills   []string
	Repeat   int
}

type UpdateJobRequest struct {
	Name     *string
	Schedule *string
	Prompt   *string
	Deliver  *string
	Skills   *[]string
	Repeat   *int
	Enabled  *bool
}

type ProjectAccessRequest struct {
	BindingID       string
	Endpoint        string
	Credential      string
	ExpectedTools   []string
	CurrentRemoteID string
}

type ProjectAccessState string

const (
	ProjectAccessPending     ProjectAccessState = "pending"
	ProjectAccessReady       ProjectAccessState = "ready"
	ProjectAccessUnavailable ProjectAccessState = "unavailable"
	ProjectAccessUnsupported ProjectAccessState = "unsupported"
)

// AccessRoute describes a generic management connection path without exposing
// a vendor or proxy product in the AgentAdapter contract.
type AccessRoute string

const (
	AccessRouteDirect             AccessRoute = "direct"
	AccessRouteAuthenticatedProxy AccessRoute = "authenticated_proxy"
)

// ProjectAccessResult.RemoteID is an opaque remote configuration reference.
// The Agent domain may persist it for a later rotation, but must not interpret
// it as a vendor-specific server name.
type ProjectAccessResult struct {
	State          ProjectAccessState
	Route          AccessRoute
	RemoteID       string
	Verified       bool
	Tools          []string
	CleanupPending bool
}

// ProjectAccessFinalizeRequest identifies a previously active remote
// configuration that may be removed after ActiveRemoteID's credential has
// been durably activated by Core. Credentials are deliberately absent from
// this boundary so a best-effort cleanup can never replay or expose them.
type ProjectAccessFinalizeRequest struct {
	ActiveRemoteID   string
	PreviousRemoteID string
}

// ProjectAccessFinalizeResult reports whether the previous remote
// configuration still needs cleanup. A pending cleanup does not invalidate
// the already activated project access path.
type ProjectAccessFinalizeResult struct {
	CleanupPending bool
}

// Adapter is the complete runtime-neutral port. Implementations must be safe
// for concurrent use, while each instance must own its own authentication and
// transport context.
type Adapter interface {
	Probe(context.Context) (ProbeResult, error)
	// CheckRuntime performs the bounded, end-to-end runtime interoperability
	// exercise after Probe has established the advertised capabilities. It is
	// intentionally separate from Probe so health/capability reads never imply
	// that Session, message, Run, SSE, status, and stop paths are usable.
	CheckRuntime(context.Context) error

	ListSessions(context.Context, SessionFilter) (SessionPage, error)
	CreateSession(context.Context, CreateSessionRequest) (Session, error)
	GetSession(context.Context, string) (Session, error)
	UpdateSession(context.Context, string, UpdateSessionRequest) (Session, error)
	DeleteSession(context.Context, string) error
	ForkSession(context.Context, string, ForkSessionRequest) (Session, error)
	ListMessages(context.Context, string) ([]Message, error)
	Chat(context.Context, string, ChatRequest) (ChatResponse, error)
	StreamChat(context.Context, string, ChatRequest, StreamOptions, EventHandler) error

	StartRun(context.Context, StartRunRequest) (Run, error)
	GetRun(context.Context, string) (Run, error)
	StreamRun(context.Context, string, StreamOptions, EventHandler) error
	ApproveRun(context.Context, string, ApprovalRequest) (ApprovalResult, error)
	StopRun(context.Context, string) (Run, error)

	ListJobs(context.Context, bool) ([]Job, error)
	CreateJob(context.Context, CreateJobRequest) (Job, error)
	GetJob(context.Context, string) (Job, error)
	UpdateJob(context.Context, string, UpdateJobRequest) (Job, error)
	DeleteJob(context.Context, string) error
	PauseJob(context.Context, string) (Job, error)
	ResumeJob(context.Context, string) (Job, error)
	RunJob(context.Context, string) (Job, error)

	VerifyProjectAccess(context.Context, ProjectAccessRequest) (ProjectAccessResult, error)
	ConfigureProjectAccess(context.Context, ProjectAccessRequest) (ProjectAccessResult, error)
	RotateProjectAccess(context.Context, ProjectAccessRequest) (ProjectAccessResult, error)
	FinalizeProjectAccess(context.Context, ProjectAccessFinalizeRequest) (ProjectAccessFinalizeResult, error)
}

// AgentAdapter keeps the design-document name available without duplicating
// the interface.
type AgentAdapter = Adapter

type Factory func(context.Context, AdapterConfig) (Adapter, error)
