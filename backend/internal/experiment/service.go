package experiment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/artifact"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/boxcontrol"
	"github.com/mmdash/mmdash/backend/internal/project"
)

var (
	ErrInvalid   = errors.New("invalid experiment request")
	ErrForbidden = errors.New("experiment access forbidden")
	ErrNotFound  = errors.New("experiment not found")
	ErrConflict  = errors.New("experiment state conflict")
	ErrNoResult  = errors.New("experiment has no result")
)

var (
	commitPattern         = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	entrypointPattern     = regexp.MustCompile(`^(python3?|node|go|binary):[a-zA-Z0-9_./-]+$`)
	manifestSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type ProjectAccess interface {
	Authenticate(context.Context, string) (auth.Identity, error)
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

type CommitValidator interface {
	ValidateCommit(context.Context, auth.Identity, string, string) error
}

type IDGenerator interface{ New() (string, error) }
type Clock interface{ Now() time.Time }

type Service struct {
	Artifacts ArtifactArchiver
	Access    ProjectAccess
	Boxes     *boxcontrol.Service
	Clock     Clock
	Commit    CommitValidator
	Generator IDGenerator
	Store     Store
}

type ArtifactArchiver interface {
	ArchiveExperimentResult(context.Context, string, string, string, string, int64, io.Reader) (artifact.Detail, error)
}

func (service Service) Authenticate(ctx context.Context, authorization string) (auth.Identity, error) {
	return service.Access.Authenticate(ctx, authorization)
}

func (service Service) Create(ctx context.Context, identity auth.Identity, projectID string, input Experiment) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Experiment{}, err
	}
	if err := validateExperiment(&input, projectID); err != nil {
		return Experiment{}, err
	}
	if service.Commit != nil {
		if err := service.Commit.ValidateCommit(ctx, identity, projectID, input.SourceCommit); err != nil {
			return Experiment{}, ErrInvalid
		}
	}
	if service.Generator == nil || service.Store == nil {
		return Experiment{}, ErrInvalid
	}
	input.ID, _ = service.Generator.New()
	input.ProjectID, input.CreatedBy = projectID, identity.User.ID
	input.Status = StatusCreated
	input.CreatedAt, input.UpdatedAt = service.now(), service.now()
	created, _, err := service.Store.Create(ctx, input)
	return created, err
}

func (service Service) List(ctx context.Context, identity auth.Identity, projectID, status, cursor string, limit int) (Page, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentRead); err != nil {
		return Page{}, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		return Page{}, ErrInvalid
	}
	offset := 0
	if cursor != "" {
		if _, err := fmt.Sscan(cursor, &offset); err != nil || offset < 0 {
			return Page{}, ErrInvalid
		}
	}
	return service.Store.List(ctx, projectID, status, offset, limit)
}

func (service Service) Get(ctx context.Context, identity auth.Identity, projectID, experimentID string) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentRead); err != nil {
		return Experiment{}, err
	}
	item, err := service.Store.Get(ctx, projectID, experimentID)
	if err != nil {
		return Experiment{}, err
	}
	return item, nil
}

func (service Service) Run(ctx context.Context, identity auth.Identity, projectID, experimentID string) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Experiment{}, err
	}
	item, err := service.Store.Get(ctx, projectID, experimentID)
	if err != nil {
		return Experiment{}, err
	}
	if item.Status != StatusCreated {
		if item.Status == StatusQueued || item.Status == StatusPreparing || item.Status == StatusRunning || item.Status == StatusSucceeded || item.Status == StatusFailed || item.Status == StatusCanceled || item.Status == StatusArchived {
			return item, nil
		}
		return Experiment{}, ErrConflict
	}
	if service.Boxes == nil || service.Generator == nil {
		return Experiment{}, ErrInvalid
	}
	taskID, err := service.Generator.New()
	if err != nil {
		return Experiment{}, err
	}
	task := boxcontrol.Task{ID: taskID, ExperimentID: item.ID, ProjectID: projectID, Status: boxcontrol.TaskQueued, Attempt: 0, MaxAttempts: item.MaxAttempts, RunSpec: runSpec(item), CreatedAt: service.now(), UpdatedAt: service.now()}
	if coordinator, ok := service.Store.(QueueCoordinator); ok {
		return coordinator.QueueWithTask(ctx, item, task, service.now())
	}
	if err := service.Boxes.CreateTask(ctx, task); err != nil {
		return Experiment{}, err
	}
	return service.Store.Queue(ctx, item.ID, taskID, service.now())
}

func (service Service) Cancel(ctx context.Context, identity auth.Identity, projectID, experimentID string) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Experiment{}, err
	}
	item, err := service.Store.Cancel(ctx, projectID, experimentID, service.now())
	if err != nil {
		return Experiment{}, err
	}
	if item.TaskID != "" && service.Boxes != nil {
		_, _ = service.Boxes.Cancel(ctx, identity, projectID, item.TaskID)
	}
	return item, nil
}

func (service Service) Archive(ctx context.Context, identity auth.Identity, projectID, experimentID string) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Experiment{}, err
	}
	return service.Store.Archive(ctx, projectID, experimentID, service.now())
}

func (service Service) Logs(ctx context.Context, identity auth.Identity, projectID, experimentID string, offset, limit int) ([]boxcontrol.Log, error) {
	item, err := service.Get(ctx, identity, projectID, experimentID)
	if err != nil {
		return nil, err
	}
	if item.TaskID == "" {
		return []boxcontrol.Log{}, nil
	}
	return service.Boxes.Logs(ctx, identity, projectID, item.TaskID, offset, limit)
}

func (service Service) Result(ctx context.Context, identity auth.Identity, projectID, experimentID string) (ResultBundle, error) {
	item, err := service.Get(ctx, identity, projectID, experimentID)
	if err != nil {
		return ResultBundle{}, err
	}
	if item.Result == nil {
		return ResultBundle{}, ErrNoResult
	}
	return *item.Result, nil
}

func (service Service) Compare(ctx context.Context, identity auth.Identity, projectID string, ids []string) (Comparison, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentRead); err != nil {
		return Comparison{}, err
	}
	if len(ids) < 2 || len(ids) > 20 {
		return Comparison{}, ErrInvalid
	}
	return service.Store.Compare(ctx, projectID, ids)
}

func (service *Service) TaskStatus(ctx context.Context, task boxcontrol.Task) error {
	if service == nil || service.Store == nil {
		return ErrInvalid
	}
	_, err := service.Store.ApplyTaskStatus(ctx, task, service.now())
	return err
}

func (service *Service) TaskResult(ctx context.Context, task boxcontrol.Task, result boxcontrol.Result) error {
	if service == nil || service.Store == nil || !validResultManifest(result.Manifest, task.ExperimentID) || !boxcontrol.ValidateArtifactPointer(result.Artifact) {
		return ErrInvalid
	}
	_, err := service.Store.ApplyResult(ctx, task, result, service.now())
	return err
}

func validResultManifest(manifest map[string]interface{}, experimentID string) bool {
	if manifest == nil || manifest["experiment_id"] != experimentID || manifest["status"] != "succeeded" || manifest["schema_version"] != "1" {
		return false
	}
	files, ok := manifest["files"].([]interface{})
	if !ok || len(files) > 10000 {
		return false
	}
	seen := map[string]struct{}{}
	for _, raw := range files {
		file, ok := raw.(map[string]interface{})
		if !ok {
			return false
		}
		name, nameOK := file["path"].(string)
		digest, digestOK := file["sha256"].(string)
		if !nameOK || !digestOK || !manifestSHA256Pattern.MatchString(digest) || name == "" || path.Clean(name) != name || strings.HasPrefix(name, "/") || name == "." || name == ".." || strings.HasPrefix(name, "../") {
			return false
		}
		if _, exists := seen[name]; exists {
			return false
		}
		seen[name] = struct{}{}
	}
	return true
}

// ArchiveArtifact is the only Core-side path by which a Box may turn its
// standalone artifact.zip into the authoritative Artifact/result pointer.
func (service *Service) ArchiveArtifact(ctx context.Context, task boxcontrol.Task, expectedSHA string, expectedSize int64, input io.Reader) (map[string]interface{}, error) {
	if service == nil || service.Store == nil || service.Artifacts == nil {
		return nil, ErrInvalid
	}
	item, err := service.Store.Get(ctx, task.ProjectID, task.ExperimentID)
	if err != nil {
		return nil, err
	}
	detail, err := service.Artifacts.ArchiveExperimentResult(ctx, task.ProjectID, task.ExperimentID, item.CreatedBy, expectedSHA, expectedSize, input)
	if err != nil {
		return nil, err
	}
	if detail.CurrentVersion == nil {
		return nil, ErrNoResult
	}
	return map[string]interface{}{"artifact_id": detail.Artifact.ID, "version_id": detail.CurrentVersion.ID, "filename": detail.CurrentVersion.Filename, "sha256": detail.CurrentVersion.SHA256, "size_bytes": detail.CurrentVersion.SizeBytes}, nil
}

func (service Service) authorize(ctx context.Context, identity auth.Identity, projectID string, permission project.Permission) error {
	if strings.TrimSpace(projectID) == "" || service.Access == nil || (identity.ProjectID != "" && identity.ProjectID != projectID) {
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

func validateExperiment(item *Experiment, projectID string) error {
	item.Name = strings.TrimSpace(item.Name)
	item.SourceCommit = strings.TrimSpace(item.SourceCommit)
	item.Entrypoint = strings.TrimSpace(item.Entrypoint)
	item.Runtime = strings.TrimSpace(item.Runtime)
	item.IdempotencyKey = strings.TrimSpace(item.IdempotencyKey)
	if item.Name == "" || len(item.Name) > 200 || item.IdempotencyKey == "" || len(item.IdempotencyKey) > 200 || !commitPattern.MatchString(item.SourceCommit) || !validEntrypoint(item.Entrypoint) || item.ProjectID != "" && item.ProjectID != projectID || item.Parameters == nil || item.Environment == nil || item.Inputs == nil {
		return ErrInvalid
	}
	if item.Runtime != "local-docker" && item.Runtime != "e2b" || item.MaxAttempts < 1 || item.MaxAttempts > 5 || item.Limits.CPUMillis < 1 || item.Limits.MemoryBytes < 1<<20 || item.Limits.TimeoutSecond < 1 || item.Limits.TimeoutSecond > 86400 || item.Limits.DiskBytes < 1<<20 || item.Limits.PIDs < 1 || item.Limits.Network != "disabled" && item.Limits.Network != "restricted" && item.Limits.Network != "enabled" {
		return ErrInvalid
	}
	return nil
}

func validEntrypoint(value string) bool {
	if !entrypointPattern.MatchString(value) {
		return false
	}
	parts := strings.SplitN(value, ":", 2)
	for _, segment := range strings.Split(parts[1], "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func runSpec(item Experiment) map[string]interface{} {
	return map[string]interface{}{"schema_version": "1", "experiment_id": item.ID, "project_id": item.ProjectID, "source_commit": item.SourceCommit, "entrypoint": item.Entrypoint, "parameters": item.Parameters, "environment": item.Environment, "inputs": item.Inputs, "runtime": item.Runtime, "limits": item.Limits}
}
