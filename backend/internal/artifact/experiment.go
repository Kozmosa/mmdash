package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

// ExperimentResultGrant issues a short-lived immutable download for the raw
// execution bundle. The Worker receives no storage-provider credential.
func (service Service) ExperimentResultGrant(
	ctx context.Context,
	projectID, experimentID, artifactID, versionID string,
) (map[string]interface{}, error) {
	if _, err := service.experimentVersion(
		ctx, projectID, experimentID, artifactID, versionID,
	); err != nil {
		return nil, err
	}
	return service.ArticleResourceGrant(ctx, projectID, artifactID, versionID)
}

// ValidateExperimentPointer verifies a self-run result pointer without
// granting storage access. The Artifact Version must be immutable, available,
// and owned by the same Project.
func (service Service) ValidateExperimentPointer(
	ctx context.Context,
	projectID, artifactID, versionID, expectedSHA string,
	expectedSize int64,
	expectedMediaType string,
) error {
	detail, err := service.Store.GetDetail(ctx, projectID, artifactID, false)
	if err != nil {
		return err
	}
	if detail.Artifact.ProjectID != projectID || detail.Artifact.Status != StatusAvailable {
		return ErrNotAvailable
	}
	version, err := service.Store.GetVersion(ctx, projectID, artifactID, versionID)
	if err != nil {
		return err
	}
	if version.ArtifactID != artifactID || version.ProjectID != projectID ||
		version.Status != StatusAvailable || version.SHA256 != expectedSHA ||
		version.SizeBytes != expectedSize || version.MIMEType != expectedMediaType {
		return ErrNotAvailable
	}
	return nil
}

// OpenExperimentResult opens the immutable raw bundle through the trusted
// Core boundary. Callers must close the returned reader.
func (service Service) OpenExperimentResult(
	ctx context.Context,
	projectID, experimentID, artifactID, versionID string,
) (io.ReadCloser, Version, error) {
	version, err := service.experimentVersion(
		ctx, projectID, experimentID, artifactID, versionID,
	)
	if err != nil {
		return nil, Version{}, err
	}
	reader, err := service.Storage.Open(ctx, version.ObjectKey)
	if err != nil {
		return nil, Version{}, service.storageError(err)
	}
	return reader, version, nil
}

func (service Service) experimentVersion(
	ctx context.Context,
	projectID, experimentID, artifactID, versionID string,
) (Version, error) {
	detail, err := service.Store.GetDetail(ctx, projectID, artifactID, false)
	if err != nil {
		return Version{}, err
	}
	if detail.Artifact.Kind != KindExperimentResult ||
		detail.Artifact.Source != SourceExperiment ||
		detail.Artifact.SourceObjectID == nil ||
		*detail.Artifact.SourceObjectID != experimentID ||
		detail.Artifact.Status != StatusAvailable {
		return Version{}, ErrNotAvailable
	}
	version, err := service.Store.GetVersion(ctx, projectID, artifactID, versionID)
	if err != nil {
		return Version{}, err
	}
	if version.Status != StatusAvailable || version.Filename != "execution-bundle.zip" ||
		version.ObjectKey == "" {
		return Version{}, ErrNotAvailable
	}
	return version, nil
}

// ArchiveExperimentFile stores one oversized result file as an immutable
// Artifact Version. The result branch records only its stable pointer.
func (service Service) ArchiveExperimentFile(
	ctx context.Context,
	projectID, experimentID, createdBy string,
	folderPath []string,
	resultPath, mimeType,
	expectedSHA string,
	expectedSize int64,
	input io.Reader,
) (Detail, error) {
	expectedSHA = strings.ToLower(strings.TrimSpace(expectedSHA))
	resultPath = strings.TrimSpace(resultPath)
	if projectID == "" || experimentID == "" || createdBy == "" ||
		input == nil || !safeZipPath(resultPath) || mimeType == "" || expectedSize < 1 ||
		expectedSize > service.MaxUploadBytes || !sha256Pattern.MatchString(expectedSHA) {
		return Detail{}, ErrInvalid
	}
	folderID, err := service.ensureManagedFolder(ctx, projectID, folderPath)
	if err != nil {
		return Detail{}, err
	}
	filename := path.Base(resultPath)
	pathHash := sha256.Sum256([]byte(resultPath))
	idempotencyKey := "experiment-file:" + experimentID + ":" + hex.EncodeToString(pathHash[:8])
	initial := InitializeUploadInput{
		Filename: filename, SizeBytes: expectedSize, SHA256: expectedSHA,
		MIMEType: mimeType, Kind: KindExperimentResult,
		FolderID: folderID, IdempotencyKey: idempotencyKey,
	}
	if existing, lookupErr := service.Store.GetUploadByIdempotency(
		ctx, projectID, idempotencyKey,
	); lookupErr == nil {
		if !matchesInitial(existing, createdBy, initial) {
			return Detail{}, ErrUploadConflict
		}
		if existing.Status != UploadCompleted {
			return Detail{}, ErrUploadConflict
		}
		if err := service.ensureManagedArtifactPlacement(
			ctx, projectID, existing.ArtifactID, folderID,
		); err != nil {
			return Detail{}, err
		}
		return service.Store.GetDetail(ctx, projectID, existing.ArtifactID, false)
	} else if !errors.Is(lookupErr, ErrNotFound) {
		return Detail{}, lookupErr
	}
	temporary, err := stageResultInput(input, expectedSize, expectedSHA)
	if err != nil {
		return Detail{}, err
	}
	defer os.Remove(temporary)
	temporaryFile, err := os.Open(temporary)
	if err != nil {
		return Detail{}, err
	}
	defer temporaryFile.Close()
	plan, err := CalculateMultipartPlan(expectedSize, service.MultipartPartBytes, service.MaxUploadBytes)
	if err != nil {
		return Detail{}, err
	}
	artifactID, err := service.Generator.New()
	if err != nil {
		return Detail{}, err
	}
	versionID, err := service.Generator.New()
	if err != nil {
		return Detail{}, err
	}
	uploadID, err := service.Generator.New()
	if err != nil {
		return Detail{}, err
	}
	now := service.now()
	sourceID := experimentID
	artifact := Artifact{
		ID: artifactID, ProjectID: projectID, Kind: KindExperimentResult,
		Source: SourceExperiment, SourceObjectID: &sourceID,
		Tags: []string{"experiment-result", "large-result-file"}, Name: resultPath,
		RecommendedUsage: []string{"result"}, CurrentVersionID: &versionID,
		FolderID: folderID,
		Status:   StatusPendingUpload, CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now,
	}
	version := Version{
		ID: versionID, ArtifactID: artifactID, ProjectID: projectID, VersionNo: 1,
		StorageClass: "object", Filename: filename, SHA256: expectedSHA,
		MIMEType: mimeType, SizeBytes: expectedSize, Status: StatusPendingUpload,
		CreatedBy: createdBy, CreatedAt: now,
	}
	upload, providerUpload, err := service.prepareUpload(
		ctx, projectID, artifactID, versionID, uploadID, createdBy,
		filename, mimeType, expectedSHA, expectedSize, idempotencyKey, plan,
	)
	if err != nil {
		return Detail{}, err
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
				if placeErr := service.ensureManagedArtifactPlacement(
					ctx, projectID, existing.ArtifactID, folderID,
				); placeErr != nil {
					return Detail{}, placeErr
				}
				return service.Store.GetDetail(ctx, projectID, existing.ArtifactID, false)
			}
		}
		return Detail{}, err
	}
	if upload.Status == UploadCompleted {
		return service.Store.GetDetail(ctx, projectID, artifactID, false)
	}
	provider := providerHandle(upload)
	parts := make([]CompletedPart, 0, plan.PartCount)
	var copied int64
	for partNumber := 1; partNumber <= plan.PartCount; partNumber++ {
		size, sizeErr := plan.PartSize(partNumber)
		if sizeErr != nil {
			return Detail{}, sizeErr
		}
		part, putErr := service.Storage.PutPart(
			ctx, provider, partNumber, io.LimitReader(temporaryFile, size), size,
		)
		if putErr != nil {
			return Detail{}, service.storageError(putErr)
		}
		copied += part.SizeBytes
		parts = append(parts, part)
	}
	if copied != expectedSize {
		return Detail{}, ErrSizeMismatch
	}
	if err := service.Store.UpsertParts(ctx, upload.ID, completedToUploadParts(parts, now)); err != nil {
		return Detail{}, err
	}
	if err := service.Store.MarkUploading(ctx, upload.ID, now); err != nil {
		return Detail{}, err
	}
	if _, err := service.Storage.CompleteMultipart(ctx, provider, parts); err != nil {
		return Detail{}, service.storageError(err)
	}
	if err := service.Store.SetUploadStatus(ctx, upload.ID, UploadVerifying, "", now); err != nil {
		return Detail{}, err
	}
	contentKey := ContentObjectKey(projectID, expectedSHA)
	if err := service.verifyObject(ctx, upload.StagingKey, expectedSize, expectedSHA); err != nil {
		return Detail{}, err
	}
	if err := service.promoteVerified(ctx, upload, contentKey); err != nil {
		return Detail{}, err
	}
	blobID, err := service.Generator.New()
	if err != nil {
		return Detail{}, err
	}
	return service.Store.FinalizeUpload(ctx, upload, Blob{
		ID: blobID, ProjectID: projectID, SHA256: expectedSHA, SizeBytes: expectedSize,
		Backend: service.Storage.Backend(), ObjectKey: contentKey,
	}, service.now())
}
