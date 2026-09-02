package artifact

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

// ArchiveExperimentResult streams one verified artifact.zip through the same
// Artifact storage and immutable-version boundary used by browser uploads.
// The caller owns the input stream; this method never buffers the complete
// result in memory or exposes provider handles.
func (service Service) ArchiveExperimentResult(ctx context.Context, projectID, experimentID, createdBy string, folderPath []string, expectedSHA string, expectedSize int64, input io.Reader) (Detail, error) {
	expectedSHA = strings.ToLower(strings.TrimSpace(expectedSHA))
	idempotencyKey := "experiment-result:" + experimentID
	if projectID == "" || experimentID == "" || createdBy == "" || input == nil || expectedSize < 1 || expectedSize > service.MaxUploadBytes || !sha256Pattern.MatchString(expectedSHA) {
		return Detail{}, ErrInvalid
	}
	folderID, err := service.ensureManagedFolder(ctx, projectID, folderPath)
	if err != nil {
		return Detail{}, err
	}
	initial := InitializeUploadInput{
		Filename: "execution-bundle.zip", SizeBytes: expectedSize,
		SHA256: expectedSHA, MIMEType: "application/zip",
		Kind: KindExperimentResult, FolderID: folderID,
		IdempotencyKey: idempotencyKey,
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
	if err := validateArtifactZip(temporary, experimentID); err != nil {
		return Detail{}, err
	}
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
	artifact := Artifact{ID: artifactID, ProjectID: projectID, Kind: KindExperimentResult, Source: SourceExperiment, SourceObjectID: &sourceID, Tags: []string{"experiment-result", "execution-bundle"}, Name: "execution-bundle.zip", RecommendedUsage: []string{"result", "evidence"}, CurrentVersionID: &versionID, FolderID: folderID, Status: StatusPendingUpload, CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now}
	version := Version{ID: versionID, ArtifactID: artifactID, ProjectID: projectID, VersionNo: 1, StorageClass: "object", Filename: "execution-bundle.zip", SHA256: expectedSHA, MIMEType: "application/zip", SizeBytes: expectedSize, Status: StatusPendingUpload, CreatedBy: createdBy, CreatedAt: now}
	upload, providerUpload, err := service.prepareUpload(ctx, projectID, artifactID, versionID, uploadID, createdBy, "execution-bundle.zip", "application/zip", expectedSHA, expectedSize, idempotencyKey, plan)
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
		size, err := plan.PartSize(partNumber)
		if err != nil {
			return Detail{}, err
		}
		reader := io.LimitReader(temporaryFile, size)
		part, err := service.Storage.PutPart(ctx, provider, partNumber, reader, size)
		if err != nil {
			return Detail{}, service.storageError(err)
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
	return service.Store.FinalizeUpload(ctx, upload, Blob{ID: blobID, ProjectID: projectID, SHA256: expectedSHA, SizeBytes: expectedSize, Backend: service.Storage.Backend(), ObjectKey: contentKey}, service.now())
}

func stageResultInput(input io.Reader, expectedSize int64, expectedSHA string) (string, error) {
	temporaryFile, err := os.CreateTemp("", "mmdash-experiment-result-*.zip")
	if err != nil {
		return "", err
	}
	name := temporaryFile.Name()
	removeOnError := func(cause error) (string, error) {
		_ = temporaryFile.Close()
		_ = os.Remove(name)
		return "", cause
	}
	digest := sha256.New()
	copied, err := io.CopyN(io.MultiWriter(temporaryFile, digest), input, expectedSize)
	if err != nil {
		return removeOnError(err)
	}
	if copied != expectedSize {
		return removeOnError(ErrSizeMismatch)
	}
	var extra [1]byte
	read, readErr := input.Read(extra[:])
	if read > 0 {
		return removeOnError(ErrSizeMismatch)
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return removeOnError(readErr)
	}
	if hex.EncodeToString(digest.Sum(nil)) != expectedSHA {
		return removeOnError(ErrHashMismatch)
	}
	if err := temporaryFile.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func validateArtifactZip(filename, experimentID string) error {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return ErrInvalid
	}
	defer archive.Close()
	if len(archive.File) == 0 || len(archive.File) > 10001 {
		return ErrInvalid
	}
	var uncompressed int64
	var manifestFile *zip.File
	files := map[string]*zip.File{}
	for _, file := range archive.File {
		name := file.Name
		if name == "manifest.json" {
			if manifestFile != nil {
				return ErrInvalid
			}
			manifestFile = file
			continue
		}
		if !safeZipPath(name) || file.FileInfo().Mode()&os.ModeSymlink != 0 || strings.HasSuffix(name, "/") {
			return ErrInvalid
		}
		if _, exists := files[name]; exists {
			return ErrInvalid
		}
		files[name] = file
		uncompressed += int64(file.UncompressedSize64)
		if uncompressed > 10<<30 {
			return ErrInvalid
		}
	}
	if manifestFile == nil || manifestFile.UncompressedSize64 > 1<<20 {
		return ErrInvalid
	}
	manifestReader, err := manifestFile.Open()
	if err != nil {
		return ErrInvalid
	}
	manifestBytes, readErr := io.ReadAll(io.LimitReader(manifestReader, 1<<20))
	_ = manifestReader.Close()
	if readErr != nil {
		return ErrInvalid
	}
	var manifest map[string]interface{}
	if json.Unmarshal(manifestBytes, &manifest) != nil || manifest["schema_version"] != "2" || manifest["experiment_id"] != experimentID || manifest["status"] != "succeeded" {
		return ErrInvalid
	}
	entries, ok := manifest["files"].([]interface{})
	if !ok || len(entries) != len(files) || len(entries) > 10000 {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, raw := range entries {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			return ErrInvalid
		}
		name, nameOK := entry["path"].(string)
		digest, digestOK := entry["sha256"].(string)
		size, sizeOK := jsonInteger(entry["size_bytes"])
		if !nameOK || !digestOK || !sha256Pattern.MatchString(digest) || !sizeOK || size < 0 || !safeZipPath(name) {
			return ErrInvalid
		}
		if _, exists := seen[name]; exists {
			return ErrInvalid
		}
		seen[name] = struct{}{}
		file, exists := files[name]
		if !exists || int64(file.UncompressedSize64) != size {
			return ErrInvalid
		}
		reader, err := file.Open()
		if err != nil {
			return ErrInvalid
		}
		hasher := sha256.New()
		copied, copyErr := io.Copy(hasher, io.LimitReader(reader, size+1))
		_ = reader.Close()
		if copyErr != nil || copied != size || hex.EncodeToString(hasher.Sum(nil)) != digest {
			return ErrInvalid
		}
	}
	return nil
}

func safeZipPath(name string) bool {
	return name != "" && !strings.ContainsRune(name, 0) && !strings.ContainsAny(name, "\\:") && !strings.HasPrefix(name, "/") && path.Clean(name) == name && name != "." && name != ".." && !strings.HasPrefix(name, "../")
}

func jsonInteger(value interface{}) (int64, bool) {
	switch number := value.(type) {
	case float64:
		if number < 0 || number != float64(int64(number)) {
			return 0, false
		}
		return int64(number), true
	case int64:
		return number, number >= 0
	case int:
		return int64(number), number >= 0
	default:
		return 0, false
	}
}
