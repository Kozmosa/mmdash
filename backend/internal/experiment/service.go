package experiment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/artifact"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/boxcontrol"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/repo"
)

var (
	ErrInvalid   = errors.New("invalid experiment request")
	ErrForbidden = errors.New("experiment access forbidden")
	ErrNotFound  = errors.New("experiment not found")
	ErrConflict  = errors.New("experiment state conflict")
	ErrNoResult  = errors.New("experiment has no result")
)

var (
	commitPattern     = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	entrypointPattern = regexp.MustCompile(`^(python3?|node|go|binary):[a-zA-Z0-9_./-]+$`)
)

type ProjectAccess interface {
	Authenticate(context.Context, string) (auth.Identity, error)
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

type CommitValidator interface {
	ValidateCommit(context.Context, auth.Identity, string, string) error
}

type ResultVerifier interface {
	VerifySelfResult(context.Context, auth.Identity, Experiment, string) (ResultVerification, error)
}

type JobAccess interface {
	ClaimedWorkerJob(context.Context, auth.Identity, string) (jobs.Job, error)
}

type ResultArtifactAccess interface {
	ExperimentResultGrant(context.Context, string, string, string, string) (map[string]interface{}, error)
	OpenExperimentResult(context.Context, string, string, string, string) (io.ReadCloser, artifact.Version, error)
	ArchiveExperimentFile(context.Context, string, string, string, []string, string, string, string, int64, io.Reader) (artifact.Detail, error)
}

type ResultRepoAccess interface {
	ResolveHead(context.Context, string) (repo.Revision, error)
	Commit(context.Context, repo.ResultCommitRequest) (repo.CommitResult, error)
	Revert(context.Context, repo.ResultRevertRequest) (repo.CommitResult, error)
}

type IDGenerator interface{ New() (string, error) }
type Clock interface{ Now() time.Time }

type Service struct {
	Artifacts       ArtifactArchiver
	Access          ProjectAccess
	Boxes           *boxcontrol.Service
	Clock           Clock
	Commit          CommitValidator
	Generator       IDGenerator
	JobAccess       JobAccess
	ResultArtifacts ResultArtifactAccess
	ResultRepo      ResultRepoAccess
	Results         ResultVerifier
	Store           Store
}

type ArtifactArchiver interface {
	ArchiveExperimentResult(context.Context, string, string, string, []string, string, int64, io.Reader) (artifact.Detail, error)
}

func (service Service) Authenticate(ctx context.Context, authorization string) (auth.Identity, error) {
	return service.Access.Authenticate(ctx, authorization)
}

func (service Service) GetSettings(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (Settings, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentRead); err != nil {
		return Settings{}, err
	}
	return service.Store.GetSettings(ctx, projectID)
}

func (service Service) UpdateSettings(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	patch SettingsPatch,
) (Settings, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Settings{}, err
	}
	if !humanIdentity(identity) || !validSettingsPatch(patch) {
		return Settings{}, ErrInvalid
	}
	return service.Store.UpdateSettings(ctx, projectID, identity.User.ID, patch, service.now())
}

func (service Service) Create(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	input Experiment,
) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Experiment{}, err
	}
	if service.Generator == nil || service.Store == nil {
		return Experiment{}, ErrInvalid
	}
	settings, err := service.Store.GetSettings(ctx, projectID)
	if err != nil {
		return Experiment{}, err
	}
	applyDefaults(&input, settings)
	if err := validateExperiment(&input, projectID, false); err != nil {
		return Experiment{}, err
	}
	if service.Commit != nil {
		if err := service.Commit.ValidateCommit(ctx, identity, projectID, input.SourceCommit); err != nil {
			return Experiment{}, ErrInvalid
		}
	}
	input.ID, err = service.Generator.New()
	if err != nil {
		return Experiment{}, err
	}
	now := service.now()
	input.ProjectID, input.CreatedBy = projectID, identity.User.ID
	input.ProjectTimezone = settings.Timezone
	input.ResultDirectory, err = resultDirectory(input.ID, now, settings.Timezone)
	if err != nil {
		return Experiment{}, ErrInvalid
	}
	input.ExecutionStatus = StatusCreated
	if input.Type == TypeSelf {
		input.ExecutionStatus = StatusAwaitingResult
	}
	input.MaxAttempts = 1
	input.CreatedAt, input.UpdatedAt = now, now
	input.Retry = Retry{RootExperimentID: input.ID, LatestExperimentID: input.ID}
	input.GitLargeFileThreshold = settings.GitLargeFileThresholdBytes
	created, _, err := service.Store.Create(ctx, input)
	return decorate(created), err
}

func (service Service) Rerun(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	experimentID string,
	overrides RerunOverrides,
) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Experiment{}, err
	}
	if !humanIdentity(identity) || strings.TrimSpace(overrides.IdempotencyKey) == "" ||
		len(overrides.IdempotencyKey) > 200 || service.Generator == nil {
		return Experiment{}, ErrInvalid
	}
	previous, err := service.Store.Get(ctx, projectID, experimentID)
	if err != nil {
		return Experiment{}, err
	}
	if previous.Type == TypeSelf || !terminalStatus(previous.ExecutionStatus) || previous.ExecutionStatus == StatusArchived {
		return Experiment{}, ErrConflict
	}
	next := previous
	next.ID, err = service.Generator.New()
	if err != nil {
		return Experiment{}, err
	}
	next.Type, next.ExecutionStatus = TypeBoxRe, StatusCreated
	next.CreatedBy = identity.User.ID
	next.IdempotencyKey = strings.TrimSpace(overrides.IdempotencyKey)
	next.RunIdempotencyKey, next.ResultBindIdempotency = "", ""
	next.BoxID, next.TaskID, next.ActualRuntime, next.RuntimeVersion = "", "", "", ""
	next.ConnectivityStatus, next.ResultCommitSHA = "", ""
	next.StagingCommitSHA, next.RevertCommitSHA = "", ""
	next.ExitCode, next.Failure, next.ExecutionBundle = nil, nil, nil
	next.ResourceUsage, next.ResultManifest = map[string]interface{}{}, nil
	next.Summary, next.ResultAnalysis, next.ResultManifestSHA256 = "", "", ""
	next.LogsTruncated, next.LogsTruncatedAt = false, nil
	applyRerunOverrides(&next, overrides)
	if err := validateExperiment(&next, projectID, true); err != nil {
		return Experiment{}, err
	}
	if service.Commit != nil && next.SourceCommit != previous.SourceCommit {
		if err := service.Commit.ValidateCommit(ctx, identity, projectID, next.SourceCommit); err != nil {
			return Experiment{}, ErrInvalid
		}
	}
	now := service.now()
	next.ResultDirectory, err = resultDirectory(next.ID, now, previous.ProjectTimezone)
	if err != nil {
		return Experiment{}, ErrInvalid
	}
	next.CreatedAt, next.UpdatedAt, next.StartedAt, next.FinishedAt = now, now, nil, nil
	next.Retry = Retry{
		RetryOfExperimentID: previous.ID,
		RootExperimentID:    previous.Retry.RootExperimentID,
		LatestExperimentID:  next.ID,
		RetrySequence:       previous.Retry.RetrySequence + 1,
	}
	created, _, err := service.Store.CreateRerun(ctx, previous, next, now)
	return decorate(created), err
}

func (service Service) List(
	ctx context.Context,
	identity auth.Identity,
	projectID, status, cursor string,
	limit int,
) (Page, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentRead); err != nil {
		return Page{}, err
	}
	if status != "" && !validStatus(status) {
		return Page{}, ErrInvalid
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
	page, err := service.Store.List(ctx, projectID, status, offset, limit)
	for index := range page.Items {
		page.Items[index] = decorate(page.Items[index])
	}
	return page, err
}

func (service Service) Get(
	ctx context.Context,
	identity auth.Identity,
	projectID, experimentID string,
) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentRead); err != nil {
		return Experiment{}, err
	}
	item, err := service.Store.Get(ctx, projectID, experimentID)
	return decorate(item), err
}

func (service Service) Run(
	ctx context.Context,
	identity auth.Identity,
	projectID, experimentID, idempotencyKey string,
) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Experiment{}, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return Experiment{}, ErrInvalid
	}
	item, err := service.Store.Get(ctx, projectID, experimentID)
	if err != nil {
		return Experiment{}, err
	}
	if item.Type == TypeSelf {
		return Experiment{}, ErrConflict
	}
	if item.ExecutionStatus != StatusCreated {
		if item.RunIdempotencyKey == idempotencyKey {
			return decorate(item), nil
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
	now := service.now()
	task := boxcontrol.Task{
		ID: taskID, ExperimentID: item.ID, ProjectID: projectID,
		Status: boxcontrol.TaskQueued, Attempt: 0, MaxAttempts: 1,
		RunSpec: runSpec(item), CreatedAt: now, UpdatedAt: now,
	}
	queued, err := service.Store.QueueWithTask(ctx, item, task, idempotencyKey, now)
	return decorate(queued), err
}

func (service Service) Cancel(
	ctx context.Context,
	identity auth.Identity,
	projectID, experimentID string,
) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Experiment{}, err
	}
	item, err := service.Store.Cancel(ctx, projectID, experimentID, service.now())
	return decorate(item), err
}

func (service Service) Archive(
	ctx context.Context,
	identity auth.Identity,
	projectID, experimentID string,
) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Experiment{}, err
	}
	item, err := service.Store.Archive(ctx, projectID, experimentID, service.now())
	return decorate(item), err
}

func (service Service) BindResult(
	ctx context.Context,
	identity auth.Identity,
	projectID, experimentID, commitSHA, idempotencyKey string,
) (Experiment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Experiment{}, err
	}
	commitSHA, idempotencyKey = strings.TrimSpace(commitSHA), strings.TrimSpace(idempotencyKey)
	if !commitPattern.MatchString(commitSHA) || idempotencyKey == "" || len(idempotencyKey) > 200 {
		return Experiment{}, ErrInvalid
	}
	item, err := service.Store.BeginSelfBinding(
		ctx, projectID, experimentID, commitSHA, idempotencyKey, service.now(),
	)
	if err != nil {
		return Experiment{}, err
	}
	if item.ExecutionStatus == StatusSucceeded || service.Results == nil {
		return decorate(item), nil
	}
	verified, err := service.Results.VerifySelfResult(ctx, identity, item, commitSHA)
	if err != nil {
		failure := Failure{
			Stage: "result_binding", Code: "RESULT_COMMIT_VERIFICATION_FAILED",
			Message: err.Error(), FailedAt: service.now(), Retryable: true,
			CleanupResult: map[string]interface{}{},
		}
		failed, storeErr := service.Store.FailResult(ctx, projectID, experimentID, failure, service.now())
		if storeErr != nil {
			return Experiment{}, storeErr
		}
		return decorate(failed), nil
	}
	completed, err := service.Store.CompleteResult(ctx, projectID, experimentID, verified, service.now())
	return decorate(completed), err
}

func (service Service) Logs(
	ctx context.Context,
	identity auth.Identity,
	projectID, experimentID string,
	afterSequence int64,
	limit int,
) ([]boxcontrol.Log, bool, error) {
	item, err := service.Get(ctx, identity, projectID, experimentID)
	if err != nil {
		return nil, false, err
	}
	if item.Type == TypeSelf || item.TaskID == "" {
		return []boxcontrol.Log{}, false, nil
	}
	if service.Boxes == nil {
		return nil, false, ErrInvalid
	}
	return service.Boxes.Logs(ctx, identity, projectID, item.TaskID, afterSequence, limit)
}

func (service Service) TailLogs(
	ctx context.Context,
	identity auth.Identity,
	projectID, experimentID string,
	limit int,
) ([]boxcontrol.Log, bool, error) {
	item, err := service.Get(ctx, identity, projectID, experimentID)
	if err != nil {
		return nil, false, err
	}
	if item.Type == TypeSelf || item.TaskID == "" || limit == 0 {
		return []boxcontrol.Log{}, false, nil
	}
	if service.Boxes == nil {
		return nil, false, ErrInvalid
	}
	return service.Boxes.TailLogs(ctx, identity, projectID, item.TaskID, limit)
}

func (service Service) Result(
	ctx context.Context,
	identity auth.Identity,
	projectID, experimentID string,
) (ResultBundle, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentRead); err != nil {
		return ResultBundle{}, err
	}
	return service.Store.Result(ctx, projectID, experimentID)
}

func (service Service) Compare(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	ids []string,
) (Comparison, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentRead); err != nil {
		return Comparison{}, err
	}
	if len(ids) < 2 || len(ids) > 20 {
		return Comparison{}, ErrInvalid
	}
	comparison, err := service.Store.Compare(ctx, projectID, ids)
	for index := range comparison.Items {
		comparison.Items[index] = decorate(comparison.Items[index])
	}
	return comparison, err
}

func (service *Service) TaskStatus(ctx context.Context, task boxcontrol.Task) error {
	if service == nil || service.Store == nil {
		return ErrInvalid
	}
	item, err := service.Store.ApplyTaskStatus(ctx, task, service.now())
	if err != nil {
		return err
	}
	forceInvalidation := task.Status == boxcontrol.TaskFailed && task.Failure != nil &&
		(task.Failure.Code == "BOX_FORCE_REVOKED" || task.Failure.Code == "BOX_PROJECT_FORCE_UNASSIGNED")
	if forceInvalidation && item.StagingCommitSHA != "" && item.ResultCommitSHA == "" && item.RevertCommitSHA == "" {
		if service.ResultRepo == nil || len(item.StagingPaths) == 0 {
			return ErrInvalid
		}
		return service.revertResultCommit(ctx, item, item.StagingCommitSHA, item.StagingPaths)
	}
	return nil
}

func (service *Service) TaskResult(
	ctx context.Context,
	task boxcontrol.Task,
	result boxcontrol.Result,
) error {
	if service == nil || service.Store == nil ||
		result.ExecutionBundle.ArtifactID == "" || result.ManifestSHA256 == "" {
		return ErrInvalid
	}
	_, err := service.Store.ApplyResult(ctx, task, result, service.now())
	return err
}

// ArchiveArtifact is the only Core-side path by which a Box may turn its
// immutable execution-bundle.zip into the authoritative raw Artifact pointer.
func (service *Service) ArchiveArtifact(
	ctx context.Context,
	task boxcontrol.Task,
	expectedSHA string,
	expectedSize int64,
	input io.Reader,
) (map[string]interface{}, error) {
	if service == nil || service.Store == nil || service.Artifacts == nil {
		return nil, ErrInvalid
	}
	item, err := service.Store.Get(ctx, task.ProjectID, task.ExperimentID)
	if err != nil {
		return nil, err
	}
	detail, err := service.Artifacts.ArchiveExperimentResult(
		ctx, task.ProjectID, task.ExperimentID, item.CreatedBy,
		experimentArtifactFolder(item), expectedSHA, expectedSize, input,
	)
	if err != nil {
		return nil, err
	}
	if detail.CurrentVersion == nil {
		return nil, ErrNoResult
	}
	return map[string]interface{}{
		"artifact_id": detail.Artifact.ID,
		"version_id":  detail.CurrentVersion.ID,
		"filename":    detail.CurrentVersion.Filename,
		"sha256":      detail.CurrentVersion.SHA256,
		"size_bytes":  detail.CurrentVersion.SizeBytes,
	}, nil
}

func (service Service) authorize(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	permission project.Permission,
) error {
	if strings.TrimSpace(projectID) == "" || service.Access == nil ||
		(identity.ProjectID != "" && identity.ProjectID != projectID) {
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

func applyDefaults(item *Experiment, settings Settings) {
	item.Type = strings.TrimSpace(item.Type)
	if item.RequestedRuntimePolicy == "" {
		item.RequestedRuntimePolicy = settings.DefaultRuntimePolicy
	}
	if item.Limits == (ResourceLimits{}) {
		item.Limits = settings.DefaultLimits
	}
	item.GitLargeFileThreshold = settings.GitLargeFileThresholdBytes
}

func validateExperiment(item *Experiment, projectID string, rerun bool) error {
	item.Name = strings.TrimSpace(item.Name)
	item.SourceCommit = strings.TrimSpace(item.SourceCommit)
	item.Entrypoint = strings.TrimSpace(item.Entrypoint)
	item.RequestedRuntimePolicy = strings.TrimSpace(item.RequestedRuntimePolicy)
	item.RequestedBoxID = strings.TrimSpace(item.RequestedBoxID)
	item.IdempotencyKey = strings.TrimSpace(item.IdempotencyKey)
	if item.Name == "" || len(item.Name) > 200 || item.IdempotencyKey == "" ||
		len(item.IdempotencyKey) > 200 || !commitPattern.MatchString(item.SourceCommit) ||
		!validEntrypoint(item.Entrypoint) || item.ProjectID != "" && item.ProjectID != projectID ||
		item.Parameters == nil || item.Environment == nil || item.Inputs == nil {
		return ErrInvalid
	}
	if rerun {
		if item.Type != TypeBoxRe {
			return ErrInvalid
		}
	} else if item.Type != TypeBox && item.Type != TypeSelf {
		return ErrInvalid
	}
	if item.RequestedRuntimePolicy != "auto" && item.RequestedRuntimePolicy != "local-docker" &&
		item.RequestedRuntimePolicy != "e2b" && item.RequestedRuntimePolicy != "local-process" ||
		!validLimits(item.Limits) {
		return ErrInvalid
	}
	if item.Type == TypeSelf && item.RequestedBoxID != "" {
		return ErrInvalid
	}
	return nil
}

func validSettingsPatch(patch SettingsPatch) bool {
	if patch.Timezone == nil && patch.DefaultRuntimePolicy == nil &&
		patch.DefaultLimits == nil && patch.GitLargeFileThresholdBytes == nil {
		return false
	}
	if patch.Timezone != nil {
		value := strings.TrimSpace(*patch.Timezone)
		if value == "" || len(value) > 100 {
			return false
		}
		if _, err := time.LoadLocation(value); err != nil {
			return false
		}
		*patch.Timezone = value
	}
	if patch.DefaultRuntimePolicy != nil {
		value := strings.TrimSpace(*patch.DefaultRuntimePolicy)
		if value != "auto" && value != "local-docker" && value != "e2b" && value != "local-process" {
			return false
		}
		*patch.DefaultRuntimePolicy = value
	}
	if patch.DefaultLimits != nil && !validLimits(*patch.DefaultLimits) {
		return false
	}
	return patch.GitLargeFileThresholdBytes == nil ||
		(*patch.GitLargeFileThresholdBytes >= 1 && *patch.GitLargeFileThresholdBytes <= 5<<30)
}

func validLimits(limits ResourceLimits) bool {
	return limits.CPUMillis >= 1 && limits.MemoryBytes >= 1<<20 &&
		limits.TimeoutSecond >= 1 && limits.TimeoutSecond <= 86400 &&
		limits.DiskBytes >= 1<<20 && limits.PIDs >= 1 &&
		(limits.Network == "disabled" || limits.Network == "restricted" || limits.Network == "enabled")
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

func resultDirectory(experimentID string, createdAt time.Time, timezone string) (string, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"experiments/%s_%s/", experimentID,
		createdAt.In(location).Format("20060102_1504"),
	), nil
}

func applyRerunOverrides(item *Experiment, overrides RerunOverrides) {
	if overrides.Name != nil {
		item.Name = *overrides.Name
	}
	if overrides.SourceCommit != nil {
		item.SourceCommit = *overrides.SourceCommit
	}
	if overrides.Entrypoint != nil {
		item.Entrypoint = *overrides.Entrypoint
	}
	if overrides.Parameters != nil {
		item.Parameters = *overrides.Parameters
	}
	if overrides.Environment != nil {
		item.Environment = *overrides.Environment
	}
	if overrides.Inputs != nil {
		item.Inputs = *overrides.Inputs
	}
	if overrides.RequestedRuntimePolicy != nil {
		item.RequestedRuntimePolicy = *overrides.RequestedRuntimePolicy
	}
	if overrides.RequestedBoxID != nil {
		item.RequestedBoxID = *overrides.RequestedBoxID
	}
	if overrides.Limits != nil {
		item.Limits = *overrides.Limits
	}
}

func runSpec(item Experiment) map[string]interface{} {
	return map[string]interface{}{
		"schema_version": "2", "experiment_id": item.ID,
		"project_id": item.ProjectID, "source_commit": item.SourceCommit,
		"entrypoint": item.Entrypoint, "parameters": item.Parameters,
		"environment": item.Environment, "inputs": item.Inputs,
		"runtime_policy":   item.RequestedRuntimePolicy,
		"requested_box_id": item.RequestedBoxID, "limits": item.Limits,
		"result_directory": item.ResultDirectory,
	}
}

func validStatus(status string) bool {
	switch status {
	case StatusCreated, StatusQueued, StatusPreparing, StatusRunning, StatusUploading,
		StatusProcessingResult, StatusAwaitingResult, StatusVerifyingResult,
		StatusSucceeded, StatusFailed, StatusCanceled, StatusTimedOut, StatusArchived:
		return true
	default:
		return false
	}
}

func terminalStatus(status string) bool {
	return status == StatusSucceeded || status == StatusFailed || status == StatusCanceled ||
		status == StatusTimedOut || status == StatusArchived
}

func humanIdentity(identity auth.Identity) bool {
	return identity.Kind == "session" || identity.Kind == "api"
}
