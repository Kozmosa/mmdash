package artifact

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
)

const articleTemplateMIME = "application/zip"

func isBuiltInArticleTemplate(value Artifact) bool {
	if value.Source != SourceArticle {
		return false
	}
	for _, tag := range value.Tags {
		if tag == "article-template" {
			return true
		}
	}
	return false
}

// ArticleTemplateGrant returns a job-scoped immutable template transfer. The
// Worker receives neither a storage provider handle nor a reusable browser
// credential.
func (service Service) ArticleTemplateGrant(ctx context.Context, projectID, artifactID, versionID string) (map[string]interface{}, error) {
	grant, err := service.ArticleResourceGrant(ctx, projectID, artifactID, versionID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"method": grant["method"], "url": grant["url"],
		"headers": grant["headers"], "expires_at": grant["expires_at"],
	}, nil
}

// ArticleResourceGrant returns an immutable, job-scoped input transfer for a
// template, figure, table, or attachment pinned by Article. Worker inputs
// always stream through Core's internal transfer origin, regardless of the
// configured storage backend, so the Worker does not need public DNS or object
// storage credentials.
func (service Service) ArticleResourceGrant(ctx context.Context, projectID, artifactID, versionID string) (map[string]interface{}, error) {
	detail, err := service.Store.GetDetail(ctx, projectID, artifactID, false)
	if err != nil {
		return nil, err
	}
	if detail.Artifact.Status != StatusAvailable {
		return nil, ErrNotAvailable
	}
	version, err := service.Store.GetVersion(ctx, projectID, artifactID, versionID)
	if err != nil {
		return nil, err
	}
	if version.Status != StatusAvailable || version.StorageClass != "object" {
		return nil, ErrNotAvailable
	}
	signer := service.WorkerSigner
	if signer == nil {
		return nil, ErrNotAvailable
	}
	transfer, err := signer.Sign(TransferClaims{Kind: transferDownload, ProjectID: projectID, ArtifactID: artifactID, VersionID: versionID, SizeBytes: version.SizeBytes}, service.now(), service.transferTTL())
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"method": transfer.Method, "url": transfer.URL, "headers": transfer.Headers,
		"expires_at": transfer.ExpiresAt, "filename": version.Filename,
		"mime_type": version.MIMEType, "size_bytes": version.SizeBytes,
		"sha256": version.SHA256,
	}, nil
}

// ArchiveArticleTemplate stores one immutable Article template attachment.
// It is an internal Core boundary: bytes are staged, verified, promoted, and
// finalized through the same Artifact storage path as other object uploads.
// The idempotency key is scoped to projectID and is retained on the upload
// session so retries return the original Artifact and Version identifiers.
func (service Service) ArchiveArticleTemplate(
	ctx context.Context,
	projectID, createdBy, filename, idempotencyKey, expectedSHA string,
	expectedSize int64, input io.Reader,
) (artifactID, versionID string, err error) {
	filename = strings.TrimSpace(filename)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	expectedSHA = strings.ToLower(strings.TrimSpace(expectedSHA))
	if projectID == "" || createdBy == "" || !validFilename(filename) ||
		idempotencyKey == "" || len(idempotencyKey) > 200 ||
		expectedSize < 1 || expectedSize > service.MaxUploadBytes ||
		!sha256Pattern.MatchString(expectedSHA) || input == nil {
		return "", "", ErrInvalid
	}

	initial := InitializeUploadInput{
		Filename: filename, SizeBytes: expectedSize, SHA256: expectedSHA,
		MIMEType: articleTemplateMIME, Kind: KindAttachment,
		IdempotencyKey: idempotencyKey,
	}
	if existing, lookupErr := service.Store.GetUploadByIdempotency(
		ctx, projectID, idempotencyKey,
	); lookupErr == nil {
		if !matchesInitial(existing, createdBy, initial) {
			return "", "", ErrUploadConflict
		}
		if existing.Status == UploadCompleted {
			return existing.ArtifactID, existing.VersionID, nil
		}
		return "", "", ErrUploadConflict
	} else if !errors.Is(lookupErr, ErrNotFound) {
		return "", "", lookupErr
	}

	temporary, err := stageResultInput(input, expectedSize, expectedSHA)
	if err != nil {
		return "", "", err
	}
	defer os.Remove(temporary)
	temporaryFile, err := os.Open(temporary)
	if err != nil {
		return "", "", err
	}
	defer temporaryFile.Close()
	plan, err := CalculateMultipartPlan(
		expectedSize, service.MultipartPartBytes, service.MaxUploadBytes,
	)
	if err != nil {
		return "", "", err
	}
	artifactID, versionID, uploadID, err := service.newUploadIDs()
	if err != nil {
		return "", "", err
	}
	now := service.now()
	artifact := Artifact{
		ID: artifactID, ProjectID: projectID, Kind: KindAttachment,
		Source: SourceArticle, Tags: []string{"article-template"}, Name: filename,
		RecommendedUsage: []string{"article-template"}, CurrentVersionID: &versionID,
		Status: StatusPendingUpload, CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now,
	}
	version := Version{
		ID: versionID, ArtifactID: artifactID, ProjectID: projectID, VersionNo: 1,
		StorageClass: "object", Filename: filename, SHA256: expectedSHA,
		MIMEType: articleTemplateMIME, SizeBytes: expectedSize,
		Status: StatusPendingUpload, CreatedBy: createdBy, CreatedAt: now,
	}
	upload, providerUpload, err := service.prepareUpload(
		ctx, projectID, artifactID, versionID, uploadID, createdBy, filename,
		articleTemplateMIME, expectedSHA, expectedSize, idempotencyKey, plan,
	)
	if err != nil {
		return "", "", err
	}
	if upload.Status == UploadCompleted {
		artifact.Status = StatusAvailable
		version.Status = StatusAvailable
		version.BlobID = strings.TrimPrefix(upload.ProviderUploadID, "deduplicated:")
		version.AvailableAt = upload.CompletedAt
	}
	if err := service.Store.CreateFirst(ctx, artifact, version, upload); err != nil {
		service.abortPrepared(ctx, providerUpload)
		if errors.Is(err, ErrUploadConflict) {
			existing, findErr := service.Store.GetUploadByIdempotency(
				ctx, projectID, idempotencyKey,
			)
			if findErr == nil && matchesInitial(existing, createdBy, initial) &&
				existing.Status == UploadCompleted {
				return existing.ArtifactID, existing.VersionID, nil
			}
		}
		return "", "", err
	}
	if upload.Status == UploadCompleted {
		return artifactID, versionID, nil
	}

	provider := providerHandle(upload)
	parts := make([]CompletedPart, 0, plan.PartCount)
	var copied int64
	for partNumber := 1; partNumber <= plan.PartCount; partNumber++ {
		size, partErr := plan.PartSize(partNumber)
		if partErr != nil {
			return "", "", partErr
		}
		part, partErr := service.Storage.PutPart(
			ctx, provider, partNumber, io.LimitReader(temporaryFile, size), size,
		)
		if partErr != nil {
			return "", "", service.storageError(partErr)
		}
		copied += part.SizeBytes
		parts = append(parts, part)
	}
	if copied != expectedSize {
		return "", "", ErrSizeMismatch
	}
	if err := service.Store.UpsertParts(ctx, upload.ID, completedToUploadParts(parts, now)); err != nil {
		return "", "", err
	}
	if err := service.Store.MarkUploading(ctx, upload.ID, now); err != nil {
		return "", "", err
	}
	if _, err := service.Storage.CompleteMultipart(ctx, provider, parts); err != nil {
		return "", "", service.storageError(err)
	}
	if err := service.Store.SetUploadStatus(ctx, upload.ID, UploadVerifying, "", now); err != nil {
		return "", "", err
	}
	contentKey := ContentObjectKey(projectID, expectedSHA)
	if err := service.verifyObject(ctx, upload.StagingKey, expectedSize, expectedSHA); err != nil {
		return "", "", err
	}
	if err := service.promoteVerified(ctx, upload, contentKey); err != nil {
		return "", "", err
	}
	blobID, err := service.Generator.New()
	if err != nil {
		return "", "", err
	}
	if _, err = service.Store.FinalizeUpload(
		ctx, upload, Blob{ID: blobID, ProjectID: projectID, SHA256: expectedSHA,
			SizeBytes: expectedSize, Backend: service.Storage.Backend(), ObjectKey: contentKey},
		service.now(),
	); err != nil {
		return "", "", err
	}
	return artifactID, versionID, nil
}

// ArchiveArticleBuildOutput streams one immutable build output through the
// Artifact verification/promotion boundary. Article stores only the returned
// stable Artifact and Version identifiers.
func (service Service) ArchiveArticleBuildOutput(ctx context.Context, projectID, buildID, createdBy, role, filename, mimeType, expectedSHA string, expectedSize int64, input io.Reader) (string, string, error) {
	filename = strings.TrimSpace(filename)
	mimeType = strings.TrimSpace(mimeType)
	expectedSHA = strings.ToLower(strings.TrimSpace(expectedSHA))
	idempotencyKey := "article-build:" + buildID + ":" + role
	if projectID == "" || buildID == "" || createdBy == "" || role == "" ||
		!validFilename(filename) || mimeType == "" || input == nil ||
		expectedSize < 1 || expectedSize > service.MaxUploadBytes ||
		!sha256Pattern.MatchString(expectedSHA) {
		return "", "", ErrInvalid
	}
	initial := InitializeUploadInput{
		Filename: filename, SizeBytes: expectedSize, SHA256: expectedSHA,
		MIMEType: mimeType, Kind: KindArticleBuild,
		IdempotencyKey: idempotencyKey,
	}
	if existing, lookupErr := service.Store.GetUploadByIdempotency(
		ctx, projectID, idempotencyKey,
	); lookupErr == nil {
		if !matchesInitial(existing, createdBy, initial) {
			return "", "", ErrUploadConflict
		}
		if existing.Status == UploadCompleted {
			return existing.ArtifactID, existing.VersionID, nil
		}
		return "", "", ErrUploadConflict
	} else if !errors.Is(lookupErr, ErrNotFound) {
		return "", "", lookupErr
	}
	temporary, err := stageResultInput(input, expectedSize, expectedSHA)
	if err != nil {
		return "", "", err
	}
	defer os.Remove(temporary)
	temporaryFile, err := os.Open(temporary)
	if err != nil {
		return "", "", err
	}
	defer temporaryFile.Close()
	plan, err := CalculateMultipartPlan(expectedSize, service.MultipartPartBytes, service.MaxUploadBytes)
	if err != nil {
		return "", "", err
	}
	artifactID, err := service.Generator.New()
	if err != nil {
		return "", "", err
	}
	versionID, err := service.Generator.New()
	if err != nil {
		return "", "", err
	}
	uploadID, err := service.Generator.New()
	if err != nil {
		return "", "", err
	}
	now := service.now()
	sourceID := buildID
	artifact := Artifact{ID: artifactID, ProjectID: projectID, Kind: KindArticleBuild, Source: SourceArticle, SourceObjectID: &sourceID, Tags: []string{"article-build", role}, Name: filename, RecommendedUsage: []string{role}, CurrentVersionID: &versionID, Status: StatusPendingUpload, CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now}
	version := Version{ID: versionID, ArtifactID: artifactID, ProjectID: projectID, VersionNo: 1, StorageClass: "object", Filename: filename, SHA256: expectedSHA, MIMEType: mimeType, SizeBytes: expectedSize, Status: StatusPendingUpload, CreatedBy: createdBy, CreatedAt: now}
	upload, providerUpload, err := service.prepareUpload(ctx, projectID, artifactID, versionID, uploadID, createdBy, filename, mimeType, expectedSHA, expectedSize, idempotencyKey, plan)
	if err != nil {
		return "", "", err
	}
	if upload.Status == UploadCompleted {
		artifact.Status = StatusAvailable
		version.Status = StatusAvailable
		version.BlobID = strings.TrimPrefix(upload.ProviderUploadID, "deduplicated:")
		version.AvailableAt = upload.CompletedAt
	}
	if err := service.Store.CreateFirst(ctx, artifact, version, upload); err != nil {
		service.abortPrepared(ctx, providerUpload)
		if errors.Is(err, ErrUploadConflict) {
			existing, findErr := service.Store.GetUploadByIdempotency(
				ctx, projectID, idempotencyKey,
			)
			if findErr == nil && matchesInitial(existing, createdBy, initial) &&
				existing.Status == UploadCompleted {
				return existing.ArtifactID, existing.VersionID, nil
			}
		}
		return "", "", err
	}
	if upload.Status == UploadCompleted {
		return artifactID, versionID, nil
	}
	provider := providerHandle(upload)
	parts := make([]CompletedPart, 0, plan.PartCount)
	var copied int64
	for partNumber := 1; partNumber <= plan.PartCount; partNumber++ {
		size, err := plan.PartSize(partNumber)
		if err != nil {
			return "", "", err
		}
		part, err := service.Storage.PutPart(ctx, provider, partNumber, io.LimitReader(temporaryFile, size), size)
		if err != nil {
			return "", "", service.storageError(err)
		}
		copied += part.SizeBytes
		parts = append(parts, part)
	}
	if copied != expectedSize {
		return "", "", ErrSizeMismatch
	}
	if err := service.Store.UpsertParts(ctx, upload.ID, completedToUploadParts(parts, now)); err != nil {
		return "", "", err
	}
	if err := service.Store.MarkUploading(ctx, upload.ID, now); err != nil {
		return "", "", err
	}
	if _, err := service.Storage.CompleteMultipart(ctx, provider, parts); err != nil {
		return "", "", service.storageError(err)
	}
	if err := service.Store.SetUploadStatus(ctx, upload.ID, UploadVerifying, "", now); err != nil {
		return "", "", err
	}
	contentKey := ContentObjectKey(projectID, expectedSHA)
	if err := service.verifyObject(ctx, upload.StagingKey, expectedSize, expectedSHA); err != nil {
		return "", "", err
	}
	if err := service.promoteVerified(ctx, upload, contentKey); err != nil {
		return "", "", err
	}
	blobID, err := service.Generator.New()
	if err != nil {
		return "", "", err
	}
	if _, err = service.Store.FinalizeUpload(ctx, upload, Blob{ID: blobID, ProjectID: projectID, SHA256: expectedSHA, SizeBytes: expectedSize, Backend: service.Storage.Backend(), ObjectKey: contentKey}, service.now()); err != nil {
		return "", "", err
	}
	return artifactID, versionID, nil
}
