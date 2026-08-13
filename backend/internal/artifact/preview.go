package artifact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
)

const (
	previewResultMaxBytes       = 64 * 1024
	defaultPreviewOutputBytes   = 4 * 1024 * 1024
	previewTransferInput        = "input"
	previewTransferOutput       = "output"
	transferPreviewInput        = "preview_input"
	transferPreviewOutput       = "preview_output"
	transferPreviewDownload     = "preview_download"
	previewJobType              = "artifact.preview"
	previewOutputPartNumber     = 1
	previewFailureMessageLength = 2000
)

// PreviewJobAccess resolves a claimed Worker Job without exposing Job storage.
type PreviewJobAccess interface {
	ClaimedWorkerJob(context.Context, auth.Identity, string) (jobs.Job, error)
}

// TransferContent is safe HTTP metadata for a signed streaming response.
type TransferContent struct {
	Filename  string
	MIMEType  string
	SizeBytes int64
}

// ListPreviews returns bounded preview state and signs only available bytes.
func (service Service) ListPreviews(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	artifactID string,
	versionID string,
) (PreviewList, error) {
	if err := service.authorize(
		ctx, identity, projectID, project.PermissionArtifactRead,
	); err != nil {
		return PreviewList{}, err
	}
	detail, err := service.Store.GetDetail(ctx, projectID, artifactID, false)
	if err != nil || detail.Artifact.Status != StatusAvailable {
		return PreviewList{}, ErrNotFound
	}
	version, err := service.Store.GetVersion(
		ctx, projectID, artifactID, versionID,
	)
	if err != nil || version.Status != StatusAvailable {
		return PreviewList{}, ErrNotFound
	}
	previews, err := service.Store.ListPreviews(
		ctx, projectID, artifactID, versionID,
	)
	if err != nil {
		return PreviewList{}, err
	}
	for index := range previews.Items {
		preview := &previews.Items[index]
		if preview.Status != PreviewAvailable || preview.ObjectKey == "" {
			continue
		}
		grant, err := service.previewDownloadTransfer(ctx, *preview)
		if err != nil {
			return PreviewList{}, err
		}
		preview.Transfer = &grant
	}
	return previews, nil
}

// PreviewJobTransfer issues a short-lived input or output grant only for the
// immutable Version and preview target carried by a running Artifact Job.
func (service Service) PreviewJobTransfer(
	ctx context.Context,
	identity auth.Identity,
	jobID string,
	input PreviewTransferInput,
) (TransferGrant, error) {
	if service.Jobs == nil || service.Signer == nil || service.Storage == nil {
		return TransferGrant{}, ErrNotAvailable
	}
	job, err := service.Jobs.ClaimedWorkerJob(ctx, identity, jobID)
	if err != nil {
		return TransferGrant{}, mapJobError(err)
	}
	target, err := previewTarget(job)
	if err != nil {
		return TransferGrant{}, err
	}
	preview, err := service.Store.GetPreviewByJob(ctx, job.ID)
	if err != nil ||
		preview.ProjectID != target.ProjectID ||
		preview.ArtifactID != target.ArtifactID ||
		preview.VersionID != target.VersionID ||
		preview.ID != target.PreviewID ||
		preview.PreviewType != target.PreviewType {
		return TransferGrant{}, ErrNotFound
	}
	input.Direction = strings.TrimSpace(input.Direction)
	input.VersionID = strings.TrimSpace(input.VersionID)
	if input.VersionID != target.VersionID {
		return TransferGrant{}, ErrInvalid
	}
	switch input.Direction {
	case previewTransferInput:
		return service.previewInputTransfer(ctx, job, target)
	case previewTransferOutput:
		return service.previewOutputTransfer(ctx, job, target, input)
	default:
		return TransferGrant{}, ErrInvalid
	}
}

func (service Service) previewInputTransfer(
	ctx context.Context,
	job jobs.Job,
	target previewJobTarget,
) (TransferGrant, error) {
	version, err := service.Store.GetVersion(
		ctx, target.ProjectID, target.ArtifactID, target.VersionID,
	)
	if err != nil ||
		version.Status != StatusAvailable ||
		(version.StorageClass != "object" && version.StorageClass != "git") {
		return TransferGrant{}, ErrNotFound
	}
	if version.StorageClass == "object" &&
		(version.ObjectKey == "" || version.Backend != service.Storage.Backend()) {
		return TransferGrant{}, ErrNotFound
	}
	if version.StorageClass == "git" &&
		(version.GitReference == nil || service.Git == nil) {
		return TransferGrant{}, ErrNotFound
	}
	grant, err := service.workerSigner().Sign(TransferClaims{
		Kind: transferPreviewInput, ProjectID: target.ProjectID,
		ArtifactID: target.ArtifactID, VersionID: target.VersionID,
		PreviewID: target.PreviewID, JobID: job.ID,
		SizeBytes: version.SizeBytes,
	}, service.now(), service.transferTTL())
	if err == nil {
		service.record(
			ctx, "artifact.preview.input.signed", "success", target.ProjectID,
			target.ArtifactID, map[string]interface{}{
				"job_id": job.ID, "version_id": target.VersionID,
			},
		)
	}
	return grant, err
}

func (service Service) previewOutputTransfer(
	ctx context.Context,
	job jobs.Job,
	target previewJobTarget,
	input PreviewTransferInput,
) (TransferGrant, error) {
	input.PreviewType = strings.TrimSpace(input.PreviewType)
	input.Filename = strings.TrimSpace(input.Filename)
	input.MIMEType = strings.ToLower(strings.TrimSpace(input.MIMEType))
	input.SHA256 = strings.ToLower(strings.TrimSpace(input.SHA256))
	if input.PreviewType != PreviewThumbnail ||
		!validFilename(input.Filename) ||
		(input.MIMEType != "image/png" && input.MIMEType != "image/jpeg") ||
		input.SizeBytes < 0 ||
		input.SizeBytes > service.maxPreviewOutputBytes() ||
		!sha256Pattern.MatchString(input.SHA256) {
		return TransferGrant{}, ErrInvalid
	}
	if existing, err := service.Store.GetPreviewTransfer(
		ctx, job.ID, input.PreviewType,
	); err == nil {
		if !matchesPreviewTransfer(existing, input, service.Storage.Backend()) ||
			existing.Status == "expired" || existing.Status == "aborted" {
			return TransferGrant{}, ErrUploadConflict
		}
		return service.signPreviewOutput(existing)
	} else if !errors.Is(err, ErrNotFound) {
		return TransferGrant{}, err
	}

	transferID, err := service.Generator.New()
	if err != nil {
		return TransferGrant{}, err
	}
	now := service.now()
	stagingKey := path.Join(
		"projects", target.ProjectID, "preview-staging", transferID,
	)
	provider, err := service.Storage.CreateMultipart(
		ctx, stagingKey, input.MIMEType,
	)
	if err != nil {
		return TransferGrant{}, service.storageError(err)
	}
	prepared := PreviewTransfer{
		ID: transferID, JobID: job.ID, ProjectID: target.ProjectID,
		ArtifactID: target.ArtifactID, VersionID: target.VersionID,
		PreviewType: input.PreviewType, Backend: service.Storage.Backend(),
		ProviderUploadID: provider.ProviderUploadID, StagingKey: stagingKey,
		Filename: input.Filename, MIMEType: input.MIMEType,
		ExpectedSize: input.SizeBytes, ExpectedSHA256: input.SHA256,
		ExpiresAt: now.Add(service.transferTTL()), CreatedAt: now, UpdatedAt: now,
	}
	persisted, created, err := service.Store.CreatePreviewTransfer(ctx, prepared)
	if err != nil {
		_ = service.Storage.AbortMultipart(ctx, provider)
		return TransferGrant{}, err
	}
	if !created {
		_ = service.Storage.AbortMultipart(ctx, provider)
		if !matchesPreviewTransfer(persisted, input, service.Storage.Backend()) {
			return TransferGrant{}, ErrUploadConflict
		}
	}
	grant, err := service.signPreviewOutput(persisted)
	if err == nil {
		service.record(
			ctx, "artifact.preview.output.signed", "success", target.ProjectID,
			target.ArtifactID, map[string]interface{}{
				"job_id": job.ID, "preview_type": input.PreviewType,
			},
		)
	}
	return grant, err
}

func (service Service) signPreviewOutput(
	transfer PreviewTransfer,
) (TransferGrant, error) {
	remaining := transfer.ExpiresAt.Sub(service.now())
	if remaining <= 0 {
		return TransferGrant{}, ErrTransferExpired
	}
	ttl := service.transferTTL()
	if remaining < ttl {
		ttl = remaining
	}
	return service.workerSigner().Sign(TransferClaims{
		Kind: transferPreviewOutput, ProjectID: transfer.ProjectID,
		ArtifactID: transfer.ArtifactID, VersionID: transfer.VersionID,
		PreviewID: transfer.ID, JobID: transfer.JobID,
		PreviewType: transfer.PreviewType, SizeBytes: transfer.ExpectedSize,
	}, service.now(), ttl)
}

// PutSignedTransfer streams a browser upload part or a Worker preview output
// directly into the configured provider without buffering the complete part.
func (service Service) PutSignedTransfer(
	ctx context.Context,
	token string,
	body io.Reader,
	contentLength int64,
) (CompletedPart, error) {
	claims, err := service.Signer.Verify(token, service.now())
	if err != nil {
		return CompletedPart{}, err
	}
	requestctx.SetProject(ctx, claims.ProjectID)
	if claims.Kind == transferUploadPart {
		return service.PutSignedPart(ctx, token, body, contentLength)
	}
	if claims.Kind != transferPreviewOutput ||
		(contentLength >= 0 && contentLength != claims.SizeBytes) {
		return CompletedPart{}, ErrPartInvalid
	}
	transfer, err := service.Store.GetPreviewTransfer(
		ctx, claims.JobID, claims.PreviewType,
	)
	if err != nil ||
		transfer.ID != claims.PreviewID ||
		transfer.ProjectID != claims.ProjectID ||
		transfer.ArtifactID != claims.ArtifactID ||
		transfer.VersionID != claims.VersionID ||
		transfer.ExpectedSize != claims.SizeBytes ||
		transfer.Backend != service.Storage.Backend() ||
		transfer.ExpiresAt.Before(service.now()) ||
		(transfer.Status != "prepared" && transfer.Status != "uploaded") {
		return CompletedPart{}, ErrNotFound
	}
	part, err := service.Storage.PutPart(
		ctx, previewProviderHandle(transfer), previewOutputPartNumber,
		body, transfer.ExpectedSize,
	)
	if err != nil {
		return CompletedPart{}, service.storageError(err)
	}
	if part.PartNumber != previewOutputPartNumber ||
		part.SizeBytes != transfer.ExpectedSize {
		return CompletedPart{}, ErrSizeMismatch
	}
	if err := service.Store.MarkPreviewTransferUploaded(
		ctx, transfer.ID, part.ETag, service.now(),
	); err != nil {
		return CompletedPart{}, err
	}
	return part, nil
}

// OpenSignedTransfer streams an Artifact Version, Worker input, or thumbnail.
func (service Service) OpenSignedTransfer(
	ctx context.Context,
	token string,
) (io.ReadCloser, TransferContent, error) {
	claims, err := service.Signer.Verify(token, service.now())
	if err != nil {
		if errors.Is(err, ErrTransferExpired) {
			return nil, TransferContent{}, err
		}
		return nil, TransferContent{}, ErrNotFound
	}
	requestctx.SetProject(ctx, claims.ProjectID)
	switch claims.Kind {
	case transferDownload:
		reader, version, err := service.OpenSignedDownload(ctx, token)
		return reader, TransferContent{
			Filename: version.Filename, MIMEType: version.MIMEType,
			SizeBytes: version.SizeBytes,
		}, err
	case transferPreviewInput:
		return service.openPreviewInput(ctx, claims)
	case transferPreviewDownload:
		return service.openPreviewDownload(ctx, claims)
	default:
		return nil, TransferContent{}, ErrInvalid
	}
}

func (service Service) openPreviewInput(
	ctx context.Context,
	claims TransferClaims,
) (io.ReadCloser, TransferContent, error) {
	if service.Jobs == nil {
		return nil, TransferContent{}, ErrNotFound
	}
	preview, err := service.Store.GetPreviewByJob(ctx, claims.JobID)
	if err != nil ||
		preview.ID != claims.PreviewID ||
		preview.ProjectID != claims.ProjectID ||
		preview.ArtifactID != claims.ArtifactID ||
		preview.VersionID != claims.VersionID {
		return nil, TransferContent{}, ErrNotFound
	}
	version, err := service.Store.GetVersion(
		ctx, claims.ProjectID, claims.ArtifactID, claims.VersionID,
	)
	if err != nil || version.Status != StatusAvailable ||
		version.SizeBytes != claims.SizeBytes {
		return nil, TransferContent{}, ErrNotFound
	}
	reader, err := service.openVersion(ctx, version)
	if err != nil {
		return nil, TransferContent{}, err
	}
	return reader, TransferContent{
		Filename: version.Filename, MIMEType: version.MIMEType,
		SizeBytes: version.SizeBytes,
	}, nil
}

func (service Service) openPreviewDownload(
	ctx context.Context,
	claims TransferClaims,
) (io.ReadCloser, TransferContent, error) {
	detail, err := service.Store.GetDetail(
		ctx, claims.ProjectID, claims.ArtifactID, false,
	)
	if err != nil || detail.Artifact.Status != StatusAvailable {
		return nil, TransferContent{}, ErrNotFound
	}
	preview, err := service.Store.GetPreview(
		ctx, claims.ProjectID, claims.ArtifactID, claims.VersionID,
		claims.PreviewID,
	)
	if err != nil ||
		preview.Status != PreviewAvailable ||
		preview.SizeBytes != claims.SizeBytes ||
		preview.ObjectKey == "" ||
		preview.Backend != service.Storage.Backend() {
		return nil, TransferContent{}, ErrNotFound
	}
	reader, err := service.Storage.Open(ctx, preview.ObjectKey)
	if err != nil {
		return nil, TransferContent{}, service.storageError(err)
	}
	return reader, TransferContent{
		Filename: preview.Filename, MIMEType: preview.MIMEType,
		SizeBytes: preview.SizeBytes,
	}, nil
}

func (service Service) openVersion(
	ctx context.Context,
	version Version,
) (io.ReadCloser, error) {
	if version.StorageClass == "git" {
		if version.GitReference == nil || service.Git == nil {
			return nil, ErrNotFound
		}
		reader, sizeBytes, err := service.Git.Open(
			ctx, version.ProjectID, *version.GitReference,
		)
		if err != nil || sizeBytes != version.SizeBytes {
			if reader != nil {
				_ = reader.Close()
			}
			return nil, ErrNotFound
		}
		return reader, nil
	}
	if version.StorageClass != "object" ||
		version.ObjectKey == "" ||
		version.Backend != service.Storage.Backend() {
		return nil, ErrNotFound
	}
	reader, err := service.Storage.Open(ctx, version.ObjectKey)
	if err != nil {
		return nil, service.storageError(err)
	}
	return reader, nil
}

func (service Service) previewDownloadTransfer(
	ctx context.Context,
	preview Preview,
) (TransferGrant, error) {
	if preview.Backend != service.Storage.Backend() {
		return TransferGrant{}, ErrNotAvailable
	}
	if service.Storage.Backend() != "local" {
		signed, err := service.Storage.PresignGet(
			ctx,
			preview.ObjectKey,
			service.transferTTL(),
			GetObjectOptions{},
		)
		if err != nil {
			return TransferGrant{}, service.storageError(err)
		}
		return TransferGrant{
			Method: signed.Method, URL: signed.URL, Headers: signed.Headers,
			ExpiresAt: signed.ExpiresAt,
		}, nil
	}
	return service.Signer.Sign(TransferClaims{
		Kind: transferPreviewDownload, ProjectID: preview.ProjectID,
		ArtifactID: preview.ArtifactID, VersionID: preview.VersionID,
		PreviewID: preview.ID, PreviewType: preview.PreviewType,
		SizeBytes: preview.SizeBytes,
	}, service.now(), service.transferTTL())
}

// PrepareComplete validates and promotes Worker output before Job completion.
func (service Service) PrepareComplete(
	ctx context.Context,
	job jobs.Job,
	result map[string]interface{},
) error {
	if job.JobType == semanticDescriptionJobType {
		_, err := parseSemanticResult(result)
		return err
	}
	if job.JobType != previewJobType {
		return nil
	}
	if job.CancelRequestedAt != nil ||
		(job.TimeoutAt != nil && !job.TimeoutAt.After(service.now())) {
		return jobs.ErrLeaseLost
	}
	parsed, err := parsePreviewResult(job, result)
	if err != nil {
		return err
	}
	for _, output := range parsed.Outputs {
		transfer, err := service.Store.GetPreviewTransfer(
			ctx, job.ID, output.PreviewType,
		)
		if err != nil ||
			transfer.ProjectID != parsed.ProjectID ||
			transfer.ArtifactID != parsed.ArtifactID ||
			transfer.VersionID != parsed.VersionID ||
			transfer.Backend != service.Storage.Backend() ||
			transfer.ExpectedSize > service.maxPreviewOutputBytes() ||
			normalizeETag(transfer.ProviderETag) != output.ETag {
			return jobs.ErrInvalid
		}
		if err := service.completePreviewOutput(ctx, transfer, output); err != nil {
			return err
		}
	}
	return nil
}

// ClaimInTransaction moves the domain preview projection with its Job lease.
func (service Service) ClaimInTransaction(
	ctx context.Context,
	tx transaction.Tx,
	job jobs.Job,
) error {
	return service.Store.UpdatePreviewJobInTransaction(
		ctx, tx, job, "", "", service.now(),
	)
}

// CompleteInTransaction updates preview and registry state atomically with Job.
func (service Service) CompleteInTransaction(
	ctx context.Context,
	tx transaction.Tx,
	job jobs.Job,
	result map[string]interface{},
) error {
	if job.JobType == semanticDescriptionJobType {
		return service.completeSemanticDescription(ctx, tx, job, result)
	}
	if job.JobType != previewJobType {
		return nil
	}
	parsed, err := parsePreviewResult(job, result)
	if err != nil {
		return err
	}
	return service.Store.CompletePreviewInTransaction(
		ctx, tx, job, parsed, service.now(),
	)
}

// FailInTransaction mirrors retry and terminal failure state atomically.
func (service Service) FailInTransaction(
	ctx context.Context,
	tx transaction.Tx,
	job jobs.Job,
	failure jobs.Failure,
) error {
	if job.JobType != previewJobType {
		return nil
	}
	return service.Store.UpdatePreviewJobInTransaction(
		ctx, tx, job, boundedString(failure.Code, 200),
		boundedString(failure.Message, previewFailureMessageLength), service.now(),
	)
}

func (service Service) completePreviewOutput(
	ctx context.Context,
	transfer PreviewTransfer,
	output previewResultOutput,
) error {
	if transfer.Status == "expired" || transfer.Status == "aborted" {
		return jobs.ErrInvalid
	}
	contentKey := ContentObjectKey(
		transfer.ProjectID, transfer.ExpectedSHA256,
	)
	if transfer.Status == "completed" {
		return service.verifyObject(
			ctx, contentKey, transfer.ExpectedSize, transfer.ExpectedSHA256,
		)
	}
	handle := previewProviderHandle(transfer)
	parts, listErr := service.Storage.ListParts(ctx, handle)
	if listErr == nil {
		if len(parts) != 1 ||
			parts[0].PartNumber != previewOutputPartNumber ||
			parts[0].SizeBytes != transfer.ExpectedSize ||
			normalizeETag(parts[0].ETag) != output.ETag {
			return jobs.ErrInvalid
		}
		if _, err := service.Storage.CompleteMultipart(
			ctx, handle, parts,
		); err != nil {
			return service.storageError(err)
		}
	} else if !errors.Is(listErr, ErrUploadNotFound) {
		return service.storageError(listErr)
	}

	verificationKey := transfer.StagingKey
	if _, err := service.Storage.Stat(ctx, verificationKey); err != nil {
		if _, contentErr := service.Storage.Stat(ctx, contentKey); contentErr != nil {
			if listErr != nil {
				return service.storageError(listErr)
			}
			return service.storageError(err)
		}
		verificationKey = contentKey
	}
	if err := service.verifyObject(
		ctx, verificationKey, transfer.ExpectedSize, transfer.ExpectedSHA256,
	); err != nil {
		return err
	}
	if verificationKey != contentKey {
		return service.promoteVerified(ctx, UploadSession{
			ProjectID: transfer.ProjectID, StagingKey: transfer.StagingKey,
			ExpectedSize:   transfer.ExpectedSize,
			ExpectedSHA256: transfer.ExpectedSHA256,
		}, contentKey)
	}
	return nil
}

type previewJobTarget struct {
	ProjectID   string
	ArtifactID  string
	VersionID   string
	PreviewID   string
	PreviewType string
}

func previewTarget(job jobs.Job) (previewJobTarget, error) {
	if job.JobType != previewJobType || job.Payload == nil {
		return previewJobTarget{}, ErrNotFound
	}
	target := previewJobTarget{
		ProjectID:   payloadString(job.Payload, "project_id"),
		ArtifactID:  payloadString(job.Payload, "artifact_id"),
		VersionID:   payloadString(job.Payload, "version_id"),
		PreviewID:   payloadString(job.Payload, "preview_id"),
		PreviewType: payloadString(job.Payload, "preview_type"),
	}
	if target.ProjectID != job.ProjectID ||
		!uuidPattern.MatchString(target.ProjectID) ||
		!uuidPattern.MatchString(target.ArtifactID) ||
		!uuidPattern.MatchString(target.VersionID) ||
		!uuidPattern.MatchString(target.PreviewID) ||
		!validPrimaryPreviewType(target.PreviewType) {
		return previewJobTarget{}, ErrInvalid
	}
	return target, nil
}

func parsePreviewResult(
	job jobs.Job,
	raw map[string]interface{},
) (previewResult, error) {
	target, err := previewTarget(job)
	if err != nil {
		return previewResult{}, jobs.ErrInvalid
	}
	encoded, err := json.Marshal(raw)
	if err != nil || len(encoded) > previewResultMaxBytes {
		return previewResult{}, jobs.ErrInvalid
	}
	var result previewResult
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil ||
		decoder.Decode(&struct{}{}) != io.EOF {
		return previewResult{}, jobs.ErrInvalid
	}
	if result.ProjectID != target.ProjectID ||
		result.ArtifactID != target.ArtifactID ||
		result.VersionID != target.VersionID ||
		result.PreviewID != target.PreviewID ||
		result.PreviewType != target.PreviewType ||
		result.StructuralSummary == nil ||
		(result.Status != PreviewAvailable && result.Status != PreviewUnsupported) ||
		len(result.ErrorCode) > 200 ||
		len(result.Outputs) > 1 {
		return previewResult{}, jobs.ErrInvalid
	}
	if result.Status == PreviewAvailable && result.ErrorCode != "" {
		return previewResult{}, jobs.ErrInvalid
	}
	if result.Status == PreviewUnsupported &&
		(result.ErrorCode == "" || len(result.Outputs) != 0) {
		return previewResult{}, jobs.ErrInvalid
	}
	for index := range result.Outputs {
		result.Outputs[index].PreviewType = strings.TrimSpace(
			result.Outputs[index].PreviewType,
		)
		result.Outputs[index].ETag = normalizeETag(result.Outputs[index].ETag)
		if result.Outputs[index].PreviewType != PreviewThumbnail ||
			result.Outputs[index].ETag == "" ||
			len(result.Outputs[index].ETag) > 1024 {
			return previewResult{}, jobs.ErrInvalid
		}
	}
	return result, nil
}

func matchesPreviewTransfer(
	transfer PreviewTransfer,
	input PreviewTransferInput,
	backend string,
) bool {
	return transfer.VersionID == input.VersionID &&
		transfer.PreviewType == input.PreviewType &&
		transfer.Backend == backend &&
		transfer.Filename == input.Filename &&
		transfer.MIMEType == input.MIMEType &&
		transfer.ExpectedSize == input.SizeBytes &&
		transfer.ExpectedSHA256 == input.SHA256
}

func previewProviderHandle(transfer PreviewTransfer) MultipartUpload {
	return MultipartUpload{
		ObjectKey:        transfer.StagingKey,
		ProviderUploadID: transfer.ProviderUploadID,
	}
}

func validPrimaryPreviewType(value string) bool {
	switch value {
	case PreviewImage, PreviewPDF, PreviewCSV, PreviewJSON, PreviewText:
		return true
	default:
		return false
	}
}

func payloadString(payload map[string]interface{}, key string) string {
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func boundedString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func mapJobError(err error) error {
	switch {
	case errors.Is(err, jobs.ErrForbidden), errors.Is(err, jobs.ErrWorkerToken):
		return ErrForbidden
	case errors.Is(err, jobs.ErrNotFound), errors.Is(err, jobs.ErrLeaseLost):
		return ErrNotFound
	case errors.Is(err, jobs.ErrInvalid):
		return ErrInvalid
	default:
		return fmt.Errorf("resolve Artifact preview Job: %w", err)
	}
}

func (service Service) maxPreviewOutputBytes() int64 {
	if service.MaxPreviewOutputBytes <= 0 {
		return defaultPreviewOutputBytes
	}
	return service.MaxPreviewOutputBytes
}

func (service Service) workerSigner() *TransferSigner {
	if service.WorkerSigner != nil {
		return service.WorkerSigner
	}
	return service.Signer
}

// ExpirePreviewTransfers aborts only unconfirmed, regenerable preview staging.
func (service Service) ExpirePreviewTransfers(
	ctx context.Context,
	limit int,
) (int, error) {
	now := service.now()
	if err := service.Store.ReconcilePreviewJobs(ctx, now); err != nil {
		return 0, err
	}
	transfers, err := service.Store.ExpirePreviewTransfers(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	var firstErr error
	for _, transfer := range transfers {
		if transfer.Backend != service.Storage.Backend() {
			if firstErr == nil {
				firstErr = ErrStorage
			}
			continue
		}
		err := service.Storage.AbortMultipart(
			ctx, previewProviderHandle(transfer),
		)
		if err != nil && !errors.Is(err, ErrUploadNotFound) {
			if firstErr == nil {
				firstErr = service.storageError(err)
			}
			continue
		}
		if err := service.Store.MarkPreviewTransferAborted(
			ctx, transfer.ID, service.now(),
		); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return len(transfers), firstErr
}
