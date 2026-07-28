// Package jobs owns the durable PostgreSQL job queue and worker protocol.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/project"
)

// Status is a stable job lifecycle state.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
	StatusTimedOut  Status = "timed_out"
)

var jobTypePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`)

// Job is the authoritative queue record returned to clients and workers.
type Job struct {
	Attempts          int                    `json:"attempts"`
	AvailableAt       time.Time              `json:"available_at"`
	CancelRequestedAt *time.Time             `json:"cancel_requested_at,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	CreatedBy         string                 `json:"created_by"`
	ErrorCode         string                 `json:"error_code,omitempty"`
	ErrorMessage      string                 `json:"error_message,omitempty"`
	FinishedAt        *time.Time             `json:"finished_at,omitempty"`
	ID                string                 `json:"id"`
	IdempotencyKey    string                 `json:"idempotency_key,omitempty"`
	JobType           string                 `json:"job_type"`
	LeaseExpiresAt    *time.Time             `json:"lease_expires_at,omitempty"`
	LockedBy          string                 `json:"locked_by,omitempty"`
	MaxAttempts       int                    `json:"max_attempts"`
	Payload           map[string]interface{} `json:"payload"`
	Priority          int                    `json:"priority"`
	ProjectID         string                 `json:"project_id"`
	Result            map[string]interface{} `json:"result,omitempty"`
	StartedAt         *time.Time             `json:"started_at,omitempty"`
	Status            Status                 `json:"status"`
	TimeoutAt         *time.Time             `json:"timeout_at,omitempty"`
	TimeoutSeconds    int                    `json:"timeout_seconds"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

// Log is one append-only worker log entry.
type Log struct {
	Attempt   int                    `json:"attempt"`
	CreatedAt time.Time              `json:"created_at"`
	Fields    map[string]interface{} `json:"fields"`
	ID        string                 `json:"id"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	WorkerID  string                 `json:"worker_id"`
}

// CreateInput describes a new durable job.
type CreateInput struct {
	AvailableAt    *time.Time
	IdempotencyKey string
	JobType        string
	MaxAttempts    int
	Payload        map[string]interface{}
	Priority       int
	ProjectID      string
	TimeoutSeconds int
}

// ClaimInput describes one worker claim attempt.
type ClaimInput struct {
	Admin        bool
	JobTypes     []string
	LeaseSeconds int
	ProjectID    string
	UserID       string
	WorkerID     string
}

// WorkerHeartbeat records worker process liveness and advertised handlers.
type WorkerHeartbeat struct {
	Capabilities []string
	Metadata     map[string]interface{}
	Version      string
	WorkerID     string
}

// Failure describes a worker-reported handler error.
type Failure struct {
	Code              string
	Message           string
	RetryDelaySeconds int
	Retryable         bool
	WorkerID          string
}

// Store is the durable queue boundary.
type Store interface {
	AppendLog(context.Context, string, string, string, string, map[string]interface{}) (Log, error)
	Cancel(context.Context, string, string) (Job, error)
	Claim(context.Context, ClaimInput) (*Job, error)
	Complete(context.Context, string, string, map[string]interface{}) (Job, error)
	Create(context.Context, string, CreateInput) (Job, bool, error)
	Fail(context.Context, string, Failure) (Job, error)
	Get(context.Context, string) (Job, error)
	HeartbeatWorker(context.Context, WorkerHeartbeat) error
	ListLogs(context.Context, string) ([]Log, error)
	Renew(context.Context, string, string, int) (Job, error)
}

// Authenticator resolves trusted caller identity.
type Authenticator interface {
	Authenticate(context.Context, string) (auth.Identity, error)
}

// ProjectAuthorizer applies collaboration permissions.
type ProjectAuthorizer interface {
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

// Service applies queue validation, authentication, and collaboration policy.
type Service struct {
	Auth     Authenticator
	Clock    clock.Clock
	Projects ProjectAuthorizer
	Store    Store
}

// Authenticate resolves a request identity through the Auth module.
func (service Service) Authenticate(
	ctx context.Context,
	authorization string,
) (auth.Identity, error) {
	return service.Auth.Authenticate(ctx, authorization)
}

// Create validates and enqueues a project job.
func (service Service) Create(
	ctx context.Context,
	identity auth.Identity,
	input CreateInput,
) (Job, bool, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.JobType = strings.TrimSpace(input.JobType)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ProjectID == "" || !jobTypePattern.MatchString(input.JobType) || input.Payload == nil {
		return Job{}, false, ErrInvalid
	}
	if input.MaxAttempts == 0 {
		input.MaxAttempts = 3
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 900
	}
	if input.MaxAttempts < 1 || input.MaxAttempts > 100 ||
		input.TimeoutSeconds < 1 || input.TimeoutSeconds > 86400 {
		return Job{}, false, ErrInvalid
	}
	if err := service.authorize(
		ctx,
		identity,
		input.ProjectID,
		project.PermissionJobsCreate,
	); err != nil {
		return Job{}, false, err
	}
	return service.Store.Create(ctx, identity.User.ID, input)
}

// Get returns one job after project authorization.
func (service Service) Get(
	ctx context.Context,
	identity auth.Identity,
	jobID string,
) (Job, error) {
	job, err := service.Store.Get(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if err := service.authorize(
		ctx,
		identity,
		job.ProjectID,
		project.PermissionJobsRead,
	); err != nil {
		return Job{}, err
	}
	return job, nil
}

// Logs returns append-only job logs after project authorization.
func (service Service) Logs(
	ctx context.Context,
	identity auth.Identity,
	jobID string,
) ([]Log, error) {
	if _, err := service.Get(ctx, identity, jobID); err != nil {
		return nil, err
	}
	return service.Store.ListLogs(ctx, jobID)
}

// Cancel requests cancellation or immediately cancels a queued job.
func (service Service) Cancel(
	ctx context.Context,
	identity auth.Identity,
	jobID string,
) (Job, error) {
	job, err := service.Store.Get(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if err := service.authorize(
		ctx,
		identity,
		job.ProjectID,
		project.PermissionJobsCancel,
	); err != nil {
		return Job{}, err
	}
	return service.Store.Cancel(ctx, identity.User.ID, jobID)
}

// Claim atomically claims the next eligible job for an API-token worker.
func (service Service) Claim(
	ctx context.Context,
	identity auth.Identity,
	input ClaimInput,
) (*Job, error) {
	if err := validateWorker(identity, input.WorkerID); err != nil {
		return nil, err
	}
	if input.LeaseSeconds == 0 {
		input.LeaseSeconds = 60
	}
	if input.LeaseSeconds < 10 ||
		input.LeaseSeconds > 900 ||
		len(input.JobTypes) < 1 ||
		len(input.JobTypes) > 100 {
		return nil, ErrInvalid
	}
	for index, jobType := range input.JobTypes {
		input.JobTypes[index] = strings.TrimSpace(jobType)
		if !jobTypePattern.MatchString(input.JobTypes[index]) {
			return nil, ErrInvalid
		}
	}
	input.UserID = identity.User.ID
	input.ProjectID = identity.ProjectID
	input.Admin = identity.User.SystemRole == "admin" && identity.ProjectID == ""
	return service.Store.Claim(ctx, input)
}

// HeartbeatWorker records worker liveness independently of a leased job.
func (service Service) HeartbeatWorker(
	ctx context.Context,
	identity auth.Identity,
	heartbeat WorkerHeartbeat,
) error {
	if err := validateWorker(identity, heartbeat.WorkerID); err != nil {
		return err
	}
	if strings.TrimSpace(heartbeat.Version) == "" ||
		len(heartbeat.Capabilities) < 1 ||
		len(heartbeat.Capabilities) > 100 {
		return ErrInvalid
	}
	if heartbeat.Metadata == nil {
		heartbeat.Metadata = map[string]interface{}{}
	}
	for _, capability := range heartbeat.Capabilities {
		if !jobTypePattern.MatchString(capability) {
			return ErrInvalid
		}
	}
	return service.Store.HeartbeatWorker(ctx, heartbeat)
}

// Renew extends an active lease and returns the current cancellation signal.
func (service Service) Renew(
	ctx context.Context,
	identity auth.Identity,
	jobID string,
	workerID string,
	leaseSeconds int,
) (Job, error) {
	if err := service.authorizeWorkerJob(ctx, identity, jobID, workerID); err != nil {
		return Job{}, err
	}
	if leaseSeconds < 10 || leaseSeconds > 900 {
		return Job{}, ErrInvalid
	}
	return service.Store.Renew(ctx, jobID, workerID, leaseSeconds)
}

// AppendLog appends a worker log to an actively leased job.
func (service Service) AppendLog(
	ctx context.Context,
	identity auth.Identity,
	jobID string,
	workerID string,
	level string,
	message string,
	fields map[string]interface{},
) (Log, error) {
	if err := service.authorizeWorkerJob(ctx, identity, jobID, workerID); err != nil {
		return Log{}, err
	}
	level = strings.TrimSpace(level)
	message = strings.TrimSpace(message)
	if message == "" ||
		(level != "debug" && level != "info" && level != "warning" && level != "error") {
		return Log{}, ErrInvalid
	}
	if fields == nil {
		fields = map[string]interface{}{}
	}
	return service.Store.AppendLog(ctx, jobID, workerID, level, message, fields)
}

// Complete submits a successful handler result.
func (service Service) Complete(
	ctx context.Context,
	identity auth.Identity,
	jobID string,
	workerID string,
	result map[string]interface{},
) (Job, error) {
	if err := service.authorizeWorkerJob(ctx, identity, jobID, workerID); err != nil {
		return Job{}, err
	}
	if result == nil {
		result = map[string]interface{}{}
	}
	return service.Store.Complete(ctx, jobID, workerID, result)
}

// Fail submits a handler failure and lets Core decide retry versus terminal state.
func (service Service) Fail(
	ctx context.Context,
	identity auth.Identity,
	jobID string,
	failure Failure,
) (Job, error) {
	if err := service.authorizeWorkerJob(
		ctx,
		identity,
		jobID,
		failure.WorkerID,
	); err != nil {
		return Job{}, err
	}
	failure.Code = strings.TrimSpace(failure.Code)
	failure.Message = strings.TrimSpace(failure.Message)
	if failure.Code == "" || failure.Message == "" ||
		failure.RetryDelaySeconds < 0 || failure.RetryDelaySeconds > 86400 {
		return Job{}, ErrInvalid
	}
	return service.Store.Fail(ctx, jobID, failure)
}

func (service Service) authorizeWorkerJob(
	ctx context.Context,
	identity auth.Identity,
	jobID string,
	workerID string,
) error {
	if err := validateWorker(identity, workerID); err != nil {
		return err
	}
	job, err := service.Store.Get(ctx, jobID)
	if err != nil {
		return err
	}
	return service.authorize(
		ctx,
		identity,
		job.ProjectID,
		project.PermissionJobsRead,
	)
}

func (service Service) authorize(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	permission project.Permission,
) error {
	if identity.Kind == "api" &&
		identity.User.SystemRole == "admin" &&
		(identity.ProjectID == "" || identity.ProjectID == projectID) {
		return nil
	}
	if service.Projects == nil ||
		service.Projects.Authorize(ctx, identity, projectID, permission) != nil {
		return ErrForbidden
	}
	return nil
}

func validateWorker(identity auth.Identity, workerID string) error {
	if identity.Kind != "api" {
		return ErrWorkerToken
	}
	if strings.TrimSpace(workerID) == "" || len(workerID) > 200 {
		return ErrInvalid
	}
	return nil
}

var (
	ErrConflict    = errors.New("job conflict")
	ErrForbidden   = errors.New("job permission denied")
	ErrInvalid     = errors.New("invalid job input")
	ErrLeaseLost   = errors.New("job lease lost")
	ErrNotFound    = errors.New("job not found")
	ErrWorkerToken = errors.New("worker API token required")
)

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
