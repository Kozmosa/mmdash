package boxcontrol

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
)

var (
	ErrInvalid   = errors.New("invalid Box request")
	ErrForbidden = errors.New("Box access forbidden")
	ErrNotFound  = errors.New("Box resource not found")
	ErrConflict  = errors.New("Box resource conflict")
	ErrNoTask    = errors.New("no Box task available")
)

var artifactPointerIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
var artifactSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Service struct {
	Access       Access
	Artifacts    ArtifactReceiver
	Clock        Clock
	Generator    IDGenerator
	Issuer       BoxCredentialIssuer
	Observer     Observer
	Revoker      BoxCredentialRevoker
	SourceSigner *SourceTransferSigner
	Sources      SourceArchiveWriter
	Store        Store
}

type Observer interface {
	TaskStatus(context.Context, Task) error
	TaskResult(context.Context, Task, Result) error
}

type ArtifactReceiver interface {
	ArchiveArtifact(context.Context, Task, string, int64, io.Reader) (map[string]interface{}, error)
}

func (service Service) Authenticate(ctx context.Context, authorization string) (auth.Identity, error) {
	return service.Access.Authenticate(ctx, authorization)
}

func (service Service) Register(ctx context.Context, registrationGrant string, box Box) (BoxRegistration, error) {
	if service.Issuer == nil || service.Generator == nil || service.Store == nil {
		return BoxRegistration{}, ErrInvalid
	}
	if err := validateRegistration(&box); err != nil {
		return BoxRegistration{}, err
	}
	boxID, err := service.Generator.New()
	if err != nil {
		return BoxRegistration{}, err
	}
	now := service.now()
	box.ID = boxID
	box.Status = StatusRegistering
	box.CreatedAt = now
	box.UpdatedAt = now
	box.ProjectAssignments = []ProjectBinding{}
	issued, err := service.Issuer.IssueBoxTokenFromRegistrationGrant(
		ctx,
		registrationGrant,
		box.Name,
		func(ctx context.Context, tx interfaceTransaction, token auth.Token) error {
			box.OwnerUserID = token.UserID
			box.TokenID = token.ID
			return service.Store.CreateInTransaction(ctx, tx, box)
		},
	)
	if err != nil {
		return BoxRegistration{}, mapAuthError(err)
	}
	box.OwnerUserID = issued.Token.UserID
	box.TokenID = issued.Token.ID
	return BoxRegistration{Box: box, Token: issued.Secret, TokenExpiresAt: issued.Token.ExpiresAt}, nil
}

// interfaceTransaction aliases the platform transaction boundary without
// allowing Box registration to escape Auth's atomic grant/token transaction.
type interfaceTransaction = transaction.Tx

func (service Service) ListOwned(ctx context.Context, identity auth.Identity) ([]Box, error) {
	if !humanIdentity(identity) || identity.User.ID == "" {
		return nil, ErrForbidden
	}
	return service.Store.ListOwned(ctx, identity.User.ID)
}

func (service Service) GetOwned(ctx context.Context, identity auth.Identity, boxID string) (Box, error) {
	box, err := service.Store.Get(ctx, strings.TrimSpace(boxID))
	if err != nil {
		return Box{}, err
	}
	if !humanIdentity(identity) || identity.User.ID != box.OwnerUserID {
		return Box{}, ErrForbidden
	}
	return box, nil
}

func (service Service) UpdateOwned(ctx context.Context, identity auth.Identity, boxID, name string) (Box, error) {
	box, err := service.GetOwned(ctx, identity, boxID)
	if err != nil {
		return Box{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || box.Status == StatusRevoked {
		return Box{}, ErrInvalid
	}
	return service.Store.UpdateName(ctx, box.ID, name, service.now())
}

func (service Service) Revoke(ctx context.Context, identity auth.Identity, boxID, mode string) (Revocation, error) {
	box, err := service.GetOwned(ctx, identity, boxID)
	if err != nil {
		return Revocation{}, err
	}
	if service.Revoker == nil || (mode != "drain" && mode != "force") {
		return Revocation{}, ErrInvalid
	}
	if mode == "drain" {
		box, active, err := service.Store.BeginDrain(ctx, box.ID, service.now())
		if err != nil {
			return Revocation{}, err
		}
		if active == 0 {
			if finalized, changed, finalizeErr := service.Store.FinalizeDrained(ctx, box.ID, service.now()); finalizeErr != nil {
				return Revocation{}, finalizeErr
			} else if changed {
				box = finalized
				if err := service.Revoker.RevokeBoxToken(ctx, box.TokenID, box.OwnerUserID); err != nil {
					return Revocation{}, err
				}
			}
		}
		return Revocation{Box: box, Mode: mode, ActiveExperiments: active}, nil
	}
	box, failed, err := service.Store.ForceRevoke(ctx, box.ID, service.now())
	if err != nil {
		return Revocation{}, err
	}
	if err := service.Revoker.RevokeBoxToken(ctx, box.TokenID, box.OwnerUserID); err != nil {
		return Revocation{}, err
	}
	for _, task := range failed {
		if service.Observer != nil {
			if err := service.Observer.TaskStatus(ctx, task); err != nil {
				return Revocation{}, err
			}
		}
	}
	return Revocation{Box: box, Mode: mode, ActiveExperiments: len(failed)}, nil
}

func (service Service) Heartbeat(ctx context.Context, identity auth.Identity, boxID string, update Box) (Heartbeat, error) {
	box, err := service.authorizeBoxIdentity(ctx, identity, boxID)
	if err != nil {
		return Heartbeat{}, err
	}
	if err := validateHeartbeat(&update, box); err != nil {
		return Heartbeat{}, err
	}
	updated, _, err := service.Store.UpdateHeartbeat(ctx, box.ID, update, service.now())
	if err != nil {
		return Heartbeat{}, err
	}
	return Heartbeat{Box: updated, CancelTaskIDs: []string{}}, nil
}

func (service Service) MarkOffline(ctx context.Context, now, heartbeatBefore time.Time, limit int) ([]Box, error) {
	if service.Store == nil {
		return nil, ErrInvalid
	}
	return service.Store.MarkOffline(ctx, now, heartbeatBefore, limit)
}

func (service Service) FailOfflineTimeouts(ctx context.Context, now, offlineBefore time.Time, limit int) ([]Task, error) {
	items, err := service.Store.FailOfflineTimeouts(ctx, now, offlineBefore, limit)
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

func (service Service) ListProject(ctx context.Context, identity auth.Identity, projectID string) ([]Box, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionBoxRead); err != nil {
		return nil, err
	}
	return service.Store.ListProject(ctx, projectID)
}

func (service Service) GetProject(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	boxID string,
) (Box, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionBoxRead); err != nil {
		return Box{}, err
	}
	box, err := service.Store.Get(ctx, strings.TrimSpace(boxID))
	if err != nil {
		return Box{}, err
	}
	for _, binding := range box.ProjectAssignments {
		if binding.ProjectID == projectID && binding.ForceUnboundAt == nil {
			return box, nil
		}
	}
	return Box{}, ErrNotFound
}

func (service Service) Assign(ctx context.Context, identity auth.Identity, projectID, boxID string) (ProjectBinding, error) {
	box, err := service.Store.Get(ctx, strings.TrimSpace(boxID))
	if err != nil {
		return ProjectBinding{}, err
	}
	if !humanIdentity(identity) || identity.User.ID != box.OwnerUserID || box.Status == StatusRevoked {
		return ProjectBinding{}, ErrForbidden
	}
	if err := service.authorize(ctx, identity, projectID, project.PermissionBoxRead); err != nil {
		return ProjectBinding{}, err
	}
	binding := ProjectBinding{AssignedAt: service.now(), AssignedBy: identity.User.ID, BoxID: box.ID, ProjectID: projectID}
	return service.Store.Assign(ctx, binding)
}

func (service Service) Unassign(ctx context.Context, identity auth.Identity, projectID, boxID string, force bool) error {
	box, err := service.Store.Get(ctx, strings.TrimSpace(boxID))
	if err != nil {
		return err
	}
	owner := humanIdentity(identity) && identity.User.ID == box.OwnerUserID
	if owner {
		if err := service.authorize(ctx, identity, projectID, project.PermissionBoxRead); err != nil {
			return err
		}
	} else if err := service.authorize(ctx, identity, projectID, project.PermissionBoxManage); err != nil {
		return err
	}
	failed, err := service.Store.Unassign(ctx, projectID, box.ID, force, service.now())
	if err != nil {
		return err
	}
	for _, task := range failed {
		if service.Observer != nil {
			if err := service.Observer.TaskStatus(ctx, task); err != nil {
				return err
			}
		}
	}
	return nil
}

// BeforeProjectMemberRemoval force-unassigns only Boxes owned by the departing
// account from this Project. The account-level Box and token remain valid for
// every other Project.
func (service Service) BeforeProjectMemberRemoval(ctx context.Context, projectID, userID string) error {
	if service.Store == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(userID) == "" {
		return ErrInvalid
	}
	boxes, err := service.Store.ListOwned(ctx, userID)
	if err != nil {
		return err
	}
	for _, box := range boxes {
		assigned := false
		for _, binding := range box.ProjectAssignments {
			if binding.ProjectID == projectID && binding.ForceUnboundAt == nil {
				assigned = true
				break
			}
		}
		if !assigned {
			continue
		}
		failed, err := service.Store.Unassign(ctx, projectID, box.ID, true, service.now())
		if err != nil {
			return err
		}
		for _, task := range failed {
			if service.Observer != nil {
				if err := service.Observer.TaskStatus(ctx, task); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (service Service) CreateTask(ctx context.Context, task Task) error {
	if task.ID == "" || task.ExperimentID == "" || task.ProjectID == "" || task.Status != TaskQueued || task.RunSpec == nil {
		return ErrInvalid
	}
	return service.Store.CreateTask(ctx, task)
}

func (service Service) Claim(ctx context.Context, identity auth.Identity, boxID string, wait time.Duration) (*Task, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return nil, err
	}
	if wait < 0 || wait > 60*time.Second {
		return nil, ErrInvalid
	}
	deadline := service.now().Add(wait)
	for {
		task, err := service.Store.ClaimTask(ctx, boxID, service.now())
		if err == nil {
			prepared, prepareErr := service.prepareClaimedTask(ctx, *task)
			if prepareErr != nil {
				return nil, prepareErr
			}
			return &prepared, nil
		}
		if !errors.Is(err, ErrNoTask) || !service.now().Before(deadline) {
			return nil, err
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (service Service) prepareClaimedTask(ctx context.Context, task Task) (Task, error) {
	if service.SourceSigner == nil || service.Sources == nil {
		return Task{}, ErrInvalid
	}
	sourceCommit, ok := stringRunSpecValue(task.RunSpec, "source_commit")
	if !ok {
		return Task{}, ErrInvalid
	}
	resultDirectory, ok := stringRunSpecValue(task.RunSpec, "result_directory")
	if !ok {
		return Task{}, ErrInvalid
	}
	transfer, err := service.SourceSigner.Sign(task, sourceCommit, service.now())
	if err != nil {
		return Task{}, err
	}
	prepared := map[string]interface{}{
		"schema_version": "2", "experiment_id": task.ExperimentID,
		"project_id": task.ProjectID, "execution_epoch": task.ExecutionEpoch,
		"source_commit": sourceCommit, "source_transfer": transfer,
		"entrypoint": task.RunSpec["entrypoint"], "parameters": task.RunSpec["parameters"],
		"environment": task.RunSpec["environment"], "inputs": task.RunSpec["inputs"],
		"runtime": task.ActualRuntime, "runtime_version": task.RuntimeVersion,
		"limits": task.RunSpec["limits"],
		"result_contract": map[string]interface{}{
			"directory": resultDirectory, "bundle_filename": "execution-bundle.zip",
			"manifest_schema":  "https://mmdash.moe/contracts/manifest.schema.json",
			"max_bundle_bytes": int64(5 << 30),
		},
	}
	if credentials, exists := task.RunSpec["readonly_credentials"]; exists {
		prepared["readonly_credentials"] = credentials
	}
	task.RunSpec = prepared
	return task, nil
}

func (service Service) Resume(ctx context.Context, identity auth.Identity, boxID, taskID string, request ResumeRequest) (Resume, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return Resume{}, err
	}
	if request.ExecutionEpoch == "" || !validTaskStatus(request.LocalPhase) || request.LastLocalSequence < 0 {
		return Resume{}, ErrInvalid
	}
	return service.Store.ResumeTask(ctx, boxID, taskID, request, service.now())
}

func (service Service) Cancel(ctx context.Context, identity auth.Identity, projectID, taskID string) (Task, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentManage); err != nil {
		return Task{}, err
	}
	return service.Store.CancelTask(ctx, taskID, service.now())
}

func (service Service) AppendLogs(ctx context.Context, identity auth.Identity, boxID, taskID string, batch LogBatch) (LogAcknowledgement, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return LogAcknowledgement{}, err
	}
	if batch.ExecutionEpoch == "" || batch.FirstSequence < 1 || len(batch.Entries) > 500 || (!batch.LogsTruncated && len(batch.Entries) == 0) {
		return LogAcknowledgement{}, ErrInvalid
	}
	for index := range batch.Entries {
		entry := &batch.Entries[index]
		if entry.Sequence != batch.FirstSequence+int64(index) || entry.Message == "" || len(entry.Message) > 20000 ||
			(entry.Stream != "stdout" && entry.Stream != "stderr" && entry.Stream != "system") {
			return LogAcknowledgement{}, ErrInvalid
		}
	}
	if batch.LogsTruncated && batch.TruncatedAt == nil {
		return LogAcknowledgement{}, ErrInvalid
	}
	return service.Store.AppendLogs(ctx, boxID, taskID, batch, service.now())
}

func (service Service) ReportStatus(ctx context.Context, identity auth.Identity, boxID, taskID, executionEpoch, status string, occurredAt time.Time, exitCode *int, failure *Failure, usage map[string]interface{}, summary string) (Task, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return Task{}, err
	}
	if executionEpoch == "" || !boxReportableTaskStatus(status) || occurredAt.IsZero() {
		return Task{}, ErrInvalid
	}
	if (status == TaskFailed || status == TaskTimedOut) && failure == nil {
		return Task{}, ErrInvalid
	}
	task, err := service.Store.ReportStatus(ctx, boxID, taskID, executionEpoch, status, occurredAt, exitCode, failure, usage, strings.TrimSpace(summary))
	if err == nil && service.Observer != nil {
		err = service.Observer.TaskStatus(ctx, task)
	}
	if err == nil {
		err = service.finalizeDrain(ctx, boxID)
	}
	return task, err
}

func (service Service) UploadArtifact(ctx context.Context, identity auth.Identity, boxID, taskID, executionEpoch, expectedSHA string, expectedSize int64, input io.Reader) (map[string]interface{}, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return nil, err
	}
	if service.Artifacts == nil || executionEpoch == "" || expectedSize < 1 || expectedSize > 5<<30 || !artifactSHA256Pattern.MatchString(expectedSHA) {
		return nil, ErrInvalid
	}
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil || task.BoxID != boxID || task.ExecutionEpoch != executionEpoch {
		return nil, ErrNotFound
	}
	return service.Artifacts.ArchiveArtifact(ctx, task, expectedSHA, expectedSize, input)
}

func (service Service) SubmitResult(ctx context.Context, identity auth.Identity, boxID, taskID string, result Result) (Task, error) {
	if _, err := service.authorizeBoxIdentity(ctx, identity, boxID); err != nil {
		return Task{}, err
	}
	if result.ExecutionEpoch == "" || !artifactSHA256Pattern.MatchString(result.ManifestSHA256) || !validArtifactPointer(result.ExecutionBundle) {
		return Task{}, ErrInvalid
	}
	task, err := service.Store.SubmitResult(ctx, boxID, taskID, result, service.now())
	if err == nil && service.Observer != nil {
		err = service.Observer.TaskResult(ctx, task, result)
	}
	if err == nil {
		err = service.finalizeDrain(ctx, boxID)
	}
	return task, err
}

func (service Service) Logs(ctx context.Context, identity auth.Identity, projectID, taskID string, afterSequence int64, limit int) ([]Log, bool, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentRead); err != nil {
		return nil, false, err
	}
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil || task.ProjectID != projectID {
		return nil, false, ErrNotFound
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 || afterSequence < 0 {
		return nil, false, ErrInvalid
	}
	return service.Store.ListLogs(ctx, taskID, afterSequence, limit)
}

func (service Service) TailLogs(ctx context.Context, identity auth.Identity, projectID, taskID string, limit int) ([]Log, bool, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionExperimentRead); err != nil {
		return nil, false, err
	}
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil || task.ProjectID != projectID {
		return nil, false, ErrNotFound
	}
	if limit <= 0 {
		return []Log{}, false, nil
	}
	if limit > 500 {
		return nil, false, ErrInvalid
	}
	store, ok := service.Store.(interface {
		TailLogs(context.Context, string, int) ([]Log, bool, error)
	})
	if !ok {
		return nil, false, ErrInvalid
	}
	return store.TailLogs(ctx, taskID, limit)
}

func (service Service) ReadTask(ctx context.Context, identity auth.Identity, projectID, taskID string) (Task, error) {
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil || task.ProjectID != projectID {
		return Task{}, ErrNotFound
	}
	if err := service.authorize(ctx, identity, task.ProjectID, project.PermissionExperimentRead); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (service Service) authorizeBoxIdentity(ctx context.Context, identity auth.Identity, boxID string) (Box, error) {
	if identity.Kind != "box" || identity.TokenID == "" {
		return Box{}, ErrForbidden
	}
	box, err := service.Store.Get(ctx, strings.TrimSpace(boxID))
	if err != nil {
		return Box{}, err
	}
	if identity.TokenID != box.TokenID || identity.User.ID != box.OwnerUserID || box.Status == StatusRevoked || box.LegacyReauthorizationRequired {
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

func (service Service) finalizeDrain(ctx context.Context, boxID string) error {
	if service.Revoker == nil {
		return nil
	}
	box, changed, err := service.Store.FinalizeDrained(ctx, boxID, service.now())
	if err != nil || !changed {
		return err
	}
	return service.Revoker.RevokeBoxToken(ctx, box.TokenID, box.OwnerUserID)
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock.Now().UTC()
}

func humanIdentity(identity auth.Identity) bool {
	return identity.Kind == "session" || identity.Kind == "api"
}

func validateRegistration(box *Box) error {
	box.Name = strings.TrimSpace(box.Name)
	box.Version = strings.TrimSpace(box.Version)
	box.InstallationID = strings.TrimSpace(box.InstallationID)
	if box.Name == "" || len(box.Name) > 200 || box.Version == "" || len(box.Version) > 100 ||
		box.InstallationID == "" || len(box.InstallationID) > 200 {
		return ErrInvalid
	}
	if box.Capabilities == nil {
		box.Capabilities = []Capability{}
	}
	if box.Runtimes == nil {
		box.Runtimes = []Runtime{}
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

func validateHeartbeat(update *Box, registered Box) error {
	update.Name = registered.Name
	update.InstallationID = registered.InstallationID
	if err := validateRegistration(update); err != nil {
		return err
	}
	return nil
}

func validateLimits(limits ResourceLimits) error {
	if limits.CPUMillis < 1 || limits.MemoryBytes < 1<<20 || limits.TimeoutSecond < 1 ||
		limits.TimeoutSecond > 86400 || limits.DiskBytes < 1<<20 || limits.PIDs < 1 {
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
	case TaskPreparing, TaskRunning, TaskUploading, TaskProcessingResult,
		TaskSucceeded, TaskFailed, TaskCanceled, TaskTimedOut:
		return true
	default:
		return false
	}
}

func boxReportableTaskStatus(status string) bool {
	switch status {
	case TaskPreparing, TaskRunning, TaskUploading, TaskFailed, TaskCanceled, TaskTimedOut:
		return true
	default:
		return false
	}
}

func validArtifactPointer(value ArtifactPointer) bool {
	return artifactPointerIDPattern.MatchString(value.ArtifactID) &&
		artifactPointerIDPattern.MatchString(value.VersionID) &&
		value.Filename == "execution-bundle.zip" &&
		artifactSHA256Pattern.MatchString(value.SHA256) && value.SizeBytes > 0
}

func ValidateArtifactPointer(value map[string]interface{}) bool {
	return validArtifactPointer(artifactPointerFromMap(value))
}

func artifactPointerFromMap(value map[string]interface{}) ArtifactPointer {
	result := ArtifactPointer{}
	result.ArtifactID, _ = value["artifact_id"].(string)
	result.VersionID, _ = value["version_id"].(string)
	result.Filename, _ = value["filename"].(string)
	result.SHA256, _ = value["sha256"].(string)
	switch size := value["size_bytes"].(type) {
	case int:
		result.SizeBytes = int64(size)
	case int64:
		result.SizeBytes = size
	case float64:
		result.SizeBytes = int64(size)
	}
	return result
}

func mapAuthError(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalid):
		return ErrInvalid
	case errors.Is(err, auth.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, auth.ErrConflict), errors.Is(err, auth.ErrAuthorizationExpired):
		return ErrConflict
	default:
		return err
	}
}
