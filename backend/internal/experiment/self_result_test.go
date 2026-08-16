package experiment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/repo"
)

type selfResultRepoStub struct {
	contents map[string]string
	files    []repo.ResultTreeFile
	hashes   map[string]string
	synced   bool
}

func (stub *selfResultRepoStub) SyncNow(context.Context, string) error {
	stub.synced = true
	return nil
}
func (*selfResultRepoStub) VerifyCommitReachable(context.Context, string, string) error {
	return nil
}
func (stub *selfResultRepoStub) ListResultFiles(
	context.Context, string, string, string,
) ([]repo.ResultTreeFile, error) {
	return stub.files, nil
}
func (stub *selfResultRepoStub) ReadResultFile(
	_ context.Context, _, _, repositoryPath string,
) (repo.FileContent, error) {
	value := stub.contents[repositoryPath]
	return repo.FileContent{Content: &value, Kind: "file", Path: repositoryPath}, nil
}
func (stub *selfResultRepoStub) HashResultFile(
	_ context.Context, _ string, file repo.ResultTreeFile,
) (string, error) {
	return stub.hashes[file.Path], nil
}

type selfArtifactStub struct{ validated bool }

func (stub *selfArtifactStub) ValidateExperimentPointer(
	context.Context, string, string, string, string, int64, string,
) error {
	stub.validated = true
	return nil
}

func TestSelfResultVerifierFetchesAndValidatesGitAndArtifactFiles(t *testing.T) {
	item := Experiment{
		ID: "00000000-0000-4000-8000-000000000001", ProjectID: "project-1",
		Type: TypeSelf, ExecutionStatus: StatusVerifyingResult,
		SourceCommit:          strings.Repeat("a", 40),
		ResultDirectory:       "experiments/00000000-0000-4000-8000-000000000001_20260816_1200/",
		GitLargeFileThreshold: 10,
	}
	small := []byte("small")
	smallDigest := sha256.Sum256(small)
	largeDigest := sha256.Sum256([]byte("large contents"))
	manifest := resultManifest{
		SchemaVersion: "2", ExperimentID: item.ID, SourceCommit: item.SourceCommit,
		ResultDirectory: item.ResultDirectory, Status: "succeeded",
		StartedAt:  time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 8, 16, 4, 0, 1, 0, time.UTC),
		Runtime:    "local-docker", RuntimeVersion: "docker-1", ExitCode: intPointer(0),
		Files: []PreparedResultFile{
			{Path: "small.txt", SHA256: hex.EncodeToString(smallDigest[:]), SizeBytes: 5, Kind: "file", MediaType: "text/plain"},
			{Path: "large.bin", SHA256: hex.EncodeToString(largeDigest[:]), SizeBytes: 14, Kind: "data", MediaType: "application/octet-stream"},
		},
	}
	manifestBytes, _ := json.Marshal(manifest)
	pointerBytes, _ := json.Marshal(selfArtifactManifest{
		SchemaVersion: 1,
		Files: []selfArtifactPointer{{
			Path: "large.bin", ArtifactID: "artifact-1", ArtifactVersionID: "version-1",
			SHA256: hex.EncodeToString(largeDigest[:]), Size: 14,
			MediaType: "application/octet-stream",
		}},
	})
	manifestPath := item.ResultDirectory + "manifest.json"
	pointerPath := item.ResultDirectory + PointerPath
	smallPath := item.ResultDirectory + "small.txt"
	repository := &selfResultRepoStub{
		contents: map[string]string{
			manifestPath: string(manifestBytes), pointerPath: string(pointerBytes),
		},
		files: []repo.ResultTreeFile{
			{ObjectID: strings.Repeat("b", 40), Path: manifestPath, Size: int64(len(manifestBytes))},
			{ObjectID: strings.Repeat("c", 40), Path: pointerPath, Size: int64(len(pointerBytes))},
			{ObjectID: strings.Repeat("d", 40), Path: smallPath, Size: int64(len(small))},
		},
		hashes: map[string]string{smallPath: hex.EncodeToString(smallDigest[:])},
	}
	artifacts := &selfArtifactStub{}

	result, err := (SelfResultVerifier{Artifacts: artifacts, Repo: repository}).VerifySelfResult(
		context.Background(), auth.Identity{}, item, strings.Repeat("e", 40),
	)
	if err != nil {
		t.Fatalf("valid self result rejected: %v", err)
	}
	if !repository.synced || !artifacts.validated || len(result.Files) != 2 ||
		result.Files[0].StorageKind != "git" || result.Files[1].StorageKind != "artifact" {
		t.Fatalf("self result was not fully verified: %#v", result)
	}
}
