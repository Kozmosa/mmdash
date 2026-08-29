package experiment

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/repo"
)

const (
	maxBundleFiles   = 10000
	maxBundleBytes   = int64(10 << 30)
	maxManifestBytes = int64(1 << 20)
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ResultJobInput grants a Worker temporary access to one immutable raw Bundle.
// Provider credentials and repository credentials never cross this boundary.
type ResultJobInput struct {
	ArtifactID                 string                 `json:"artifact_id"`
	BundleSHA256               string                 `json:"bundle_sha256"`
	BundleSizeBytes            int64                  `json:"bundle_size_bytes"`
	ExperimentID               string                 `json:"experiment_id"`
	GitLargeFileThresholdBytes int64                  `json:"git_large_file_threshold_bytes"`
	ManifestSHA256             string                 `json:"manifest_sha256"`
	ProjectID                  string                 `json:"project_id"`
	ResultDirectory            string                 `json:"result_directory"`
	Transfer                   map[string]interface{} `json:"transfer"`
	VersionID                  string                 `json:"version_id"`
}

// PreparedResultFile is the Worker's typed, bounded observation of one file.
// Core repeats every security-sensitive validation against immutable bytes.
type PreparedResultFile struct {
	Kind      string `json:"kind"`
	MediaType string `json:"mime_type"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type ResultPreparation struct {
	Analysis       string               `json:"analysis,omitempty"`
	Files          []PreparedResultFile `json:"files"`
	ManifestSHA256 string               `json:"manifest_sha256"`
	Summary        string               `json:"summary,omitempty"`
}

type ResultFinalizeResponse struct {
	ExperimentID    string `json:"experiment_id"`
	ResultCommitSHA string `json:"result_commit_sha"`
}

type ResultProcessingError struct {
	Cause     error
	Code      string
	Message   string
	Retryable bool
	Stage     string
}

func (failure *ResultProcessingError) Error() string { return failure.Message }
func (failure *ResultProcessingError) Unwrap() error { return failure.Cause }

func processingError(stage, code, message string, retryable bool, cause error) error {
	return &ResultProcessingError{
		Cause: cause, Code: code, Message: message, Retryable: retryable, Stage: stage,
	}
}

func repoProcessingError(cause error) error {
	var safeError *repo.SafeError
	if errors.As(cause, &safeError) && safeError.Code == "REPO_PUSH_FAILED" {
		return processingError("repo_push", "REPO_PUSH_FAILED", "Repository push failed", true, cause)
	}
	return processingError("repo_commit", "REPO_COMMIT_FAILED", "Repository result commit failed", true, cause)
}

type resultManifest struct {
	SchemaVersion   string                     `json:"schema_version"`
	ExperimentID    string                     `json:"experiment_id"`
	SourceCommit    string                     `json:"source_commit"`
	ResultDirectory string                     `json:"result_directory"`
	Status          string                     `json:"status"`
	StartedAt       time.Time                  `json:"started_at"`
	FinishedAt      time.Time                  `json:"finished_at"`
	Runtime         string                     `json:"runtime"`
	RuntimeVersion  string                     `json:"runtime_version"`
	LogsTruncated   bool                       `json:"logs_truncated"`
	Summary         string                     `json:"summary,omitempty"`
	ExitCode        *int                       `json:"exit_code,omitempty"`
	Environment     *resultManifestEnvironment `json:"environment,omitempty"`
	Files           []PreparedResultFile       `json:"files"`
}

type resultManifestEnvironment struct {
	Provider             string            `json:"provider,omitempty"`
	EnvironmentKey       string            `json:"environment_key"`
	BaseImageID          string            `json:"base_image_id"`
	EnvironmentImageID   string            `json:"environment_image_id"`
	ManifestPaths        []string          `json:"manifest_paths"`
	ManifestHashes       map[string]string `json:"manifest_hashes"`
	ResolvedDependencies []string          `json:"resolved_dependencies,omitempty"`
	BuilderVersion       string            `json:"builder_version"`
	CacheHit             bool              `json:"cache_hit"`
}

type verifiedBundle struct {
	Archive       *zip.ReadCloser
	Files         map[string]*zip.File
	Manifest      resultManifest
	ManifestBytes []byte
	ManifestMap   map[string]interface{}
}

func (bundle *verifiedBundle) Close() error { return bundle.Archive.Close() }

func (service *Service) WorkerResultInput(
	ctx context.Context,
	caller auth.Identity,
	jobID string,
) (ResultJobInput, error) {
	job, item, artifactID, versionID, manifestSHA, err := service.resultJob(ctx, caller, jobID)
	if err != nil {
		return ResultJobInput{}, err
	}
	if service.ResultArtifacts == nil {
		return ResultJobInput{}, ErrInvalid
	}
	grant, err := service.ResultArtifacts.ExperimentResultGrant(
		ctx, job.ProjectID, item.ID, artifactID, versionID,
	)
	if err != nil {
		return ResultJobInput{}, err
	}
	return ResultJobInput{
		ArtifactID: artifactID, BundleSHA256: mapString(grant, "sha256"),
		BundleSizeBytes: mapInt64(grant, "size_bytes"), ExperimentID: item.ID,
		GitLargeFileThresholdBytes: item.GitLargeFileThreshold,
		ManifestSHA256:             manifestSHA, ProjectID: job.ProjectID,
		ResultDirectory: item.ResultDirectory,
		Transfer: map[string]interface{}{
			"method": grant["method"], "url": grant["url"],
			"headers": grant["headers"], "expires_at": grant["expires_at"],
		},
		VersionID: versionID,
	}, nil
}

func (service *Service) FinalizeWorkerResult(
	ctx context.Context,
	caller auth.Identity,
	jobID string,
	prepared ResultPreparation,
) (ResultFinalizeResponse, error) {
	job, item, artifactID, versionID, expectedManifestSHA, err := service.resultJob(ctx, caller, jobID)
	if err != nil {
		return ResultFinalizeResponse{}, err
	}
	if item.ExecutionStatus == StatusSucceeded {
		return ResultFinalizeResponse{ExperimentID: item.ID, ResultCommitSHA: item.ResultCommitSHA}, nil
	}
	if service.ResultArtifacts == nil || service.ResultRepo == nil || service.Store == nil ||
		prepared.ManifestSHA256 != expectedManifestSHA || len(prepared.Files) > maxBundleFiles {
		return ResultFinalizeResponse{}, ErrInvalid
	}
	reader, version, err := service.ResultArtifacts.OpenExperimentResult(
		ctx, job.ProjectID, item.ID, artifactID, versionID,
	)
	if err != nil {
		return ResultFinalizeResponse{}, processingError(
			"artifact_storage", "ARTIFACT_ARCHIVE_FAILED",
			"Execution Bundle is unavailable", true, err,
		)
	}
	bundlePath, err := stageImmutableBundle(reader, version.SizeBytes, version.SHA256)
	_ = reader.Close()
	if err != nil {
		return ResultFinalizeResponse{}, processingError(
			"bundle_validation", "RESULT_INVALID", "Execution Bundle is invalid", false, err,
		)
	}
	defer os.Remove(bundlePath)
	bundle, err := verifyResultBundle(bundlePath, item, expectedManifestSHA)
	if err != nil {
		return ResultFinalizeResponse{}, processingError(
			"bundle_validation", "RESULT_INVALID", "Execution Bundle is invalid", false, err,
		)
	}
	defer bundle.Close()
	if !samePreparedFiles(prepared.Files, bundle.Manifest.Files) {
		return ResultFinalizeResponse{}, processingError(
			"bundle_validation", "RESULT_INVALID", "Worker result metadata does not match Bundle", false, ErrInvalid,
		)
	}
	stagingRoot, err := os.MkdirTemp("", "mmdash-result-finalize-*")
	if err != nil {
		return ResultFinalizeResponse{}, processingError(
			"result_processing", "RESULT_PROCESSING_FAILED", "Result staging failed", true, err,
		)
	}
	defer os.RemoveAll(stagingRoot)

	resultFiles, commitFiles, commitPaths, pointers, err := service.prepareResultFiles(
		ctx, item, bundle, stagingRoot,
	)
	if err != nil {
		return ResultFinalizeResponse{}, processingError(
			"artifact_storage", "ARTIFACT_ARCHIVE_FAILED", "Result file archival failed", true, err,
		)
	}
	manifestPath, err := writeStagedFile(stagingRoot, "manifest.json", bundle.ManifestBytes)
	if err != nil {
		return ResultFinalizeResponse{}, processingError(
			"result_processing", "RESULT_PROCESSING_FAILED", "Result Manifest staging failed", true, err,
		)
	}
	manifestDigest := sha256.Sum256(bundle.ManifestBytes)
	commitFiles = append(commitFiles, repo.ResultFileChange{
		Path: item.ResultDirectory + "manifest.json", SHA256: hex.EncodeToString(manifestDigest[:]),
		SizeBytes: int64(len(bundle.ManifestBytes)), SourcePath: manifestPath,
	})
	commitPaths = append(commitPaths, item.ResultDirectory+"manifest.json")
	pointerBytes, err := json.MarshalIndent(map[string]interface{}{
		"schema_version": 1, "files": pointers,
	}, "", "  ")
	if err != nil {
		return ResultFinalizeResponse{}, processingError(
			"result_processing", "RESULT_PROCESSING_FAILED", "Artifact pointer encoding failed", true, err,
		)
	}
	pointerBytes = append(pointerBytes, '\n')
	pointerPath, err := writeStagedFile(stagingRoot, filepath.FromSlash(PointerPath), pointerBytes)
	if err != nil {
		return ResultFinalizeResponse{}, processingError(
			"result_processing", "RESULT_PROCESSING_FAILED", "Artifact pointer staging failed", true, err,
		)
	}
	pointerDigest := sha256.Sum256(pointerBytes)
	commitFiles = append(commitFiles, repo.ResultFileChange{
		Path: item.ResultDirectory + PointerPath, SHA256: hex.EncodeToString(pointerDigest[:]),
		SizeBytes: int64(len(pointerBytes)), SourcePath: pointerPath,
	})
	commitPaths = append(commitPaths, item.ResultDirectory+PointerPath)

	head, err := service.ResultRepo.ResolveHead(ctx, item.ProjectID)
	if err != nil {
		return ResultFinalizeResponse{}, repoProcessingError(err)
	}
	committed, err := service.ResultRepo.Commit(ctx, repo.ResultCommitRequest{
		ActorID: item.CreatedBy, ExpectedHeadSHA: head.CommitSHA, ExperimentID: item.ID,
		Files: commitFiles, ProjectID: item.ProjectID, ResultDirectory: item.ResultDirectory,
		SourceRoot: stagingRoot,
	})
	if err != nil {
		return ResultFinalizeResponse{}, repoProcessingError(err)
	}
	if recorder, ok := service.Store.(managedStagingRecorder); ok {
		if err := recorder.RecordManagedStaging(ctx, item.ProjectID, item.ID, committed.CommitSHA, commitPaths, service.now()); err != nil {
			return ResultFinalizeResponse{}, service.compensateResultCommit(
				ctx, item, committed.CommitSHA, commitPaths,
				processingError("result_binding", "RESULT_BINDING_FAILED", "Result binding was rejected", false, err),
			)
		}
	}
	summary := strings.TrimSpace(prepared.Summary)
	if summary == "" {
		summary = bundle.Manifest.Summary
	}
	completed, completeErr := service.Store.CompleteResult(ctx, item.ProjectID, item.ID, ResultVerification{
		CommitSHA: committed.CommitSHA, ManifestSHA256: expectedManifestSHA,
		Manifest: bundle.ManifestMap, Files: resultFiles, Summary: summary,
		Analysis: strings.TrimSpace(prepared.Analysis),
	}, service.now())
	if completeErr != nil {
		return ResultFinalizeResponse{}, service.compensateResultCommit(
			ctx, item, committed.CommitSHA, commitPaths,
			processingError("result_binding", "RESULT_BINDING_FAILED", "Result binding failed", false, completeErr),
		)
	}
	return ResultFinalizeResponse{ExperimentID: completed.ID, ResultCommitSHA: completed.ResultCommitSHA}, nil
}

func (service *Service) compensateResultCommit(
	ctx context.Context,
	item Experiment,
	stagingSHA string,
	paths []string,
	cause error,
) error {
	if err := service.revertResultCommit(ctx, item, stagingSHA, paths); err != nil {
		return fmt.Errorf("bind result: %w; compensating revert: %v", cause, err)
	}
	return cause
}

func (service *Service) revertResultCommit(
	ctx context.Context,
	item Experiment,
	stagingSHA string,
	paths []string,
) error {
	reverted, revertErr := service.ResultRepo.Revert(ctx, repo.ResultRevertRequest{
		ActorID: item.CreatedBy, ExperimentID: item.ID, Paths: paths,
		ProjectID: item.ProjectID, ResultDirectory: item.ResultDirectory,
	})
	if revertErr != nil {
		return revertErr
	}
	if recorder, ok := service.Store.(managedStagingRecorder); ok {
		if recordErr := recorder.RecordCompensation(
			ctx, item.ProjectID, item.ID, stagingSHA, reverted.CommitSHA, service.now(),
		); recordErr != nil {
			return fmt.Errorf("record compensating revert: %w", recordErr)
		}
	}
	return nil
}

func (service *Service) resultJob(
	ctx context.Context,
	caller auth.Identity,
	jobID string,
) (jobs.Job, Experiment, string, string, string, error) {
	if service == nil || service.JobAccess == nil || service.Store == nil {
		return jobs.Job{}, Experiment{}, "", "", "", ErrInvalid
	}
	job, err := service.JobAccess.ClaimedWorkerJob(ctx, caller, jobID)
	if err != nil {
		return jobs.Job{}, Experiment{}, "", "", "", err
	}
	if job.JobType != JobTypeResultProcess {
		return jobs.Job{}, Experiment{}, "", "", "", ErrNotFound
	}
	experimentID := mapString(job.Payload, "experiment_id")
	artifactID := mapString(job.Payload, "artifact_id")
	versionID := mapString(job.Payload, "version_id")
	manifestSHA := mapString(job.Payload, "manifest_sha256")
	if experimentID == "" || artifactID == "" || versionID == "" ||
		!sha256Pattern.MatchString(manifestSHA) {
		return jobs.Job{}, Experiment{}, "", "", "", ErrInvalid
	}
	item, err := service.Store.Get(ctx, job.ProjectID, experimentID)
	if err != nil {
		return jobs.Job{}, Experiment{}, "", "", "", err
	}
	if item.ExecutionBundle == nil || item.ExecutionBundle.ArtifactID != artifactID ||
		item.ExecutionBundle.VersionID != versionID || item.ResultManifestSHA256 != manifestSHA ||
		(item.ExecutionStatus != StatusProcessingResult && item.ExecutionStatus != StatusSucceeded) {
		return jobs.Job{}, Experiment{}, "", "", "", ErrConflict
	}
	return job, item, artifactID, versionID, manifestSHA, nil
}

func stageImmutableBundle(input io.Reader, expectedSize int64, expectedSHA string) (string, error) {
	if expectedSize < 1 || expectedSize > maxBundleBytes || !sha256Pattern.MatchString(expectedSHA) {
		return "", ErrInvalid
	}
	file, err := os.CreateTemp("", "mmdash-immutable-bundle-*.zip")
	if err != nil {
		return "", err
	}
	name := file.Name()
	remove := func(cause error) (string, error) {
		_ = file.Close()
		_ = os.Remove(name)
		return "", cause
	}
	hasher := sha256.New()
	copied, err := io.CopyN(io.MultiWriter(file, hasher), input, expectedSize)
	if err != nil || copied != expectedSize {
		if err == nil {
			err = ErrInvalid
		}
		return remove(err)
	}
	var extra [1]byte
	count, readErr := input.Read(extra[:])
	if count != 0 || readErr != nil && !errors.Is(readErr, io.EOF) ||
		hex.EncodeToString(hasher.Sum(nil)) != expectedSHA {
		return remove(ErrInvalid)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func verifyResultBundle(filename string, item Experiment, expectedManifestSHA string) (*verifiedBundle, error) {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return nil, ErrInvalid
	}
	fail := func() (*verifiedBundle, error) {
		_ = archive.Close()
		return nil, ErrInvalid
	}
	if len(archive.File) < 1 || len(archive.File) > maxBundleFiles+1 {
		return fail()
	}
	files := map[string]*zip.File{}
	var manifestFile *zip.File
	var expanded int64
	for _, file := range archive.File {
		if file.Name == "manifest.json" {
			if manifestFile != nil || file.FileInfo().Mode()&os.ModeSymlink != 0 || file.UncompressedSize64 > uint64(maxManifestBytes) {
				return fail()
			}
			manifestFile = file
			continue
		}
		if !safeResultPath(file.Name) || strings.HasSuffix(file.Name, "/") ||
			file.FileInfo().Mode()&os.ModeSymlink != 0 || files[file.Name] != nil ||
			file.UncompressedSize64 > uint64(maxBundleBytes-expanded) {
			return fail()
		}
		expanded += int64(file.UncompressedSize64)
		files[file.Name] = file
	}
	if manifestFile == nil {
		return fail()
	}
	reader, err := manifestFile.Open()
	if err != nil {
		return fail()
	}
	manifestBytes, err := io.ReadAll(io.LimitReader(reader, maxManifestBytes+1))
	_ = reader.Close()
	if err != nil || int64(len(manifestBytes)) > maxManifestBytes {
		return fail()
	}
	digest := sha256.Sum256(manifestBytes)
	if hex.EncodeToString(digest[:]) != expectedManifestSHA {
		return fail()
	}
	var manifest resultManifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestBytes)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || manifest.SchemaVersion != "2" ||
		manifest.ExperimentID != item.ID || manifest.SourceCommit != item.SourceCommit ||
		manifest.ResultDirectory != item.ResultDirectory || manifest.Status != "succeeded" ||
		manifest.Runtime != item.ActualRuntime || manifest.RuntimeVersion != item.RuntimeVersion ||
		manifest.LogsTruncated != item.LogsTruncated || manifest.StartedAt.IsZero() ||
		manifest.FinishedAt.IsZero() || manifest.FinishedAt.Before(manifest.StartedAt) ||
		manifest.ExitCode == nil || *manifest.ExitCode != 0 || len(manifest.Files) != len(files) ||
		len(manifest.Files) > maxBundleFiles || !validResultEnvironment(manifest.Runtime, manifest.Environment) {
		return fail()
	}
	seen := map[string]bool{}
	for index := range manifest.Files {
		entry := &manifest.Files[index]
		entry.MediaType = normalizeMediaType(entry.MediaType, entry.Path)
		file := files[entry.Path]
		if !safeResultPath(entry.Path) || file == nil || seen[entry.Path] ||
			!sha256Pattern.MatchString(entry.SHA256) || entry.SizeBytes < 0 ||
			uint64(entry.SizeBytes) != file.UncompressedSize64 || !validResultKind(entry.Kind) {
			return fail()
		}
		seen[entry.Path] = true
		fileReader, openErr := file.Open()
		if openErr != nil {
			return fail()
		}
		hasher := sha256.New()
		copied, copyErr := io.Copy(hasher, io.LimitReader(fileReader, entry.SizeBytes+1))
		_ = fileReader.Close()
		if copyErr != nil || copied != entry.SizeBytes || hex.EncodeToString(hasher.Sum(nil)) != entry.SHA256 {
			return fail()
		}
	}
	var manifestMap map[string]interface{}
	if json.Unmarshal(manifestBytes, &manifestMap) != nil {
		return fail()
	}
	return &verifiedBundle{
		Archive: archive, Files: files, Manifest: manifest,
		ManifestBytes: manifestBytes, ManifestMap: manifestMap,
	}, nil
}

func validResultEnvironment(runtimeName string, environment *resultManifestEnvironment) bool {
	if environment == nil {
		// Both environment-preparing container and bare-metal Runtimes must
		// ship immutable environment evidence in the result Manifest.
		return runtimeName != "local-docker" && runtimeName != "local-process"
	}
	if environment.EnvironmentKey == "" || environment.BaseImageID == "" ||
		environment.EnvironmentImageID == "" || environment.BuilderVersion == "" ||
		len(environment.ManifestPaths) > 32 || len(environment.ManifestPaths) != len(environment.ManifestHashes) {
		return false
	}
	if environment.Provider != "" &&
		environment.Provider != "local-docker" && environment.Provider != "local-process" {
		return false
	}
	if len(environment.ResolvedDependencies) > 2000 {
		return false
	}
	for _, dependency := range environment.ResolvedDependencies {
		if dependency == "" || strings.TrimSpace(dependency) == "" || len(dependency) > 500 {
			return false
		}
	}
	seen := map[string]struct{}{}
	for _, manifestPath := range environment.ManifestPaths {
		digest, exists := environment.ManifestHashes[manifestPath]
		if !exists || !safeResultPath(manifestPath) || !sha256Pattern.MatchString(digest) {
			return false
		}
		if _, exists := seen[manifestPath]; exists {
			return false
		}
		seen[manifestPath] = struct{}{}
	}
	return true
}

func (service *Service) prepareResultFiles(
	ctx context.Context,
	item Experiment,
	bundle *verifiedBundle,
	stagingRoot string,
) ([]ResultFile, []repo.ResultFileChange, []string, []map[string]interface{}, error) {
	resultFiles := make([]ResultFile, 0, len(bundle.Manifest.Files))
	commitFiles := make([]repo.ResultFileChange, 0, len(bundle.Manifest.Files)+2)
	commitPaths := make([]string, 0, len(bundle.Manifest.Files)+2)
	pointers := make([]map[string]interface{}, 0)
	for _, manifestFile := range bundle.Manifest.Files {
		archiveFile := bundle.Files[manifestFile.Path]
		reader, err := archiveFile.Open()
		if err != nil {
			return nil, nil, nil, nil, err
		}
		repositoryPath := item.ResultDirectory + manifestFile.Path
		result := ResultFile{
			Path: manifestFile.Path, SHA256: manifestFile.SHA256,
			SizeBytes: manifestFile.SizeBytes, MediaType: manifestFile.MediaType,
		}
		if manifestFile.SizeBytes < item.GitLargeFileThreshold {
			bytes, readErr := io.ReadAll(io.LimitReader(reader, manifestFile.SizeBytes+1))
			_ = reader.Close()
			if readErr != nil || int64(len(bytes)) != manifestFile.SizeBytes {
				return nil, nil, nil, nil, ErrInvalid
			}
			staged, writeErr := writeStagedFile(stagingRoot, filepath.FromSlash(manifestFile.Path), bytes)
			if writeErr != nil {
				return nil, nil, nil, nil, writeErr
			}
			result.StorageKind, result.RepositoryPath = "git", repositoryPath
			commitFiles = append(commitFiles, repo.ResultFileChange{
				Path: repositoryPath, SHA256: manifestFile.SHA256,
				SizeBytes: manifestFile.SizeBytes, SourcePath: staged,
			})
			commitPaths = append(commitPaths, repositoryPath)
		} else {
			detail, archiveErr := service.ResultArtifacts.ArchiveExperimentFile(
				ctx, item.ProjectID, item.ID, item.CreatedBy, manifestFile.Path,
				manifestFile.MediaType, manifestFile.SHA256, manifestFile.SizeBytes, reader,
			)
			_ = reader.Close()
			if archiveErr != nil || detail.CurrentVersion == nil {
				if archiveErr == nil {
					archiveErr = ErrInvalid
				}
				return nil, nil, nil, nil, archiveErr
			}
			result.StorageKind = "artifact"
			result.ArtifactID = detail.Artifact.ID
			result.ArtifactVersionID = detail.CurrentVersion.ID
			result.RepositoryPath = item.ResultDirectory + PointerPath
			pointers = append(pointers, map[string]interface{}{
				"path": manifestFile.Path, "artifact_id": detail.Artifact.ID,
				"artifact_version_id": detail.CurrentVersion.ID,
				"sha256":              manifestFile.SHA256, "size": manifestFile.SizeBytes,
				"media_type": manifestFile.MediaType,
			})
		}
		resultFiles = append(resultFiles, result)
	}
	sort.Slice(pointers, func(i, j int) bool {
		return pointers[i]["path"].(string) < pointers[j]["path"].(string)
	})
	return resultFiles, commitFiles, commitPaths, pointers, nil
}

func writeStagedFile(root, relative string, contents []byte) (string, error) {
	if !safeResultPath(filepath.ToSlash(relative)) {
		return "", ErrInvalid
	}
	filename := filepath.Join(root, relative)
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(filename)
	if err != nil || !strings.HasPrefix(resolved, resolvedRoot+string(filepath.Separator)) {
		return "", ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(resolved, contents, 0o600); err != nil {
		return "", err
	}
	return resolved, nil
}

func samePreparedFiles(left, right []PreparedResultFile) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]PreparedResultFile(nil), left...)
	rightCopy := append([]PreparedResultFile(nil), right...)
	sort.Slice(leftCopy, func(i, j int) bool { return leftCopy[i].Path < leftCopy[j].Path })
	sort.Slice(rightCopy, func(i, j int) bool { return rightCopy[i].Path < rightCopy[j].Path })
	for index := range leftCopy {
		leftCopy[index].MediaType = normalizeMediaType(leftCopy[index].MediaType, leftCopy[index].Path)
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

func safeResultPath(value string) bool {
	return value != "" && len(value) <= 4096 && !strings.ContainsRune(value, 0) &&
		!strings.ContainsAny(value, "\\:") && !strings.HasPrefix(value, "/") &&
		path.Clean(value) == value && value != "." && value != ".." &&
		!strings.HasPrefix(value, "../")
}

func validResultKind(value string) bool {
	switch value {
	case "file", "log", "figure", "table", "data", "model", "summary":
		return true
	default:
		return false
	}
}

func normalizeMediaType(value, _ string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 {
		return "application/octet-stream"
	}
	return value
}

func mapString(value map[string]interface{}, key string) string {
	result, _ := value[key].(string)
	return strings.TrimSpace(result)
}

func mapInt64(value map[string]interface{}, key string) int64 {
	switch item := value[key].(type) {
	case int64:
		return item
	case int:
		return int64(item)
	case float64:
		return int64(item)
	default:
		return 0
	}
}

type managedStagingRecorder interface {
	RecordManagedStaging(context.Context, string, string, string, []string, time.Time) error
	RecordCompensation(context.Context, string, string, string, string, time.Time) error
}

type transactionalResultFailure interface {
	FailResultInTransaction(context.Context, transaction.Tx, string, Failure, time.Time) error
}

func (service *Service) PrepareComplete(ctx context.Context, job jobs.Job, _ map[string]interface{}) error {
	if job.JobType != JobTypeResultProcess {
		return nil
	}
	experimentID := mapString(job.Payload, "experiment_id")
	item, err := service.Store.Get(ctx, job.ProjectID, experimentID)
	if err != nil {
		return err
	}
	if item.ExecutionStatus != StatusSucceeded {
		return ErrConflict
	}
	return nil
}

func (service *Service) ClaimInTransaction(context.Context, transaction.Tx, jobs.Job) error {
	return nil
}

func (service *Service) CompleteInTransaction(context.Context, transaction.Tx, jobs.Job, map[string]interface{}) error {
	return nil
}

func (service *Service) FailInTransaction(
	ctx context.Context,
	tx transaction.Tx,
	job jobs.Job,
	failure jobs.Failure,
) error {
	if job.JobType != JobTypeResultProcess ||
		(job.Status != jobs.StatusFailed && job.Status != jobs.StatusCancelled && job.Status != jobs.StatusTimedOut) {
		return nil
	}
	store, ok := service.Store.(transactionalResultFailure)
	if !ok {
		return ErrInvalid
	}
	code := strings.TrimSpace(failure.Code)
	if code == "" {
		code = "RESULT_PROCESSING_FAILED"
	}
	stage := resultFailureStage(code)
	return store.FailResultInTransaction(ctx, tx, mapString(job.Payload, "experiment_id"), Failure{
		Stage: stage, Code: code, Message: strings.TrimSpace(failure.Message),
		FailedAt: service.now(), Retryable: false, Attempt: job.Attempts,
		CleanupResult: map[string]interface{}{"job_id": job.ID, "job_status": job.Status},
	}, service.now())
}

func resultFailureStage(code string) string {
	switch code {
	case "RESULT_INVALID":
		return "bundle_validation"
	case "ARTIFACT_ARCHIVE_FAILED":
		return "artifact_storage"
	case "REPO_COMMIT_FAILED":
		return "repo_commit"
	case "REPO_PUSH_FAILED":
		return "repo_push"
	case "RESULT_BINDING_FAILED":
		return "result_binding"
	default:
		return "result_processing"
	}
}
