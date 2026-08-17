package experiment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/repo"
)

type SelfResultRepository interface {
	SyncNow(context.Context, string) error
	VerifyCommitReachable(context.Context, string, string) error
	ListResultFiles(context.Context, string, string, string) ([]repo.ResultTreeFile, error)
	ReadResultFile(context.Context, string, string, string) (repo.FileContent, error)
	HashResultFile(context.Context, string, repo.ResultTreeFile) (string, error)
}

type SelfResultArtifactValidator interface {
	ValidateExperimentPointer(context.Context, string, string, string, string, int64, string) error
}

type SelfResultVerifier struct {
	Artifacts SelfResultArtifactValidator
	Repo      SelfResultRepository
}

type selfArtifactManifest struct {
	SchemaVersion int                   `json:"schema_version"`
	Files         []selfArtifactPointer `json:"files"`
}

type selfArtifactPointer struct {
	Path              string `json:"path"`
	ArtifactID        string `json:"artifact_id"`
	ArtifactVersionID string `json:"artifact_version_id"`
	SHA256            string `json:"sha256"`
	Size              int64  `json:"size"`
	MediaType         string `json:"media_type"`
}

func (verifier SelfResultVerifier) VerifySelfResult(
	ctx context.Context,
	_ auth.Identity,
	item Experiment,
	commitSHA string,
) (ResultVerification, error) {
	if verifier.Repo == nil || verifier.Artifacts == nil || item.Type != TypeSelf ||
		item.ExecutionStatus != StatusVerifyingResult || !commitPattern.MatchString(commitSHA) {
		return ResultVerification{}, ErrInvalid
	}
	if err := verifier.Repo.SyncNow(ctx, item.ProjectID); err != nil {
		return ResultVerification{}, fmt.Errorf("sync result branch: %w", err)
	}
	if err := verifier.Repo.VerifyCommitReachable(ctx, item.ProjectID, commitSHA); err != nil {
		return ResultVerification{}, fmt.Errorf("verify remote result Commit: %w", err)
	}
	tree, err := verifier.Repo.ListResultFiles(
		ctx, item.ProjectID, commitSHA, item.ResultDirectory,
	)
	if err != nil {
		return ResultVerification{}, fmt.Errorf("list result directory: %w", err)
	}
	byPath := make(map[string]repo.ResultTreeFile, len(tree))
	for _, file := range tree {
		if !strings.HasPrefix(file.Path, item.ResultDirectory) || byPath[file.Path].Path != "" {
			return ResultVerification{}, ErrInvalid
		}
		byPath[file.Path] = file
	}
	manifestPath := item.ResultDirectory + "manifest.json"
	pointerPath := item.ResultDirectory + PointerPath
	manifestBytes, err := verifier.readMetadata(ctx, item, commitSHA, byPath, manifestPath, maxManifestBytes)
	if err != nil {
		return ResultVerification{}, err
	}
	pointerBytes, err := verifier.readMetadata(ctx, item, commitSHA, byPath, pointerPath, maxManifestBytes)
	if err != nil {
		return ResultVerification{}, err
	}
	manifestDigest := sha256.Sum256(manifestBytes)
	manifestSHA := hex.EncodeToString(manifestDigest[:])
	manifest, manifestMap, err := decodeSelfManifest(manifestBytes, item)
	if err != nil {
		return ResultVerification{}, err
	}
	pointers, err := decodeSelfPointers(pointerBytes)
	if err != nil {
		return ResultVerification{}, err
	}
	resultFiles := make([]ResultFile, 0, len(manifest.Files))
	expectedTree := map[string]bool{manifestPath: true, pointerPath: true}
	for _, manifestFile := range manifest.Files {
		mediaType := normalizeMediaType(manifestFile.MediaType, manifestFile.Path)
		repositoryPath := item.ResultDirectory + manifestFile.Path
		result := ResultFile{
			Path: manifestFile.Path, SHA256: manifestFile.SHA256,
			SizeBytes: manifestFile.SizeBytes, MediaType: mediaType,
		}
		if manifestFile.SizeBytes < item.GitLargeFileThreshold {
			file, exists := byPath[repositoryPath]
			if !exists || file.Size != manifestFile.SizeBytes {
				return ResultVerification{}, fmt.Errorf("Git result file missing: %s", manifestFile.Path)
			}
			digest, hashErr := verifier.Repo.HashResultFile(ctx, item.ProjectID, file)
			if hashErr != nil || digest != manifestFile.SHA256 {
				return ResultVerification{}, fmt.Errorf("Git result file hash mismatch: %s", manifestFile.Path)
			}
			expectedTree[repositoryPath] = true
			result.StorageKind, result.RepositoryPath = "git", repositoryPath
		} else {
			pointer, exists := pointers[manifestFile.Path]
			if !exists || pointer.SHA256 != manifestFile.SHA256 ||
				pointer.Size != manifestFile.SizeBytes || pointer.MediaType != mediaType {
				return ResultVerification{}, fmt.Errorf("Artifact pointer mismatch: %s", manifestFile.Path)
			}
			if err := verifier.Artifacts.ValidateExperimentPointer(
				ctx, item.ProjectID, pointer.ArtifactID, pointer.ArtifactVersionID,
				pointer.SHA256, pointer.Size, pointer.MediaType,
			); err != nil {
				return ResultVerification{}, fmt.Errorf("Artifact pointer unavailable: %s: %w", manifestFile.Path, err)
			}
			delete(pointers, manifestFile.Path)
			result.StorageKind = "artifact"
			result.ArtifactID, result.ArtifactVersionID = pointer.ArtifactID, pointer.ArtifactVersionID
			result.RepositoryPath = pointerPath
		}
		resultFiles = append(resultFiles, result)
	}
	if len(pointers) != 0 || len(expectedTree) != len(byPath) {
		return ResultVerification{}, errors.New("result directory contains undeclared files")
	}
	for repositoryPath := range byPath {
		if !expectedTree[repositoryPath] {
			return ResultVerification{}, fmt.Errorf("undeclared result file: %s", repositoryPath)
		}
	}
	return ResultVerification{
		CommitSHA: commitSHA, ManifestSHA256: manifestSHA,
		Manifest: manifestMap, Files: resultFiles, Summary: manifest.Summary,
	}, nil
}

func (verifier SelfResultVerifier) readMetadata(
	ctx context.Context,
	item Experiment,
	commitSHA string,
	tree map[string]repo.ResultTreeFile,
	repositoryPath string,
	limit int64,
) ([]byte, error) {
	file, exists := tree[repositoryPath]
	if !exists || file.Size < 1 || file.Size > limit {
		return nil, fmt.Errorf("required result metadata missing: %s", repositoryPath)
	}
	content, err := verifier.Repo.ReadResultFile(
		ctx, item.ProjectID, commitSHA, repositoryPath,
	)
	if err != nil || content.Kind != "file" || content.Content == nil ||
		int64(len([]byte(*content.Content))) != file.Size {
		return nil, fmt.Errorf("read result metadata: %s", repositoryPath)
	}
	return []byte(*content.Content), nil
}

func decodeSelfManifest(
	contents []byte,
	item Experiment,
) (resultManifest, map[string]interface{}, error) {
	var manifest resultManifest
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || manifest.SchemaVersion != "2" ||
		manifest.ExperimentID != item.ID || manifest.SourceCommit != item.SourceCommit ||
		manifest.ResultDirectory != item.ResultDirectory || manifest.Status != "succeeded" ||
		(manifest.Runtime != "local-docker" && manifest.Runtime != "e2b") ||
		strings.TrimSpace(manifest.RuntimeVersion) == "" || manifest.StartedAt.IsZero() ||
		manifest.FinishedAt.IsZero() || manifest.FinishedAt.Before(manifest.StartedAt) ||
		manifest.ExitCode != nil && *manifest.ExitCode != 0 || len(manifest.Files) > maxBundleFiles {
		return resultManifest{}, nil, ErrInvalid
	}
	seen := map[string]bool{}
	for index := range manifest.Files {
		file := &manifest.Files[index]
		file.MediaType = normalizeMediaType(file.MediaType, file.Path)
		if !safeResultPath(file.Path) || seen[file.Path] || !sha256Pattern.MatchString(file.SHA256) ||
			file.SizeBytes < 0 || !validResultKind(file.Kind) {
			return resultManifest{}, nil, ErrInvalid
		}
		seen[file.Path] = true
	}
	var raw map[string]interface{}
	if json.Unmarshal(contents, &raw) != nil {
		return resultManifest{}, nil, ErrInvalid
	}
	return manifest, raw, nil
}

func decodeSelfPointers(contents []byte) (map[string]selfArtifactPointer, error) {
	var manifest selfArtifactManifest
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || manifest.SchemaVersion != 1 || len(manifest.Files) > maxBundleFiles {
		return nil, ErrInvalid
	}
	result := make(map[string]selfArtifactPointer, len(manifest.Files))
	for _, pointer := range manifest.Files {
		if !safeResultPath(pointer.Path) || result[pointer.Path].Path != "" ||
			pointer.ArtifactID == "" || pointer.ArtifactVersionID == "" ||
			!sha256Pattern.MatchString(pointer.SHA256) || pointer.Size < 0 ||
			normalizeMediaType(pointer.MediaType, pointer.Path) != pointer.MediaType {
			return nil, ErrInvalid
		}
		result[pointer.Path] = pointer
	}
	return result, nil
}
