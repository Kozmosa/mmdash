package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/project"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
var uuidPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

// Access is implemented by Project without exposing membership persistence.
type Access interface {
	Authenticate(context.Context, string) (auth.Identity, error)
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

// AuditRecorder accepts secret-free Artifact audit records.
type AuditRecorder interface {
	Record(context.Context, audit.Event) error
}

// MetricRecorder accepts only bounded Artifact labels.
type MetricRecorder interface {
	ObserveArtifactOperation(string, string, string, time.Duration)
}

// IDGenerator creates opaque application identifiers.
type IDGenerator interface {
	New() (string, error)
}

// Service owns Artifact authorization, state transitions, and verification.
type Service struct {
	Access    Access
	AgentRuns interface {
		ValidateProvenance(context.Context, string, string, string, string) error
	}
	Audit                 AuditRecorder
	Clock                 interface{ Now() time.Time }
	Generator             IDGenerator
	Git                   GitContentReader
	Jobs                  PreviewJobAccess
	SemanticJobs          SemanticJobAccess
	SemanticModel         SemanticDescriptionModel
	SemanticStore         SemanticDescriptionStore
	MaxPreviewOutputBytes int64
	MaxUploadBytes        int64
	Metrics               MetricRecorder
	MultipartPartBytes    int64
	Signer                *TransferSigner
	Storage               BlobStore
	TransferTTL           time.Duration
	UploadSessionTTL      time.Duration
	ConfirmRecoveryLease  time.Duration
	Store                 Store
	WorkerSigner          *TransferSigner
	// SystemProjectID owns mmdash-maintained artifacts such as Box installers.
	// It is intentionally not exposed through ordinary Project artifact routes.
	SystemProjectID string
}

// RegisterGit registers one small immutable Repo result without copying it to
// object storage. This Core-internal method is deliberately not exposed by the
// Stage 2 public HTTP API.
func (service Service) RegisterGit(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	input RegisterGitInput,
) (Detail, error) {
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactUpload,
	); err != nil {
		return Detail{}, err
	}
	if service.Git == nil {
		return Detail{}, ErrNotAvailable
	}
	if err := normalizeGitRegistration(&input); err != nil {
		return Detail{}, err
	}
	reader, sizeBytes, err := service.Git.Open(
		ctx, projectID, input.GitReference,
	)
	if err != nil {
		return Detail{}, err
	}
	digest := sha256.New()
	copied, copyErr := io.Copy(digest, reader)
	closeErr := reader.Close()
	if copyErr != nil {
		return Detail{}, copyErr
	}
	if closeErr != nil {
		return Detail{}, closeErr
	}
	if copied != sizeBytes {
		return Detail{}, ErrSizeMismatch
	}
	artifactID, err := service.Generator.New()
	if err != nil {
		return Detail{}, err
	}
	versionID, err := service.Generator.New()
	if err != nil {
		return Detail{}, err
	}
	relationID, err := service.Generator.New()
	if err != nil {
		return Detail{}, err
	}
	now := service.now()
	sourceObjectID := input.SourceObjectID
	artifact := Artifact{
		ID: artifactID, ProjectID: projectID, Kind: input.Kind,
		Source: input.Source, SourceObjectID: &sourceObjectID,
		Tags: input.Tags, Name: input.Name, Description: input.Description,
		RecommendedUsage: []string{}, CurrentVersionID: &versionID,
		Status: StatusAvailable, CreatedBy: identity.User.ID,
		CreatedAt: now, UpdatedAt: now,
	}
	version := Version{
		ID: versionID, ArtifactID: artifactID, ProjectID: projectID,
		VersionNo: 1, StorageClass: "git", Filename: input.Filename,
		SHA256: hex.EncodeToString(digest.Sum(nil)), MIMEType: input.MIMEType,
		SizeBytes: sizeBytes, Status: StatusAvailable, AvailableAt: &now,
		GitReference: &input.GitReference, CreatedBy: identity.User.ID,
		CreatedAt: now,
	}
	if err := service.Store.CreateGit(
		ctx, artifact, version, relationID,
	); err != nil {
		return Detail{}, err
	}
	return Detail{Artifact: artifact, CurrentVersion: &version}, nil
}

func (service Service) Authenticate(
	ctx context.Context,
	authorization string,
) (auth.Identity, error) {
	return service.Access.Authenticate(ctx, authorization)
}

func (service Service) Initialize(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	input InitializeUploadInput,
) (PublicUploadSession, error) {
	started := time.Now()
	outcome := "error"
	defer func() {
		service.observe("initialize", outcome, started)
	}()
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactUpload,
	); err != nil {
		return PublicUploadSession{}, err
	}
	createdBy := identity.User.ID
	source := SourceUserUpload
	if identity.Kind == "agent" {
		if identity.AgentInstanceID == "" || createdBy == "" || input.Kind != KindAgent {
			return PublicUploadSession{}, safe(
				"ARTIFACT_KIND_INVALID",
				"Agent uploads must create an Agent Artifact",
				ErrKindInvalid,
			)
		}
		if (input.AgentSessionID == "") != (input.AgentRunID == "") {
			return PublicUploadSession{}, ErrInvalid
		}
		if input.AgentRunID != "" {
			if service.validateAgentRunProvenance(
				ctx, identity, projectID, input.AgentSessionID, input.AgentRunID,
			) != nil {
				return PublicUploadSession{}, ErrForbidden
			}
		}
		source = SourceAgent
	} else if input.Kind == KindAgent {
		return PublicUploadSession{}, safe(
			"ARTIFACT_KIND_INVALID",
			"Agent Artifact kind is reserved for Agent uploads",
			ErrKindInvalid,
		)
	}
	plan, err := service.normalizeInitialize(&input)
	if err != nil {
		return PublicUploadSession{}, err
	}
	if existing, err := service.Store.GetUploadByIdempotency(
		ctx, projectID, input.IdempotencyKey,
	); err == nil {
		if !service.uploadOwnedBy(identity, existing) ||
			!matchesInitial(existing, createdBy, input) {
			return PublicUploadSession{}, safe(
				"ARTIFACT_UPLOAD_CONFLICT",
				"Idempotency key belongs to another upload",
				ErrUploadConflict,
			)
		}
		outcome = "success"
		return service.publicUpload(existing), nil
	} else if !errors.Is(err, ErrNotFound) {
		return PublicUploadSession{}, err
	}

	artifactID, versionID, uploadID, err := service.newUploadIDs()
	if err != nil {
		return PublicUploadSession{}, err
	}
	now := service.now()
	artifact := Artifact{
		ID: artifactID, ProjectID: projectID, Kind: input.Kind,
		Source: source, Tags: input.Tags, Name: input.Name,
		Description: input.Description, RecommendedUsage: []string{},
		CurrentVersionID: &versionID, Status: StatusPendingUpload,
		CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now,
	}
	version := Version{
		ID: versionID, ArtifactID: artifactID, ProjectID: projectID,
		VersionNo: 1, StorageClass: "object", Filename: input.Filename,
		SHA256: input.SHA256, MIMEType: input.MIMEType,
		SizeBytes: input.SizeBytes, Status: StatusPendingUpload,
		CreatedBy: createdBy, CreatedAt: now,
	}
	upload, providerUpload, err := service.prepareUpload(
		ctx, projectID, artifactID, versionID, uploadID, createdBy,
		input.Filename, input.MIMEType, input.SHA256, input.SizeBytes,
		input.IdempotencyKey, plan,
	)
	if err != nil {
		return PublicUploadSession{}, err
	}
	upload.AgentInstanceID = identity.AgentInstanceID
	upload.AgentSessionID = input.AgentSessionID
	upload.AgentRunID = input.AgentRunID
	if upload.Status == UploadCompleted {
		artifact.Status = StatusAvailable
		version.Status = StatusAvailable
		version.BlobID = strings.TrimPrefix(
			upload.ProviderUploadID, "deduplicated:",
		)
		version.AvailableAt = upload.CompletedAt
	}
	if err := service.Store.CreateFirst(ctx, artifact, version, upload); err != nil {
		service.abortPrepared(ctx, providerUpload)
		if errors.Is(err, ErrUploadConflict) {
			existing, findErr := service.Store.GetUploadByIdempotency(
				ctx, projectID, input.IdempotencyKey,
			)
			if findErr == nil && service.uploadOwnedBy(identity, existing) &&
				matchesInitial(existing, createdBy, input) {
				outcome = "success"
				return service.publicUpload(existing), nil
			}
		}
		return PublicUploadSession{}, err
	}
	outcome = "success"
	return service.publicUpload(upload), nil
}

func (service Service) validateAgentRunProvenance(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	sessionID string,
	runID string,
) error {
	if service.AgentRuns == nil {
		return ErrForbidden
	}
	return service.AgentRuns.ValidateProvenance(
		ctx, identity.AgentInstanceID, projectID, sessionID, runID,
	)
}

// AttachAgentRunInputs links available project Artifacts to one already
// reserved Agent Run. Artifact remains the authoritative file owner.
func (service Service) AttachAgentRunInputs(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	runID string,
	artifactIDs []string,
) ([]ChatAttachment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionArtifactRead); err != nil {
		return nil, err
	}
	if runID == "" || len(artifactIDs) > 10 {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	items := make([]ChatAttachment, 0, len(artifactIDs))
	for _, artifactID := range artifactIDs {
		artifactID = strings.TrimSpace(artifactID)
		if artifactID == "" || seen[artifactID] {
			return nil, ErrInvalid
		}
		seen[artifactID] = true
		item, err := service.Store.AttachToAgentRun(
			ctx, projectID, artifactID, runID, identity.User.ID, service.now(),
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (service Service) ListAgentRunAttachments(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	runIDs []string,
) ([]ChatAttachment, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionArtifactRead); err != nil {
		return nil, err
	}
	if len(runIDs) > 500 {
		return nil, ErrInvalid
	}
	return service.Store.ListAgentRunAttachments(ctx, projectID, runIDs)
}

func (service Service) InitializeVersion(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	artifactID string,
	input InitializeVersionInput,
) (PublicUploadSession, error) {
	started := time.Now()
	outcome := "error"
	defer func() {
		service.observe("initialize_version", outcome, started)
	}()
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactUpload,
	); err != nil {
		return PublicUploadSession{}, err
	}
	if identity.Kind == "agent" {
		return PublicUploadSession{}, ErrForbidden
	}
	detail, err := service.Store.GetDetail(ctx, projectID, artifactID, false)
	if err != nil {
		return PublicUploadSession{}, err
	}
	if isBuiltInArticleTemplate(detail.Artifact) {
		return PublicUploadSession{}, ErrForbidden
	}
	plan, err := service.normalizeVersionInitialize(&input)
	if err != nil {
		return PublicUploadSession{}, err
	}
	if existing, err := service.Store.GetUploadByIdempotency(
		ctx, projectID, input.IdempotencyKey,
	); err == nil {
		if !matchesVersion(existing, identity.User.ID, artifactID, input) {
			return PublicUploadSession{}, safe(
				"ARTIFACT_UPLOAD_CONFLICT",
				"Idempotency key belongs to another upload",
				ErrUploadConflict,
			)
		}
		outcome = "success"
		return service.publicUpload(existing), nil
	} else if !errors.Is(err, ErrNotFound) {
		return PublicUploadSession{}, err
	}

	versionID, err := service.Generator.New()
	if err != nil {
		return PublicUploadSession{}, err
	}
	uploadID, err := service.Generator.New()
	if err != nil {
		return PublicUploadSession{}, err
	}
	now := service.now()
	version := Version{
		ID: versionID, ArtifactID: artifactID, ProjectID: projectID,
		StorageClass: "object", Filename: input.Filename,
		SHA256: input.SHA256, MIMEType: input.MIMEType,
		SizeBytes: input.SizeBytes, Status: StatusPendingUpload,
		CreatedBy: identity.User.ID, CreatedAt: now,
	}
	upload, providerUpload, err := service.prepareUpload(
		ctx, projectID, artifactID, versionID, uploadID, identity.User.ID,
		input.Filename, input.MIMEType, input.SHA256, input.SizeBytes,
		input.IdempotencyKey, plan,
	)
	if err != nil {
		return PublicUploadSession{}, err
	}
	if upload.Status == UploadCompleted {
		version.Status = StatusAvailable
		version.BlobID = strings.TrimPrefix(
			upload.ProviderUploadID, "deduplicated:",
		)
		version.AvailableAt = upload.CompletedAt
	}
	created, err := service.Store.CreateVersion(
		ctx, projectID, artifactID, version, upload,
	)
	if err != nil {
		service.abortPrepared(ctx, providerUpload)
		if errors.Is(err, ErrUploadConflict) {
			existing, findErr := service.Store.GetUploadByIdempotency(
				ctx, projectID, input.IdempotencyKey,
			)
			if findErr == nil &&
				matchesVersion(existing, identity.User.ID, artifactID, input) {
				outcome = "success"
				return service.publicUpload(existing), nil
			}
		}
		return PublicUploadSession{}, err
	}
	outcome = "success"
	return service.publicUpload(created), nil
}

func (service Service) GetUpload(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	uploadID string,
) (PublicUploadSession, error) {
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactUpload,
	); err != nil {
		return PublicUploadSession{}, err
	}
	upload, err := service.Store.GetUpload(ctx, projectID, uploadID)
	if err != nil {
		return PublicUploadSession{}, err
	}
	if !service.uploadOwnedBy(identity, upload) {
		return PublicUploadSession{}, ErrForbidden
	}
	if service.uploadExpired(upload) {
		if err := service.expire(ctx, upload); err != nil {
			return PublicUploadSession{}, err
		}
		upload, err = service.Store.GetUpload(ctx, projectID, uploadID)
		return service.publicUpload(upload), err
	}
	if isActiveUpload(upload.Status) && !isSyntheticUpload(upload) {
		parts, listErr := service.Storage.ListParts(ctx, providerHandle(upload))
		if listErr != nil {
			if (upload.Status != UploadCompleting &&
				upload.Status != UploadVerifying) ||
				!errors.Is(listErr, ErrUploadNotFound) {
				return PublicUploadSession{}, service.storageError(listErr)
			}
			if _, statErr := service.Storage.Stat(
				ctx, upload.StagingKey,
			); statErr != nil {
				return PublicUploadSession{}, service.storageError(listErr)
			}
		} else {
			upload.Parts = completedToUploadParts(parts, service.now())
			if err := service.Store.UpsertParts(
				ctx, upload.ID, upload.Parts,
			); err != nil {
				return PublicUploadSession{}, err
			}
		}
	}
	return service.publicUpload(upload), nil
}

func (service Service) SignParts(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	uploadID string,
	partNumbers []int,
) (PartGrantList, error) {
	started := time.Now()
	outcome := "error"
	defer func() {
		service.observe("sign_parts", outcome, started)
	}()
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactUpload,
	); err != nil {
		return PartGrantList{}, err
	}
	upload, err := service.Store.GetUpload(ctx, projectID, uploadID)
	if err != nil {
		return PartGrantList{}, err
	}
	if !service.uploadOwnedBy(identity, upload) {
		return PartGrantList{}, ErrForbidden
	}
	if err := service.requireActiveUpload(ctx, upload); err != nil {
		return PartGrantList{}, err
	}
	if isSyntheticUpload(upload) {
		return PartGrantList{}, safe(
			"ARTIFACT_UPLOAD_CONFLICT",
			"Deduplicated upload has no multipart parts",
			ErrUploadConflict,
		)
	}
	if len(partNumbers) == 0 || len(partNumbers) > 100 {
		return PartGrantList{}, safe(
			"ARTIFACT_PART_INVALID", "Part batch is invalid", ErrPartInvalid,
		)
	}
	seen := map[int]bool{}
	plan := MultipartPlan{
		PartBytes: upload.PartSizeBytes,
		PartCount: upload.PartCount,
		SizeBytes: upload.ExpectedSize,
	}
	items := make([]PartGrant, 0, len(partNumbers))
	for _, partNumber := range partNumbers {
		if seen[partNumber] {
			return PartGrantList{}, safe(
				"ARTIFACT_PART_INVALID", "Part batch contains duplicates",
				ErrPartInvalid,
			)
		}
		seen[partNumber] = true
		sizeBytes, err := plan.PartSize(partNumber)
		if err != nil {
			return PartGrantList{}, safe(
				"ARTIFACT_PART_INVALID", "Part number is invalid", ErrPartInvalid,
			)
		}
		grant, err := service.signPart(ctx, upload, partNumber, sizeBytes)
		if err != nil {
			return PartGrantList{}, err
		}
		items = append(items, PartGrant{
			PartNumber: partNumber, SizeBytes: sizeBytes, Transfer: grant,
		})
	}
	if err := service.Store.MarkUploading(ctx, upload.ID, service.now()); err != nil {
		return PartGrantList{}, err
	}
	service.record(
		ctx, "artifact.upload.parts.signed", "success", projectID,
		upload.ArtifactID, map[string]interface{}{"part_count": len(items)},
	)
	outcome = "success"
	return PartGrantList{Items: items}, nil
}

func (service Service) PutSignedPart(
	ctx context.Context,
	token string,
	body io.Reader,
	contentLength int64,
) (CompletedPart, error) {
	claims, err := service.Signer.Verify(token, service.now())
	if err != nil {
		return CompletedPart{}, err
	}
	if claims.Kind != transferUploadPart ||
		(contentLength >= 0 && contentLength != claims.SizeBytes) {
		return CompletedPart{}, safe(
			"ARTIFACT_PART_INVALID", "Signed part does not match the request",
			ErrPartInvalid,
		)
	}
	upload, err := service.Store.GetUpload(ctx, claims.ProjectID, claims.UploadID)
	if err != nil {
		return CompletedPart{}, err
	}
	if service.Storage.Backend() != "local" ||
		upload.ID != claims.UploadID ||
		upload.ProjectID != claims.ProjectID {
		return CompletedPart{}, ErrNotFound
	}
	if err := service.requireActiveUpload(ctx, upload); err != nil {
		return CompletedPart{}, err
	}
	plan := MultipartPlan{
		PartBytes: upload.PartSizeBytes,
		PartCount: upload.PartCount,
		SizeBytes: upload.ExpectedSize,
	}
	expectedSize, err := plan.PartSize(claims.PartNumber)
	if err != nil || expectedSize != claims.SizeBytes {
		return CompletedPart{}, safe(
			"ARTIFACT_PART_INVALID", "Signed part is invalid", ErrPartInvalid,
		)
	}
	part, err := service.Storage.PutPart(
		ctx, providerHandle(upload), claims.PartNumber, body, expectedSize,
	)
	if err != nil {
		return CompletedPart{}, service.storageError(err)
	}
	now := service.now()
	if err := service.Store.UpsertParts(ctx, upload.ID, []UploadPart{{
		PartNumber: part.PartNumber, SizeBytes: part.SizeBytes,
		ETag: normalizeETag(part.ETag), CompletedAt: now,
	}}); err != nil {
		return CompletedPart{}, err
	}
	if err := service.Store.MarkUploading(ctx, upload.ID, now); err != nil {
		return CompletedPart{}, err
	}
	return part, nil
}

func (service Service) Confirm(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	uploadID string,
	submitted []ConfirmPart,
) (Detail, bool, error) {
	started := time.Now()
	outcome := "error"
	defer func() {
		service.observe("confirm", outcome, started)
	}()
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactUpload,
	); err != nil {
		return Detail{}, false, err
	}
	upload, err := service.Store.GetUpload(ctx, projectID, uploadID)
	if err != nil {
		return Detail{}, false, err
	}
	if !service.uploadOwnedBy(identity, upload) {
		return Detail{}, false, ErrForbidden
	}
	if upload.Status == UploadCompleted {
		detail, err := service.Store.GetDetail(
			ctx, projectID, upload.ArtifactID, false,
		)
		if err == nil {
			outcome = "success"
		}
		return detail, false, err
	}
	if err := service.requireConfirmableUpload(ctx, upload); err != nil {
		return Detail{}, false, err
	}
	if isSyntheticUpload(upload) {
		return Detail{}, false, safe(
			"ARTIFACT_UPLOAD_CONFLICT",
			"Deduplicated upload is already complete",
			ErrUploadConflict,
		)
	}
	if err := validateSubmittedParts(submitted, upload.PartCount); err != nil {
		return Detail{}, false, err
	}
	priorStatus := upload.Status
	now := service.now()
	acquired, err := service.Store.BeginConfirm(
		ctx, upload.ID, now, now.Add(-service.confirmLease()),
	)
	if err != nil {
		return Detail{}, false, err
	}
	if !acquired {
		current, getErr := service.Store.GetUpload(ctx, projectID, uploadID)
		if getErr != nil {
			return Detail{}, false, getErr
		}
		if current.Status == UploadCompleted {
			detail, detailErr := service.Store.GetDetail(
				ctx, projectID, current.ArtifactID, false,
			)
			if detailErr == nil {
				outcome = "success"
			}
			return detail, false, detailErr
		}
		return Detail{}, false, safe(
			"ARTIFACT_UPLOAD_CONFLICT",
			"Upload confirmation is already in progress",
			ErrUploadConflict,
		)
	}
	providerParts, completedAlready, err := service.confirmProviderParts(
		ctx, upload, priorStatus,
	)
	if err != nil {
		return Detail{}, false, err
	}
	if !completedAlready {
		if err := compareSubmittedParts(upload, submitted, providerParts); err != nil {
			_ = service.Store.SetUploadStatus(
				ctx, upload.ID, UploadUploading, "", service.now(),
			)
			return Detail{}, false, err
		}
		now := service.now()
		if err := service.Store.UpsertParts(
			ctx, upload.ID, completedToUploadParts(providerParts, now),
		); err != nil {
			return Detail{}, false, err
		}
		if _, err := service.Storage.CompleteMultipart(
			ctx, providerHandle(upload), providerParts,
		); err != nil {
			return Detail{}, false, service.storageError(err)
		}
	}
	if err := service.Store.SetUploadStatus(
		ctx, upload.ID, UploadVerifying, "", service.now(),
	); err != nil {
		return Detail{}, false, err
	}
	contentKey := ContentObjectKey(upload.ProjectID, upload.ExpectedSHA256)
	verificationKey := upload.StagingKey
	alreadyPromoted := false
	if completedAlready {
		if _, statErr := service.Storage.Stat(
			ctx, upload.StagingKey,
		); statErr != nil {
			verificationKey = contentKey
			alreadyPromoted = true
		}
	}
	if err := service.verifyObject(
		ctx, verificationKey, upload.ExpectedSize, upload.ExpectedSHA256,
	); err != nil {
		if errors.Is(err, ErrSizeMismatch) || errors.Is(err, ErrHashMismatch) {
			service.failVerification(ctx, upload, err)
		}
		return Detail{}, false, err
	}
	if !alreadyPromoted {
		err = service.promoteVerified(ctx, upload, contentKey)
	}
	if err != nil {
		if errors.Is(err, ErrSizeMismatch) || errors.Is(err, ErrHashMismatch) {
			service.failVerification(ctx, upload, err)
		}
		return Detail{}, false, err
	}
	blobID, err := service.Generator.New()
	if err != nil {
		return Detail{}, false, err
	}
	detail, err := service.Store.FinalizeUpload(ctx, upload, Blob{
		ID: blobID, ProjectID: upload.ProjectID,
		SHA256: upload.ExpectedSHA256, SizeBytes: upload.ExpectedSize,
		Backend: service.Storage.Backend(), ObjectKey: contentKey,
	}, service.now())
	if err != nil {
		return Detail{}, false, err
	}
	outcome = "success"
	return detail, true, nil
}

func (service Service) Abort(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	uploadID string,
) error {
	started := time.Now()
	outcome := "error"
	defer func() {
		service.observe("abort", outcome, started)
	}()
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactUpload,
	); err != nil {
		return err
	}
	upload, err := service.Store.GetUpload(ctx, projectID, uploadID)
	if err != nil {
		return err
	}
	if !service.uploadOwnedBy(identity, upload) {
		return ErrForbidden
	}
	switch upload.Status {
	case UploadCompleted:
		return safe(
			"ARTIFACT_UPLOAD_CONFLICT",
			"Completed upload cannot be aborted",
			ErrUploadConflict,
		)
	case UploadAborted, UploadExpired:
		if err := service.abortProvider(ctx, upload); err != nil {
			return err
		}
		outcome = "success"
		return nil
	case UploadFailed:
		outcome = "success"
		return nil
	case UploadCompleting, UploadVerifying:
		return safe(
			"ARTIFACT_UPLOAD_CONFLICT",
			"Upload confirmation is already in progress",
			ErrUploadConflict,
		)
	}
	now := service.now()
	if err := service.Store.SetUploadStatus(
		ctx, upload.ID, UploadAborted, "ARTIFACT_UPLOAD_ABORTED", now,
	); err != nil {
		return err
	}
	upload.Status = UploadAborted
	if err := service.abortProvider(ctx, upload); err != nil {
		return err
	}
	outcome = "success"
	return nil
}

func (service Service) List(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	filter ListFilter,
) (Page, error) {
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactRead,
	); err != nil {
		return Page{}, err
	}
	if !validListFilter(filter) {
		return Page{}, ErrInvalid
	}
	return service.Store.List(ctx, projectID, filter)
}

func validFolderUUID(value string) bool {
	return uuidPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}

func validFolderName(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 255 && !strings.ContainsAny(value, "\r\n")
}

func (service Service) ListFolders(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (FolderTree, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionArtifactRead); err != nil {
		return FolderTree{}, err
	}
	if !validFolderUUID(projectID) {
		return FolderTree{}, ErrInvalid
	}
	return service.Store.GetFolderTree(ctx, projectID)
}

func (service Service) CreateFolder(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	input CreateFolderInput,
) (Folder, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionArtifactUpload); err != nil {
		return Folder{}, err
	}
	input.Name = strings.TrimSpace(input.Name)
	if !validFolderUUID(projectID) || !validFolderName(input.Name) ||
		(input.ParentFolderID != nil && !validFolderUUID(*input.ParentFolderID)) {
		return Folder{}, ErrInvalid
	}
	id, err := service.Generator.New()
	if err != nil {
		return Folder{}, err
	}
	folder := Folder{
		ID: id, ProjectID: projectID, ParentFolderID: input.ParentFolderID,
		Name: input.Name, Children: []Folder{},
	}
	return service.Store.CreateFolder(ctx, folder)
}

func (service Service) RenameFolder(
	ctx context.Context,
	identity auth.Identity,
	projectID, folderID, name string,
) (Folder, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionArtifactUpload); err != nil {
		return Folder{}, err
	}
	name = strings.TrimSpace(name)
	if !validFolderUUID(projectID) || !validFolderUUID(folderID) || !validFolderName(name) {
		return Folder{}, ErrInvalid
	}
	return service.Store.RenameFolder(ctx, projectID, folderID, name, service.now())
}

func (service Service) MoveFolder(
	ctx context.Context,
	identity auth.Identity,
	projectID, folderID string,
	input MoveFolderInput,
) (Folder, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionArtifactUpload); err != nil {
		return Folder{}, err
	}
	if !validFolderUUID(projectID) || !validFolderUUID(folderID) ||
		(input.ParentFolderID != nil && !validFolderUUID(*input.ParentFolderID)) ||
		(input.Position != nil && *input.Position < 0) {
		return Folder{}, ErrInvalid
	}
	return service.Store.MoveFolder(ctx, projectID, folderID, input.ParentFolderID, input.Position, service.now())
}

func (service Service) DeleteFolder(
	ctx context.Context,
	identity auth.Identity,
	projectID, folderID string,
	recursive bool,
) error {
	if err := service.authorize(ctx, identity, projectID, project.PermissionArtifactDelete); err != nil {
		return err
	}
	if !validFolderUUID(projectID) || !validFolderUUID(folderID) {
		return ErrInvalid
	}
	return service.Store.DeleteFolder(ctx, projectID, folderID, recursive, service.now())
}

func (service Service) MoveArtifact(
	ctx context.Context,
	identity auth.Identity,
	projectID, artifactID string,
	folderID *string,
) (Detail, error) {
	if err := service.authorize(ctx, identity, projectID, project.PermissionArtifactUpload); err != nil {
		return Detail{}, err
	}
	if !validFolderUUID(projectID) || !validFolderUUID(artifactID) ||
		(folderID != nil && !validFolderUUID(*folderID)) {
		return Detail{}, ErrInvalid
	}
	return service.Store.MoveArtifact(ctx, projectID, artifactID, folderID, service.now())
}

func (service Service) Get(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	artifactID string,
	includeTrashed bool,
) (Detail, error) {
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactRead,
	); err != nil {
		return Detail{}, err
	}
	return service.Store.GetDetail(ctx, projectID, artifactID, includeTrashed)
}

func (service Service) Update(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	artifactID string,
	input UpdateInput,
) (Detail, error) {
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactUpload,
	); err != nil {
		return Detail{}, err
	}
	if err := normalizeUpdate(&input); err != nil {
		return Detail{}, err
	}
	existing, err := service.Store.GetDetail(ctx, projectID, artifactID, false)
	if err != nil {
		return Detail{}, err
	}
	if isBuiltInArticleTemplate(existing.Artifact) {
		return Detail{}, ErrForbidden
	}
	detail, err := service.Store.Update(
		ctx, projectID, artifactID, input, service.now(),
	)
	return detail, err
}

func (service Service) ListVersions(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	artifactID string,
) (VersionList, error) {
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactRead,
	); err != nil {
		return VersionList{}, err
	}
	return service.Store.ListVersions(ctx, projectID, artifactID)
}

func (service Service) RestoreVersion(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	artifactID string,
	versionID string,
	idempotencyKey string,
) (Detail, error) {
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactUpload,
	); err != nil {
		return Detail{}, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return Detail{}, ErrInvalid
	}
	existing, err := service.Store.GetDetail(ctx, projectID, artifactID, false)
	if err != nil {
		return Detail{}, err
	}
	if isBuiltInArticleTemplate(existing.Artifact) {
		return Detail{}, ErrForbidden
	}
	newVersionID, err := service.Generator.New()
	if err != nil {
		return Detail{}, err
	}
	detail, err := service.Store.RestoreVersion(
		ctx, projectID, artifactID, versionID, newVersionID,
		idempotencyKey, identity.User.ID, service.now(),
	)
	return detail, err
}

func (service Service) Download(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	artifactID string,
	versionID string,
) (DownloadGrant, error) {
	started := time.Now()
	outcome := "error"
	defer func() {
		service.observe("download_grant", outcome, started)
	}()
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactDownload,
	); err != nil {
		return DownloadGrant{}, err
	}
	detail, err := service.Store.GetDetail(ctx, projectID, artifactID, false)
	if err != nil {
		return DownloadGrant{}, err
	}
	if versionID == "" {
		if detail.CurrentVersion == nil {
			return DownloadGrant{}, ErrNotAvailable
		}
		versionID = detail.CurrentVersion.ID
	}
	version, err := service.Store.GetVersion(
		ctx, projectID, artifactID, versionID,
	)
	if err != nil {
		return DownloadGrant{}, err
	}
	if version.Status != StatusAvailable ||
		(version.StorageClass != "object" && version.StorageClass != "git") ||
		(version.StorageClass == "object" &&
			(version.ObjectKey == "" ||
				version.Backend != service.Storage.Backend())) ||
		(version.StorageClass == "git" &&
			(version.GitReference == nil || service.Git == nil)) {
		return DownloadGrant{}, safe(
			"ARTIFACT_NOT_AVAILABLE", "Artifact Version is not available",
			ErrNotAvailable,
		)
	}
	transfer, err := service.downloadTransfer(ctx, version)
	if err != nil {
		return DownloadGrant{}, err
	}
	service.record(
		ctx, "artifact.download.signed", "success", projectID, artifactID,
		map[string]interface{}{"version_id": version.ID},
	)
	outcome = "success"
	return DownloadGrant{
		ArtifactID: artifactID, VersionID: version.ID,
		Filename: version.Filename, MIMEType: version.MIMEType,
		SizeBytes: version.SizeBytes, Transfer: transfer,
	}, nil
}

// ListBoxReleases returns current, available Box installer artifacts from the
// mmdash system Project. The endpoint that calls this method authenticates a
// human session, while this method deliberately does not use ordinary Project
// membership because the system Project is hidden from user project lists.
func (service Service) ListBoxReleases(ctx context.Context) (BoxReleaseList, error) {
	projectID := strings.TrimSpace(service.SystemProjectID)
	if projectID == "" {
		projectID = DefaultBoxReleaseProjectID
	}
	if service.Store == nil || service.Signer == nil || service.Storage == nil {
		return BoxReleaseList{}, ErrNotAvailable
	}
	page, err := service.Store.List(ctx, projectID, ListFilter{
		Kind: KindOther, Limit: 200, Source: SourceSystem, Status: StatusAvailable,
	})
	if err != nil {
		return BoxReleaseList{}, err
	}
	result := BoxReleaseList{Items: make([]BoxRelease, 0, len(page.Items))}
	for _, detail := range page.Items {
		platform, version, ok := boxReleaseMetadata(detail)
		if !ok || detail.CurrentVersion == nil {
			continue
		}
		current := detail.CurrentVersion
		grant, err := service.downloadAvailableVersion(ctx, projectID, detail.Artifact.ID, current.ID)
		if err != nil {
			if errors.Is(err, ErrNotAvailable) {
				continue
			}
			return BoxReleaseList{}, err
		}
		result.Items = append(result.Items, BoxRelease{
			Platform: platform, Version: version,
			ArtifactID: detail.Artifact.ID, VersionID: current.ID,
			Filename: current.Filename, SHA256: current.SHA256,
			SizeBytes: current.SizeBytes, Download: grant.Transfer,
			InstallCommand: boxReleaseInstallCommand(platform, current.Filename),
			Instructions:   boxReleaseInstructions(platform),
		})
	}
	return result, nil
}

func (service Service) downloadAvailableVersion(
	ctx context.Context, projectID, artifactID, versionID string,
) (DownloadGrant, error) {
	detail, err := service.Store.GetDetail(ctx, projectID, artifactID, false)
	if err != nil {
		return DownloadGrant{}, err
	}
	version, err := service.Store.GetVersion(ctx, projectID, artifactID, versionID)
	if err != nil {
		return DownloadGrant{}, err
	}
	if detail.Artifact.Status != StatusAvailable || version.Status != StatusAvailable ||
		(version.StorageClass != "object" && version.StorageClass != "git") ||
		(version.StorageClass == "object" && (version.ObjectKey == "" || version.Backend != service.Storage.Backend())) ||
		(version.StorageClass == "git" && (version.GitReference == nil || service.Git == nil)) {
		return DownloadGrant{}, ErrNotAvailable
	}
	transfer, err := service.downloadTransfer(ctx, version)
	if err != nil {
		return DownloadGrant{}, err
	}
	return DownloadGrant{
		ArtifactID: artifactID, VersionID: version.ID,
		Filename: version.Filename, MIMEType: version.MIMEType,
		SizeBytes: version.SizeBytes, Transfer: transfer,
	}, nil
}

func boxReleaseMetadata(detail Detail) (string, string, bool) {
	if detail.Artifact.Source != SourceSystem || detail.Artifact.Kind != KindOther || detail.Artifact.Status != StatusAvailable {
		return "", "", false
	}
	platform, version, tagged := "", "", false
	for _, tag := range detail.Artifact.Tags {
		switch {
		case strings.HasPrefix(tag, "platform:"):
			platform = strings.TrimPrefix(tag, "platform:")
		case strings.HasPrefix(tag, "version:"):
			version = strings.TrimPrefix(tag, "version:")
		case tag == "box-release":
			tagged = true
		}
	}
	filename := strings.ToLower(detail.Artifact.Name)
	if platform == "" {
		switch {
		case strings.Contains(filename, "windows") || strings.Contains(filename, "win"):
			platform = "windows"
		case strings.Contains(filename, "linux"):
			platform = "linux"
		}
	}
	if !tagged {
		return "", "", false
	}
	if (platform != "windows" && platform != "linux") || version == "" {
		return "", "", false
	}
	return platform, version, true
}

func boxReleaseInstallCommand(platform, filename string) string {
	filename = path.Base(strings.TrimSpace(filename))
	if platform == "windows" {
		return ".\\" + filename
	}
	return "chmod +x ./" + filename + " && ./" + filename
}

func boxReleaseInstructions(platform string) string {
	if platform == "windows" {
		return "下载后在 PowerShell 中运行该文件。首次启动会输出一次性设备码；打开提示的浏览器地址登录 mmdash 并确认授权，然后按需设置 MMDASH_CORE_URL、MMDASH_BOX_NAME 和 Runtime 依赖。"
	}
	return "下载后执行安装命令。首次启动会输出一次性设备码；打开提示的浏览器地址登录 mmdash 并确认授权，然后按需设置 MMDASH_CORE_URL、MMDASH_BOX_NAME 和 Runtime 依赖。"
}

func (service Service) OpenSignedDownload(
	ctx context.Context,
	token string,
) (io.ReadCloser, Version, error) {
	claims, err := service.Signer.Verify(token, service.now())
	if err != nil {
		if errors.Is(err, ErrTransferExpired) {
			return nil, Version{}, err
		}
		return nil, Version{}, ErrNotFound
	}
	if claims.Kind != transferDownload {
		return nil, Version{}, ErrInvalid
	}
	detail, err := service.Store.GetDetail(
		ctx, claims.ProjectID, claims.ArtifactID, false,
	)
	if err != nil || detail.Artifact.Status != StatusAvailable {
		return nil, Version{}, ErrNotFound
	}
	version, err := service.Store.GetVersion(
		ctx, claims.ProjectID, claims.ArtifactID, claims.VersionID,
	)
	if err != nil ||
		version.Status != StatusAvailable ||
		version.SizeBytes != claims.SizeBytes {
		return nil, Version{}, ErrNotFound
	}
	if version.StorageClass == "git" {
		if version.GitReference == nil || service.Git == nil {
			return nil, Version{}, ErrNotFound
		}
		reader, sizeBytes, err := service.Git.Open(
			ctx, version.ProjectID, *version.GitReference,
		)
		if err != nil || sizeBytes != version.SizeBytes {
			if reader != nil {
				_ = reader.Close()
			}
			return nil, Version{}, ErrNotFound
		}
		return reader, version, nil
	}
	if version.StorageClass != "object" ||
		version.ObjectKey == "" ||
		version.Backend != service.Storage.Backend() {
		return nil, Version{}, ErrNotFound
	}
	reader, err := service.Storage.Open(ctx, version.ObjectKey)
	if err != nil {
		return nil, Version{}, service.storageError(err)
	}
	return reader, version, nil
}

func (service Service) Trash(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	artifactID string,
) error {
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactDelete,
	); err != nil {
		return err
	}
	detail, err := service.Store.GetDetail(ctx, projectID, artifactID, true)
	if err != nil {
		return err
	}
	if detail.Artifact.Status == StatusTrashed {
		return nil
	}
	if isBuiltInArticleTemplate(detail.Artifact) {
		return ErrForbidden
	}
	if err := service.Store.Trash(
		ctx, projectID, artifactID, identity.User.ID, service.now(),
	); err != nil {
		return safe(
			"ARTIFACT_UPLOAD_CONFLICT",
			"Artifact has an active upload or incompatible state",
			ErrUploadConflict,
		)
	}
	return nil
}

func (service Service) Restore(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	artifactID string,
) (Detail, error) {
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactDelete,
	); err != nil {
		return Detail{}, err
	}
	detail, err := service.Store.GetDetail(ctx, projectID, artifactID, true)
	if err != nil {
		return Detail{}, err
	}
	if detail.Artifact.Status == StatusAvailable {
		return detail, nil
	}
	detail, err = service.Store.Restore(
		ctx, projectID, artifactID, identity.User.ID, service.now(),
	)
	return detail, err
}

func (service Service) Purge(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	artifactID string,
) error {
	started := time.Now()
	outcome := "error"
	defer func() {
		service.observe("purge", outcome, started)
	}()
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactDelete,
	); err != nil {
		return err
	}
	detail, err := service.Store.GetDetail(ctx, projectID, artifactID, true)
	if err != nil {
		return err
	}
	if isBuiltInArticleTemplate(detail.Artifact) {
		return ErrForbidden
	}
	if err := service.Store.Purge(
		ctx, projectID, artifactID,
		func(ctx context.Context, objectKey string) error {
			if err := service.Storage.Delete(ctx, objectKey); err != nil {
				return service.storageError(err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	outcome = "success"
	return nil
}

func (service Service) ExpireUploads(
	ctx context.Context,
	limit int,
) (int, error) {
	uploads, err := service.Store.ExpireUploads(ctx, service.now(), limit)
	if err != nil {
		return 0, err
	}
	var firstErr error
	for _, upload := range uploads {
		if err := service.abortProvider(ctx, upload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return len(uploads), firstErr
}

func (service Service) prepareUpload(
	ctx context.Context,
	projectID string,
	artifactID string,
	versionID string,
	uploadID string,
	createdBy string,
	filename string,
	mimeType string,
	expectedSHA256 string,
	expectedSize int64,
	idempotencyKey string,
	plan MultipartPlan,
) (UploadSession, *MultipartUpload, error) {
	now := service.now()
	upload := UploadSession{
		ID: uploadID, ProjectID: projectID, ArtifactID: artifactID,
		VersionID: versionID, ExpectedSHA256: expectedSHA256,
		ExpectedSize: expectedSize, MIMEType: mimeType,
		PartSizeBytes: plan.PartBytes, PartCount: plan.PartCount,
		Status: UploadInitialized, IdempotencyKey: idempotencyKey,
		CreatedBy: createdBy, ExpiresAt: now.Add(service.sessionTTL()),
		CreatedAt: now, UpdatedAt: now, Filename: filename,
	}
	blob, err := service.Store.FindBlob(
		ctx, projectID, expectedSHA256, expectedSize,
	)
	if err == nil {
		if blob.Backend != service.Storage.Backend() {
			return UploadSession{}, nil, safe(
				"ARTIFACT_STORAGE_UNAVAILABLE",
				"Artifact blob uses another storage backend",
				ErrStorage,
			)
		}
		info, statErr := service.Storage.Stat(ctx, blob.ObjectKey)
		if statErr != nil || info.SizeBytes != expectedSize {
			return UploadSession{}, nil, safe(
				"ARTIFACT_STORAGE_UNAVAILABLE",
				"Deduplicated Artifact blob is unavailable",
				ErrStorage,
			)
		}
		upload.ProviderUploadID = "deduplicated:" + blob.ID
		upload.StagingKey = "deduplicated/" + uploadID
		upload.Status = UploadCompleted
		upload.CompletedAt = &now
		upload.ExpiresAt = now
		return upload, nil, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return UploadSession{}, nil, err
	}
	stagingKey := path.Join("projects", projectID, "staging", uploadID)
	providerUpload, err := service.Storage.CreateMultipart(
		ctx, stagingKey, mimeType,
	)
	if err != nil {
		return UploadSession{}, nil, service.storageError(err)
	}
	upload.ProviderUploadID = providerUpload.ProviderUploadID
	upload.StagingKey = stagingKey
	return upload, &providerUpload, nil
}

func (service Service) newUploadIDs() (string, string, string, error) {
	artifactID, err := service.Generator.New()
	if err != nil {
		return "", "", "", err
	}
	versionID, err := service.Generator.New()
	if err != nil {
		return "", "", "", err
	}
	uploadID, err := service.Generator.New()
	if err != nil {
		return "", "", "", err
	}
	return artifactID, versionID, uploadID, nil
}

func (service Service) normalizeInitialize(
	input *InitializeUploadInput,
) (MultipartPlan, error) {
	input.Filename = strings.TrimSpace(input.Filename)
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = strings.TrimSpace(input.Kind)
	input.SHA256 = strings.ToLower(strings.TrimSpace(input.SHA256))
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.Name == "" {
		input.Name = input.Filename
	}
	if !validFilename(input.Filename) ||
		input.Name == "" || len(input.Name) > 255 ||
		input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 ||
		!sha256Pattern.MatchString(input.SHA256) {
		return MultipartPlan{}, ErrInvalid
	}
	if !isInitialKind(input.Kind) {
		return MultipartPlan{}, safe(
			"ARTIFACT_KIND_INVALID", "Artifact kind is not available for upload",
			ErrKindInvalid,
		)
	}
	tags, err := normalizeTags(input.Tags)
	if err != nil {
		return MultipartPlan{}, err
	}
	input.Tags = tags
	if input.Description != nil {
		value := strings.TrimSpace(*input.Description)
		if len(value) > 4000 {
			return MultipartPlan{}, ErrInvalid
		}
		input.Description = &value
	}
	input.MIMEType, err = normalizeMIME(input.MIMEType)
	if err != nil {
		return MultipartPlan{}, err
	}
	plan, err := CalculateMultipartPlan(
		input.SizeBytes, service.MultipartPartBytes, service.MaxUploadBytes,
	)
	if err != nil {
		if input.SizeBytes > service.MaxUploadBytes {
			return MultipartPlan{}, safe(
				"ARTIFACT_TOO_LARGE", "Artifact exceeds the upload limit",
				ErrTooLarge,
			)
		}
		return MultipartPlan{}, ErrInvalid
	}
	return plan, nil
}

func (service Service) normalizeVersionInitialize(
	input *InitializeVersionInput,
) (MultipartPlan, error) {
	initial := InitializeUploadInput{
		Filename: input.Filename, Name: input.Filename,
		SizeBytes: input.SizeBytes, SHA256: input.SHA256,
		MIMEType: input.MIMEType, Kind: KindAttachment,
		IdempotencyKey: input.IdempotencyKey,
	}
	plan, err := service.normalizeInitialize(&initial)
	input.Filename = initial.Filename
	input.SHA256 = initial.SHA256
	input.MIMEType = initial.MIMEType
	input.IdempotencyKey = initial.IdempotencyKey
	return plan, err
}

func normalizeUpdate(input *UpdateInput) error {
	if input.Name == nil && input.Kind == nil &&
		input.Tags == nil && input.Description == nil {
		return ErrInvalid
	}
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if value == "" || len(value) > 255 {
			return ErrInvalid
		}
		input.Name = &value
	}
	if input.Kind != nil {
		value := strings.TrimSpace(*input.Kind)
		if !isPublicKind(value) {
			return safe(
				"ARTIFACT_KIND_INVALID",
				"Artifact kind is not editable to that value",
				ErrKindInvalid,
			)
		}
		input.Kind = &value
	}
	if input.Tags != nil {
		values, err := normalizeTags(*input.Tags)
		if err != nil {
			return err
		}
		input.Tags = &values
	}
	if input.Description != nil && *input.Description != nil {
		value := strings.TrimSpace(**input.Description)
		if len(value) > 4000 {
			return ErrInvalid
		}
		*input.Description = &value
	}
	return nil
}

func normalizeGitRegistration(input *RegisterGitInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.Filename = strings.TrimSpace(input.Filename)
	input.Kind = strings.TrimSpace(input.Kind)
	input.Source = strings.TrimSpace(input.Source)
	input.SourceObjectID = strings.ToLower(strings.TrimSpace(input.SourceObjectID))
	input.GitReference.RepositoryID = strings.ToLower(
		strings.TrimSpace(input.GitReference.RepositoryID),
	)
	input.GitReference.CommitSHA = strings.ToLower(
		strings.TrimSpace(input.GitReference.CommitSHA),
	)
	input.GitReference.Path = strings.TrimSpace(input.GitReference.Path)
	if input.Name == "" || len(input.Name) > 255 ||
		!validFilename(input.Filename) ||
		!uuidPattern.MatchString(input.GitReference.RepositoryID) ||
		(input.GitReference.Workspace != "result") ||
		!commitSHAPattern.MatchString(input.GitReference.CommitSHA) ||
		input.GitReference.Path == "" ||
		strings.Contains(input.GitReference.Path, `\`) ||
		strings.HasPrefix(input.GitReference.Path, "/") ||
		path.Clean(input.GitReference.Path) != input.GitReference.Path ||
		input.GitReference.Path == "." {
		return ErrInvalid
	}
	switch input.Source {
	case SourceExperiment:
		if input.Kind != KindExperimentResult ||
			!uuidPattern.MatchString(input.SourceObjectID) {
			return ErrSourceInvalid
		}
	case SourceModel:
		if input.Kind != KindModelFile ||
			!uuidPattern.MatchString(input.SourceObjectID) {
			return ErrSourceInvalid
		}
	case SourceArticle:
		if input.Kind != KindArticleBuild ||
			!uuidPattern.MatchString(input.SourceObjectID) {
			return ErrSourceInvalid
		}
	case SourceSystem:
		if input.SourceObjectID != input.GitReference.RepositoryID {
			return ErrSourceInvalid
		}
	default:
		return ErrSourceInvalid
	}
	tags, err := normalizeTags(input.Tags)
	if err != nil {
		return err
	}
	input.Tags = tags
	if input.Description != nil {
		value := strings.TrimSpace(*input.Description)
		if len(value) > 4000 {
			return ErrInvalid
		}
		input.Description = &value
	}
	input.MIMEType, err = normalizeMIME(input.MIMEType)
	return err
}

func normalizeTags(values []string) ([]string, error) {
	if len(values) > 32 {
		return nil, safe(
			"ARTIFACT_TAG_INVALID", "Artifact has too many tags", ErrTagInvalid,
		)
	}
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 64 ||
			strings.ContainsAny(value, "\x00\r\n") {
			return nil, safe(
				"ARTIFACT_TAG_INVALID", "Artifact tag is invalid", ErrTagInvalid,
			)
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result, nil
}

func normalizeMIME(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "application/octet-stream", nil
	}
	if len(value) > 255 || strings.ContainsAny(value, "\r\n\x00") {
		return "", safe(
			"ARTIFACT_MIME_NOT_ALLOWED", "Artifact MIME type is invalid",
			ErrInvalid,
		)
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || !strings.Contains(mediaType, "/") {
		return "", safe(
			"ARTIFACT_MIME_NOT_ALLOWED", "Artifact MIME type is invalid",
			ErrInvalid,
		)
	}
	return strings.ToLower(mediaType), nil
}

func (service Service) confirmProviderParts(
	ctx context.Context,
	upload UploadSession,
	priorStatus string,
) ([]CompletedPart, bool, error) {
	parts, err := service.Storage.ListParts(ctx, providerHandle(upload))
	if err == nil {
		return parts, false, nil
	}
	if !errors.Is(err, ErrUploadNotFound) ||
		(priorStatus != UploadCompleting && priorStatus != UploadVerifying) {
		return nil, false, service.storageError(err)
	}
	if _, statErr := service.Storage.Stat(ctx, upload.StagingKey); statErr != nil {
		contentKey := ContentObjectKey(
			upload.ProjectID, upload.ExpectedSHA256,
		)
		if _, contentErr := service.Storage.Stat(
			ctx, contentKey,
		); contentErr != nil {
			return nil, false, service.storageError(err)
		}
	}
	return nil, true, nil
}

func validateSubmittedParts(parts []ConfirmPart, expectedCount int) error {
	if len(parts) != expectedCount {
		return safe(
			"ARTIFACT_UPLOAD_INCOMPLETE",
			"Upload confirmation does not contain every part",
			ErrUploadIncomplete,
		)
	}
	for index, part := range parts {
		if part.PartNumber != index+1 ||
			normalizeETag(part.ETag) == "" ||
			len(part.ETag) > 1024 {
			return safe(
				"ARTIFACT_PART_INVALID",
				"Upload confirmation part order or ETag is invalid",
				ErrPartInvalid,
			)
		}
	}
	return nil
}

func compareSubmittedParts(
	upload UploadSession,
	submitted []ConfirmPart,
	actual []CompletedPart,
) error {
	if len(actual) != upload.PartCount {
		return safe(
			"ARTIFACT_PART_MISSING",
			"Object storage is missing one or more upload parts",
			ErrPartMissing,
		)
	}
	plan := MultipartPlan{
		PartBytes: upload.PartSizeBytes,
		PartCount: upload.PartCount,
		SizeBytes: upload.ExpectedSize,
	}
	for index := range actual {
		expectedSize, err := plan.PartSize(index + 1)
		if err != nil ||
			actual[index].PartNumber != index+1 ||
			actual[index].SizeBytes != expectedSize ||
			normalizeETag(actual[index].ETag) !=
				normalizeETag(submitted[index].ETag) {
			return safe(
				"ARTIFACT_PART_INVALID",
				"Uploaded part metadata does not match confirmation",
				ErrPartInvalid,
			)
		}
	}
	return nil
}

func (service Service) verifyObject(
	ctx context.Context,
	objectKey string,
	expectedSize int64,
	expectedSHA256 string,
) error {
	info, err := service.Storage.Stat(ctx, objectKey)
	if err != nil {
		return service.storageError(err)
	}
	if info.SizeBytes != expectedSize {
		return safe(
			"ARTIFACT_SIZE_MISMATCH",
			"Completed Artifact size does not match the declaration",
			ErrSizeMismatch,
		)
	}
	reader, err := service.Storage.Open(ctx, objectKey)
	if err != nil {
		return service.storageError(err)
	}
	digest := sha256.New()
	size, copyErr := io.Copy(digest, reader)
	closeErr := reader.Close()
	if copyErr != nil {
		return service.storageError(copyErr)
	}
	if closeErr != nil {
		return service.storageError(closeErr)
	}
	if size != expectedSize {
		return safe(
			"ARTIFACT_SIZE_MISMATCH",
			"Completed Artifact size changed during verification",
			ErrSizeMismatch,
		)
	}
	if hex.EncodeToString(digest.Sum(nil)) != expectedSHA256 {
		return safe(
			"ARTIFACT_HASH_MISMATCH",
			"Completed Artifact hash does not match the declaration",
			ErrHashMismatch,
		)
	}
	return nil
}

func (service Service) promoteVerified(
	ctx context.Context,
	upload UploadSession,
	contentKey string,
) error {
	err := service.Storage.Promote(ctx, upload.StagingKey, contentKey)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrObjectExists) {
		return service.storageError(err)
	}
	if err := service.verifyObject(
		ctx, contentKey, upload.ExpectedSize, upload.ExpectedSHA256,
	); err != nil {
		return err
	}
	if err := service.Storage.Delete(ctx, upload.StagingKey); err != nil {
		return service.storageError(err)
	}
	return nil
}

func (service Service) failVerification(
	ctx context.Context,
	upload UploadSession,
	cause error,
) {
	code := "ARTIFACT_STORAGE_UNAVAILABLE"
	var safeError *SafeError
	if errors.As(cause, &safeError) {
		code = safeError.Code
	}
	_ = service.Store.SetUploadStatus(
		ctx, upload.ID, UploadFailed, code, service.now(),
	)
	_ = service.Storage.Delete(ctx, upload.StagingKey)
}

func (service Service) signPart(
	ctx context.Context,
	upload UploadSession,
	partNumber int,
	sizeBytes int64,
) (TransferGrant, error) {
	if service.Storage.Backend() == "local" {
		return service.Signer.Sign(TransferClaims{
			Kind: transferUploadPart, ProjectID: upload.ProjectID,
			UploadID: upload.ID, PartNumber: partNumber,
			SizeBytes: sizeBytes,
		}, service.now(), service.transferTTL())
	}
	signed, err := service.Storage.PresignPart(
		ctx, providerHandle(upload), partNumber, service.transferTTL(),
	)
	if err != nil {
		return TransferGrant{}, service.storageError(err)
	}
	return TransferGrant{
		Method: signed.Method, URL: signed.URL, Headers: signed.Headers,
		ExpiresAt: signed.ExpiresAt,
	}, nil
}

func (service Service) downloadTransfer(
	ctx context.Context,
	version Version,
) (TransferGrant, error) {
	if version.StorageClass == "git" ||
		service.Storage.Backend() == "local" {
		return service.Signer.Sign(TransferClaims{
			Kind: transferDownload, ProjectID: version.ProjectID,
			ArtifactID: version.ArtifactID, VersionID: version.ID,
			SizeBytes: version.SizeBytes,
		}, service.now(), service.transferTTL())
	}
	signed, err := service.Storage.PresignGet(
		ctx,
		version.ObjectKey,
		service.transferTTL(),
		GetObjectOptions{
			ContentDisposition: contentDisposition(version.Filename),
			ContentType:        version.MIMEType,
		},
	)
	if err != nil {
		return TransferGrant{}, service.storageError(err)
	}
	return TransferGrant{
		Method: signed.Method, URL: signed.URL, Headers: signed.Headers,
		ExpiresAt: signed.ExpiresAt,
	}, nil
}

func (service Service) requireActiveUpload(
	ctx context.Context,
	upload UploadSession,
) error {
	if service.uploadExpired(upload) {
		if err := service.expire(ctx, upload); err != nil {
			return err
		}
		return safe(
			"ARTIFACT_UPLOAD_EXPIRED", "Artifact upload has expired",
			ErrUploadExpired,
		)
	}
	switch upload.Status {
	case UploadInitialized, UploadUploading:
		return nil
	case UploadAborted:
		return safe(
			"ARTIFACT_UPLOAD_ABORTED", "Artifact upload was aborted",
			ErrUploadAborted,
		)
	case UploadExpired:
		return safe(
			"ARTIFACT_UPLOAD_EXPIRED", "Artifact upload has expired",
			ErrUploadExpired,
		)
	default:
		return safe(
			"ARTIFACT_UPLOAD_CONFLICT",
			"Artifact upload is not accepting parts",
			ErrUploadConflict,
		)
	}
}

func (service Service) requireConfirmableUpload(
	ctx context.Context,
	upload UploadSession,
) error {
	if service.uploadExpired(upload) {
		if err := service.expire(ctx, upload); err != nil {
			return err
		}
		return safe(
			"ARTIFACT_UPLOAD_EXPIRED", "Artifact upload has expired",
			ErrUploadExpired,
		)
	}
	switch upload.Status {
	case UploadInitialized, UploadUploading, UploadCompleting, UploadVerifying:
		return nil
	case UploadAborted:
		return safe(
			"ARTIFACT_UPLOAD_ABORTED", "Artifact upload was aborted",
			ErrUploadAborted,
		)
	case UploadExpired:
		return safe(
			"ARTIFACT_UPLOAD_EXPIRED", "Artifact upload has expired",
			ErrUploadExpired,
		)
	default:
		return safe(
			"ARTIFACT_UPLOAD_CONFLICT",
			"Artifact upload cannot be confirmed",
			ErrUploadConflict,
		)
	}
}

func (service Service) expire(
	ctx context.Context,
	upload UploadSession,
) error {
	if err := service.Store.SetUploadStatus(
		ctx, upload.ID, UploadExpired, "ARTIFACT_UPLOAD_EXPIRED",
		service.now(),
	); err != nil {
		return err
	}
	upload.Status = UploadExpired
	return service.abortProvider(ctx, upload)
}

func (service Service) abortProvider(
	ctx context.Context,
	upload UploadSession,
) error {
	if isSyntheticUpload(upload) ||
		strings.HasPrefix(upload.ProviderUploadID, "aborted:") {
		return nil
	}
	if err := service.Storage.AbortMultipart(
		ctx, providerHandle(upload),
	); err != nil {
		return service.storageError(err)
	}
	if err := service.Storage.Delete(ctx, upload.StagingKey); err != nil {
		return service.storageError(err)
	}
	return service.Store.MarkProviderAborted(ctx, upload.ID, service.now())
}

func (service Service) abortPrepared(
	ctx context.Context,
	upload *MultipartUpload,
) {
	if upload != nil {
		_ = service.Storage.AbortMultipart(ctx, *upload)
	}
}

func (service Service) authorize(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	permission project.Permission,
) error {
	if err := service.Access.Authorize(
		ctx, identity, projectID, permission,
	); err != nil {
		service.record(
			ctx, "artifact.authorization.denied", "denied", projectID, "",
			map[string]interface{}{"permission": string(permission)},
		)
		return ErrForbidden
	}
	return nil
}

func (service Service) record(
	ctx context.Context,
	action string,
	outcome string,
	projectID string,
	resourceID string,
	metadata map[string]interface{},
) {
	if service.Audit == nil {
		return
	}
	_ = service.Audit.Record(ctx, audit.Event{
		Action: action, Category: "artifact", Outcome: outcome,
		ProjectID: projectID, ResourceID: resourceID,
		ResourceType: "artifact", Source: "core", Metadata: metadata,
	})
}

func (service Service) observe(
	operation string,
	outcome string,
	started time.Time,
) {
	if service.Metrics != nil {
		service.Metrics.ObserveArtifactOperation(
			operation, outcome, service.Storage.Backend(), time.Since(started),
		)
	}
}

func (service Service) storageError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidPart):
		return safe(
			"ARTIFACT_PART_INVALID", "Artifact part is invalid", ErrPartInvalid,
		)
	case errors.Is(err, ErrUploadNotFound):
		return safe(
			"ARTIFACT_PART_MISSING", "Artifact upload parts are missing",
			ErrPartMissing,
		)
	case errors.Is(err, ErrObjectNotFound):
		return safe(
			"ARTIFACT_NOT_AVAILABLE", "Artifact bytes are not available",
			ErrNotAvailable,
		)
	default:
		return safe(
			"ARTIFACT_STORAGE_UNAVAILABLE",
			"Artifact storage is temporarily unavailable",
			ErrStorage,
		)
	}
}

func (service Service) publicUpload(upload UploadSession) PublicUploadSession {
	mode := "multipart"
	transfer := "direct"
	partCount := upload.PartCount
	if isSyntheticUpload(upload) {
		mode = "deduplicated"
		transfer = "none"
		partCount = 0
	} else if service.Storage.Backend() == "local" {
		transfer = "local_proxy"
	}
	parts := upload.Parts
	if parts == nil {
		parts = []UploadPart{}
	}
	return PublicUploadSession{
		UploadID: upload.ID, ArtifactID: upload.ArtifactID,
		VersionID: upload.VersionID, UploadMode: mode,
		TransferMode: transfer, Status: upload.Status,
		SizeBytes: upload.ExpectedSize, SHA256: upload.ExpectedSHA256,
		PartSizeBytes: upload.PartSizeBytes, PartCount: partCount,
		CompletedParts: parts, ExpiresAt: upload.ExpiresAt,
		CreatedAt: upload.CreatedAt, UpdatedAt: upload.UpdatedAt,
	}
}

func (service Service) uploadExpired(upload UploadSession) bool {
	return isActiveUpload(upload.Status) && !upload.ExpiresAt.After(service.now())
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock.Now().UTC()
}

func (service Service) sessionTTL() time.Duration {
	if service.UploadSessionTTL <= 0 {
		return 24 * time.Hour
	}
	return service.UploadSessionTTL
}

func (service Service) transferTTL() time.Duration {
	if service.TransferTTL <= 0 {
		return 15 * time.Minute
	}
	return service.TransferTTL
}

func (service Service) confirmLease() time.Duration {
	if service.ConfirmRecoveryLease <= 0 {
		return 2 * time.Minute
	}
	return service.ConfirmRecoveryLease
}

// ContentObjectKey is the fixed project-local content-addressed layout.
func ContentObjectKey(projectID, sha256 string) string {
	if projectID == "" || len(sha256) < 2 {
		return ""
	}
	return path.Join(
		"projects", projectID, "blobs", "sha256", sha256[:2], sha256,
	)
}

func providerHandle(upload UploadSession) MultipartUpload {
	return MultipartUpload{
		ObjectKey:        upload.StagingKey,
		ProviderUploadID: upload.ProviderUploadID,
	}
}

func completedToUploadParts(
	parts []CompletedPart,
	completedAt time.Time,
) []UploadPart {
	result := make([]UploadPart, len(parts))
	for index, part := range parts {
		result[index] = UploadPart{
			PartNumber: part.PartNumber, SizeBytes: part.SizeBytes,
			ETag: normalizeETag(part.ETag), CompletedAt: completedAt,
		}
	}
	return result
}

func isSyntheticUpload(upload UploadSession) bool {
	return strings.HasPrefix(upload.ProviderUploadID, "deduplicated:") ||
		strings.HasPrefix(upload.ProviderUploadID, "restored:")
}

func isActiveUpload(status string) bool {
	return status == UploadInitialized ||
		status == UploadUploading ||
		status == UploadCompleting ||
		status == UploadVerifying
}

func isPublicKind(value string) bool {
	return value == KindProblem || value == KindAttachment || value == KindOther
}

func isInitialKind(value string) bool {
	return isPublicKind(value) || value == KindAgent
}

func (service Service) uploadOwnedBy(identity auth.Identity, upload UploadSession) bool {
	if identity.Kind == "agent" {
		return identity.AgentInstanceID != "" &&
			upload.AgentInstanceID == identity.AgentInstanceID
	}
	return true
}

func validFilename(value string) bool {
	return value != "" && len(value) <= 255 &&
		!strings.ContainsAny(value, "\x00\r\n/\\")
}

func validListFilter(filter ListFilter) bool {
	if filter.Kind != "" &&
		filter.Kind != KindProblem &&
		filter.Kind != KindAttachment &&
		filter.Kind != KindExperimentResult &&
		filter.Kind != KindModelFile &&
		filter.Kind != KindArticleBuild &&
		filter.Kind != KindAgent &&
		filter.Kind != KindOther {
		return false
	}
	if filter.Source != "" &&
		filter.Source != SourceUserUpload &&
		filter.Source != SourceExperiment &&
		filter.Source != SourceModel &&
		filter.Source != SourceArticle &&
		filter.Source != SourceAgent &&
		filter.Source != SourceSystem {
		return false
	}
	if filter.Status != "" &&
		filter.Status != StatusPendingUpload &&
		filter.Status != StatusVerifying &&
		filter.Status != StatusAvailable &&
		filter.Status != StatusFailed &&
		filter.Status != StatusTrashed {
		return false
	}
	return len(strings.TrimSpace(filter.Tag)) <= 64
}

func matchesInitial(
	upload UploadSession,
	actorID string,
	input InitializeUploadInput,
) bool {
	return upload.CreatedBy == actorID &&
		upload.VersionNo == 1 &&
		upload.ExpectedSHA256 == input.SHA256 &&
		upload.ExpectedSize == input.SizeBytes &&
		upload.MIMEType == input.MIMEType &&
		upload.Filename == input.Filename &&
		upload.AgentSessionID == input.AgentSessionID &&
		upload.AgentRunID == input.AgentRunID
}

func matchesVersion(
	upload UploadSession,
	actorID string,
	artifactID string,
	input InitializeVersionInput,
) bool {
	return upload.CreatedBy == actorID &&
		upload.ArtifactID == artifactID &&
		upload.ExpectedSHA256 == input.SHA256 &&
		upload.ExpectedSize == input.SizeBytes &&
		upload.MIMEType == input.MIMEType &&
		upload.Filename == input.Filename
}
