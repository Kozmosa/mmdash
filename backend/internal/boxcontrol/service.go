package boxcontrol

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/project"
)

var (
	ErrInvalid   = errors.New("invalid Box request")
	ErrForbidden = errors.New("Box access forbidden")
	ErrNotFound  = errors.New("Box resource not found")
	ErrConflict  = errors.New("Box resource conflict")
	ErrNoTask    = errors.New("no Box task available")
	ErrLeaseLost = errors.New("Box task lease lost")
)

var artifactPointerIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
var artifactSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Service struct {
	Access    Access
	Clock     Clock
	Generator IDGenerator
	Issuer    TokenIssuer
	Revoker   TokenRevoker
	Store     Store
	Observer  Observer
	Artifacts ArtifactReceiver
}

type Observer interface {
	TaskStatus(context.Context, Task) error
	TaskResult(context.Context, Task, Result) error
}

type ArtifactReceiver interface {
	ArchiveArtifact(context.Context, Task, string, int64, io.Reader) (map[string]interface{}, error)
}

func (service Service) UploadArtifact(ctx context.Context, identity auth.Identity, boxID, taskID, expectedSHA string, expectedSize int64, input io.Reader) (map[string]interface{}, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return nil, err
	}
	if service.Artifacts == nil || expectedSize < 1 || expectedSize > 5<<30 || !artifactSHA256Pattern.MatchString(expectedSHA) {
		return nil, ErrInvalid
	}
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil || task.BoxID != boxID {
		return nil, ErrNotFound
	}
	return service.Artifacts.ArchiveArtifact(ctx, task, expectedSHA, expectedSize, input)
}

func (service Service) Authenticate(ctx context.Context, authorization string) (auth.Identity, error) {
	return service.Access.Authenticate(ctx, authorization)
}

func (service Service) Register(ctx context.Context, identity auth.Identity, projectID string, box Box, idempotency string) (BoxRegistration, error) {
	if identity.Kind != "session" && identity.Kind != "api" {
		return BoxRegistration{}, ErrForbidden
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || service.Access.Authorize(ctx, identity, projectID, project.PermissionBoxManage) != nil {
		return BoxRegistration{}, ErrForbidden
	}
	if service.Issuer == nil || service.Generator == nil || service.Store == nil || strings.TrimSpace(idempotency) == "" {
		return BoxRegistration{}, ErrInvalid
	}
	if err := validateBox(&box, projectID); err != nil {
		return BoxRegistration{}, err
	}
	box.ID, _ = service.Generator.New()
	box.ProjectID, box.Status = projectID, StatusRegistering
	box.CreatedAt, box.UpdatedAt = service.now(), service.now()
	issued, err := service.Issuer.IssueToken(ctx, identity, "box", box.Name, projectID, nil)
	if err != nil {
		return BoxRegistration{}, err
	}
	box.TokenID = issued.Token.ID
	if err := service.Store.Create(ctx, box, idempotency); err != nil {
		if service.Revoker != nil {
			_ = service.Revoker.RevokeToken(ctx, identity, issued.Token.ID)
		}
		return BoxRegistration{}, err
	}
	return BoxRegistration{Box: box, Token: issued.Secret}, nil
}

func (service Service) List(ctx context.Context, identity auth.Identity, projectID string) ([]Box, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionBoxRead); err != nil {
		return nil, err
	}
	return service.Store.List(ctx, projectID)
}

func (service Service) Get(ctx context.Context, identity auth.Identity, boxID string) (Box, error) {
	box, err := service.Store.Get(ctx, strings.TrimSpace(boxID))
	if err != nil {
		return Box{}, err
	}
	if identity.Kind == "box" {
		if identity.TokenID != box.TokenID || identity.ProjectID != box.ProjectID {
			return Box{}, ErrForbidden
		}
		return box, nil
	}
	if err := service.authorize(ctx, identity, box.ProjectID, project.PermissionBoxRead); err != nil {
		return Box{}, err
	}
	return box, nil
}

func (service Service) Heartbeat(ctx context.Context, identity auth.Identity, boxID string, update Box) (Heartbeat, error) {
	box, err := service.authorizeBoxIdentity(ctx, identity, boxID)
	if err != nil {
		return Heartbeat{}, err
	}
	if err := validateBox(&update, box.ProjectID); err != nil {
		return Heartbeat{}, err
	}
	updated, err := service.Store.UpdateHeartbeat(ctx, box.ID, update, service.now())
	if err != nil {
		return Heartbeat{}, err
	}
	return Heartbeat{Box: updated}, nil
}

// MarkOffline transitions stale online Boxes in bounded batches. It is used by
// the Core maintenance loop and is intentionally owned by Box Control so that
// lease recovery and observability share one authoritative state transition.
func (service Service) MarkOffline(ctx context.Context, now, heartbeatBefore time.Time, limit int) ([]Box, error) {
	if service.Store == nil {
		return nil, ErrInvalid
	}
	return service.Store.MarkOffline(ctx, now, heartbeatBefore, limit)
}

func (service Service) Bind(ctx context.Context, identity auth.Identity, projectID, boxID string) (Box, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionBoxManage); err != nil {
		return Box{}, err
	}
	box, err := service.Store.Get(ctx, boxID)
	if err != nil {
		return Box{}, err
	}
	if box.ProjectID != projectID {
		return Box{}, ErrConflict
	}
	return service.Store.Bind(ctx, projectID, boxID, service.now())
}

func (service Service) Unbind(ctx context.Context, identity auth.Identity, projectID string) error {
	if err := service.authorize(ctx, identity, projectID, project.PermissionBoxManage); err != nil {
		return err
	}
	return service.Store.Unbind(ctx, projectID, service.now())
}

func (service Service) CreateTask(ctx context.Context, task Task) error {
	if task.ID == "" || task.ExperimentID == "" || task.ProjectID == "" || task.Status != TaskQueued || task.RunSpec == nil {
		return ErrInvalid
	}
	return service.Store.CreateTask(ctx, task)
}

func (service Service) Claim(ctx context.Context, identity auth.Identity, boxID string, lease time.Duration) (*Task, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return nil, err
	}
	if lease <= 0 {
		lease = time.Minute
	}
	if lease < 10*time.Second || lease > 15*time.Minute {
		return nil, ErrInvalid
	}
	return service.Store.ClaimTask(ctx, boxID, service.now(), lease)
}

func (service Service) RecoverExpired(ctx context.Context, now time.Time, limit int) ([]Task, error) {
	items, err := service.Store.RecoverExpired(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	for _, task := range items {
		if service.Observer != nil {
			if err := service.Observer.TaskStatus(ctx, task); err != nil {
				return nil, err
			}
		}
	}
	return items, nil
}

func (service Service) Renew(ctx context.Context, identity auth.Identity, boxID, taskID string, lease time.Duration) (TaskLease, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return TaskLease{}, err
	}
	return service.Store.RenewTask(ctx, boxID, taskID, service.now(), lease)
}

func (service Service) Cancel(ctx context.Context, identity auth.Identity, projectID, taskID string) (Task, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Task{}, err
	}
	return service.Store.CancelTask(ctx, taskID, service.now())
}

func (service Service) AppendLog(ctx context.Context, identity auth.Identity, boxID, taskID string, log Log) (Log, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return Log{}, err
	}
	if log.TaskID != taskID || log.Message == "" || len(log.Message) > 20000 {
		return Log{}, ErrInvalid
	}
	if log.Level != "debug" && log.Level != "info" && log.Level != "warning" && log.Level != "error" {
		return Log{}, ErrInvalid
	}
	return service.Store.AppendLog(ctx, log)
}

func (service Service) ReportStatus(ctx context.Context, identity auth.Identity, boxID, taskID, status string, exitCode *int, code, message string, usage map[string]interface{}, summary string) (Task, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return Task{}, err
	}
	if !validTaskStatus(status) {
		return Task{}, ErrInvalid
	}
	task, err := service.Store.ReportStatus(ctx, boxID, taskID, status, exitCode, strings.TrimSpace(code), strings.TrimSpace(message), usage, strings.TrimSpace(summary), service.now())
	if err == nil && service.Observer != nil {
		err = service.Observer.TaskStatus(ctx, task)
	}
	return task, err
}

func (service Service) SubmitResult(ctx context.Context, identity auth.Identity, boxID, taskID string, result Result) (Task, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return Task{}, err
	}
	if result.Manifest == nil || result.Artifact == nil || !validArtifactPointer(result.Artifact) {
		return Task{}, ErrInvalid
	}
	task, err := service.Store.SubmitResult(ctx, boxID, taskID, result, service.now())
	if err == nil && service.Observer != nil {
		err = service.Observer.TaskResult(ctx, task, result)
	}
	return task, err
}

func validArtifactPointer(value map[string]interface{}) bool {
	artifactID, artifactOK := value["artifact_id"].(string)
	versionID, versionOK := value["version_id"].(string)
	filename, filenameOK := value["filename"].(string)
	sha, shaOK := value["sha256"].(string)
	if !artifactOK || !versionOK || !filenameOK || !shaOK || !artifactPointerIDPattern.MatchString(artifactID) || !artifactPointerIDPattern.MatchString(versionID) || filename != "artifact.zip" || !artifactSHA256Pattern.MatchString(sha) {
		return false
	}
	switch size := value["size_bytes"].(type) {
	case int:
		return size > 0
	case int64:
		return size > 0
	case float64:
		return size > 0 && size == float64(int64(size))
	default:
		return false
	}
}

// ValidateArtifactPointer is shared with Experiment's Core result observer;
// it keeps the artifact identity and filename checks on the Core boundary.
func ValidateArtifactPointer(value map[string]interface{}) bool {
	return validArtifactPointer(value)
}

func (service Service) Logs(ctx context.Context, identity auth.Identity, projectID, taskID string, offset, limit int) ([]Log, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentRead); err != nil {
		return nil, err
	}
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil || task.ProjectID != projectID {
		return nil, ErrNotFound
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 || offset < 0 {
		return nil, ErrInvalid
	}
	return service.Store.ListLogs(ctx, taskID, offset, limit)
}

// ReadTask is the permission-checked Data Hub adapter for an authoritative
// Box task record. Callers never receive a database handle or private Box
// token through this boundary.
func (service Service) ReadTask(ctx context.Context, identity auth.Identity, projectID, taskID string) (Task, error) {
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil || task.ProjectID != projectID {
		return Task{}, ErrNotFound
	}
	if err := service.authorizeTaskRead(ctx, identity, task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (service Service) authorizeBoxIdentity(ctx context.Context, identity auth.Identity, boxID string) (Box, error) {
	if identity.Kind != "box" {
		return Box{}, ErrForbidden
	}
	box, err := service.Store.Get(ctx, boxID)
	if err != nil {
		return Box{}, err
	}
	if identity.TokenID == "" || identity.TokenID != box.TokenID || identity.ProjectID != box.ProjectID || box.Status == StatusRevoked {
		return Box{}, ErrForbidden
	}
	return box, nil
}

func (service Service) authorize(ctx context.Context, identity auth.Identity, projectID string, permission project.Permission) error {
	if strings.TrimSpace(projectID) == "" || service.Access == nil {
		return ErrForbidden
	}
	if identity.ProjectID != "" && identity.ProjectID != projectID {
		return ErrForbidden
	}
	if err := service.Access.Authorize(ctx, identity, projectID, permission); err != nil {
		return ErrForbidden
	}
	return nil
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock.Now().UTC()
}

func validateBox(box *Box, projectID string) error {
	box.Name, box.Version = strings.TrimSpace(box.Name), strings.TrimSpace(box.Version)
	if box.Name == "" || len(box.Name) > 200 || box.Version == "" || len(box.Capabilities) == 0 || len(box.Runtimes) == 0 || box.ProjectID != "" && box.ProjectID != projectID {
		return ErrInvalid
	}
	if err := validateLimits(box.Limits); err != nil {
		return err
	}
	for _, runtime := range box.Runtimes {
		if runtime.Name != "local-docker" && runtime.Name != "e2b" {
			return ErrInvalid
		}
	}
	box.Load = normalizeLoad(box.Load)
	return nil
}

func validateLimits(limits ResourceLimits) error {
	if limits.CPUMillis < 1 || limits.MemoryBytes < 1<<20 || limits.TimeoutSecond < 1 || limits.TimeoutSecond > 86400 || limits.DiskBytes < 1<<20 || limits.PIDs < 1 {
		return ErrInvalid
	}
	if limits.Network != "disabled" && limits.Network != "restricted" && limits.Network != "enabled" {
		return ErrInvalid
	}
	return nil
}

func normalizeLoad(load Load) Load {
	if load.Capacity < 1 {
		load.Capacity = 1
	}
	if load.RunningTasks < 0 {
		load.RunningTasks = 0
	}
	return load
}
func validTaskStatus(status string) bool {
	switch status {
	case TaskPreparing, TaskRunning, TaskSucceeded, TaskFailed, TaskCanceled, TaskTimedOut:
		return true
	default:
		return false
	}
}

func (service Service) authorizeTaskRead(ctx context.Context, identity auth.Identity, task Task) error {
	return service.authorize(ctx, identity, task.ProjectID, project.PermissionExperimentRead)
}
