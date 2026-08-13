package artifact

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
)

const (
	semanticDescriptionJobType = "artifact.semantic.describe"
	semanticDescriptionLimit   = 20_000
	semanticUsageItemLimit     = 500
	semanticUsageLimit         = 16
)

// SemanticJobAccess is the narrow queue capability used by Artifact. The
// browser never receives a model credential and the Worker only receives a
// durable Job identifier.
type SemanticJobAccess interface {
	Create(context.Context, auth.Identity, jobs.CreateInput) (jobs.Job, bool, error)
}

// SemanticDescriptionStore applies a successful result in the same
// transaction as Job completion.
type SemanticDescriptionStore interface {
	CompleteSemanticDescriptionInTransaction(context.Context, transaction.Tx, jobs.Job, SemanticDescriptionResult, time.Time) error
}

// SemanticDescriptionModel is implemented by Agent. Artifact deliberately
// depends on this runtime-neutral capability instead of a provider SDK.
type SemanticDescriptionModel interface {
	DescribeArtifact(context.Context, string, string, string, SemanticDescriptionModelInput) (SemanticDescriptionResult, error)
}

type SemanticDescriptionInput struct {
	AgentInstanceID string `json:"agent_instance_id,omitempty"`
}

type SemanticDescriptionJob struct {
	JobID      string      `json:"job_id"`
	ProjectID  string      `json:"project_id"`
	ArtifactID string      `json:"artifact_id"`
	VersionID  string      `json:"version_id"`
	Status     jobs.Status `json:"status"`
}

type SemanticDescriptionModelInput struct {
	ArtifactID string `json:"artifact_id"`
	VersionID  string `json:"version_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Filename   string `json:"filename"`
	MIMEType   string `json:"mime_type"`
	SizeBytes  int64  `json:"size_bytes"`
}

type SemanticDescriptionResult struct {
	Description      string   `json:"description"`
	RecommendedUsage []string `json:"recommended_usage"`
	AgentSessionID   string   `json:"agent_session_id"`
	AgentRunID       string   `json:"agent_run_id"`
}

// RequestSemanticDescription schedules a regenerable, non-blocking model
// request against the current immutable Artifact Version.
func (service Service) RequestSemanticDescription(ctx context.Context, caller auth.Identity, projectID, artifactID string, input SemanticDescriptionInput) (SemanticDescriptionJob, error) {
	if caller.Kind != "user" || service.SemanticJobs == nil {
		return SemanticDescriptionJob{}, ErrForbidden
	}
	if err := service.authorize(ctx, caller, projectID, project.PermissionArtifactUpload); err != nil {
		return SemanticDescriptionJob{}, err
	}
	input.AgentInstanceID = strings.TrimSpace(input.AgentInstanceID)
	if input.AgentInstanceID != "" && !uuidPattern.MatchString(input.AgentInstanceID) {
		return SemanticDescriptionJob{}, ErrInvalid
	}
	detail, err := service.Store.GetDetail(ctx, projectID, artifactID, false)
	if err != nil || detail.Artifact.Status != StatusAvailable || detail.CurrentVersion == nil || detail.CurrentVersion.Status != StatusAvailable {
		return SemanticDescriptionJob{}, ErrNotAvailable
	}
	version := detail.CurrentVersion
	job, _, err := service.SemanticJobs.Create(ctx, caller, jobs.CreateInput{
		JobType: semanticDescriptionJobType, ProjectID: projectID,
		MaxAttempts: 2, TimeoutSeconds: 900,
		Payload: map[string]interface{}{
			"project_id": projectID, "artifact_id": artifactID,
			"version_id": version.ID, "agent_instance_id": input.AgentInstanceID,
		},
	})
	if err != nil {
		return SemanticDescriptionJob{}, err
	}
	service.record(ctx, "artifact.semantic_description.requested", "success", projectID, artifactID, map[string]interface{}{"job_id": job.ID, "version_id": version.ID})
	return SemanticDescriptionJob{JobID: job.ID, ProjectID: projectID, ArtifactID: artifactID, VersionID: version.ID, Status: job.Status}, nil
}

// ExecuteSemanticDescription is callable only through a live Worker Job
// lease. Core resolves the immutable target and invokes Agent itself.
func (service Service) ExecuteSemanticDescription(ctx context.Context, caller auth.Identity, jobID string) (SemanticDescriptionResult, error) {
	if service.Jobs == nil || service.SemanticModel == nil {
		return SemanticDescriptionResult{}, ErrNotAvailable
	}
	job, err := service.Jobs.ClaimedWorkerJob(ctx, caller, jobID)
	if err != nil {
		return SemanticDescriptionResult{}, mapJobError(err)
	}
	target, err := semanticTarget(job)
	if err != nil {
		return SemanticDescriptionResult{}, err
	}
	detail, err := service.Store.GetDetail(ctx, target.ProjectID, target.ArtifactID, false)
	if err != nil || detail.Artifact.Status != StatusAvailable || detail.CurrentVersion == nil || detail.CurrentVersion.ID != target.VersionID || detail.CurrentVersion.Status != StatusAvailable {
		return SemanticDescriptionResult{}, ErrNotAvailable
	}
	result, err := service.SemanticModel.DescribeArtifact(ctx, target.ProjectID, target.AgentInstanceID, job.ID, SemanticDescriptionModelInput{
		ArtifactID: target.ArtifactID, VersionID: target.VersionID, Name: detail.Artifact.Name,
		Kind: detail.Artifact.Kind, Filename: detail.CurrentVersion.Filename,
		MIMEType: detail.CurrentVersion.MIMEType, SizeBytes: detail.CurrentVersion.SizeBytes,
	})
	if err != nil {
		return SemanticDescriptionResult{}, safe("ARTIFACT_SEMANTIC_UNAVAILABLE", "Semantic description could not be generated", err)
	}
	if err := validateSemanticResult(result); err != nil {
		return SemanticDescriptionResult{}, err
	}
	return result, nil
}

type semanticDescriptionTarget struct{ ProjectID, ArtifactID, VersionID, AgentInstanceID string }

func semanticTarget(job jobs.Job) (semanticDescriptionTarget, error) {
	if job.JobType != semanticDescriptionJobType || job.Payload == nil {
		return semanticDescriptionTarget{}, ErrNotFound
	}
	target := semanticDescriptionTarget{
		ProjectID: payloadString(job.Payload, "project_id"), ArtifactID: payloadString(job.Payload, "artifact_id"),
		VersionID: payloadString(job.Payload, "version_id"), AgentInstanceID: payloadString(job.Payload, "agent_instance_id"),
	}
	if target.ProjectID != job.ProjectID || !uuidPattern.MatchString(target.ProjectID) || !uuidPattern.MatchString(target.ArtifactID) || !uuidPattern.MatchString(target.VersionID) || (target.AgentInstanceID != "" && !uuidPattern.MatchString(target.AgentInstanceID)) {
		return semanticDescriptionTarget{}, ErrInvalid
	}
	return target, nil
}

func validateSemanticResult(result SemanticDescriptionResult) error {
	result.Description = strings.TrimSpace(result.Description)
	if result.Description == "" || len(result.Description) > semanticDescriptionLimit || !uuidPattern.MatchString(result.AgentSessionID) || !uuidPattern.MatchString(result.AgentRunID) || len(result.RecommendedUsage) < 1 || len(result.RecommendedUsage) > semanticUsageLimit {
		return jobs.ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, value := range result.RecommendedUsage {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > semanticUsageItemLimit {
			return jobs.ErrInvalid
		}
		if _, exists := seen[value]; exists {
			return jobs.ErrInvalid
		}
		seen[value] = struct{}{}
	}
	return nil
}

func (service Service) completeSemanticDescription(ctx context.Context, tx transaction.Tx, job jobs.Job, raw map[string]interface{}) error {
	if service.SemanticStore == nil {
		return errors.New("Artifact semantic store is not configured")
	}
	result, err := parseSemanticResult(raw)
	if err != nil {
		return err
	}
	return service.SemanticStore.CompleteSemanticDescriptionInTransaction(ctx, tx, job, result, service.now())
}

func parseSemanticResult(raw map[string]interface{}) (SemanticDescriptionResult, error) {
	result := SemanticDescriptionResult{
		Description: payloadString(raw, "description"), AgentSessionID: payloadString(raw, "agent_session_id"), AgentRunID: payloadString(raw, "agent_run_id"),
	}
	values, ok := raw["recommended_usage"].([]interface{})
	if !ok {
		if stringsValues, exact := raw["recommended_usage"].([]string); exact {
			result.RecommendedUsage = append([]string(nil), stringsValues...)
		} else {
			return SemanticDescriptionResult{}, jobs.ErrInvalid
		}
	} else {
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return SemanticDescriptionResult{}, jobs.ErrInvalid
			}
			result.RecommendedUsage = append(result.RecommendedUsage, text)
		}
	}
	if err := validateSemanticResult(result); err != nil {
		return SemanticDescriptionResult{}, err
	}
	return result, nil
}
