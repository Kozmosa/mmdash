package experiment

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestVerifyResultBundleBindsFrozenIdentityAndFileHashes(t *testing.T) {
	item := Experiment{
		ID:              "00000000-0000-4000-8000-000000000001",
		SourceCommit:    strings.Repeat("a", 40),
		ResultDirectory: "experiments/00000000-0000-4000-8000-000000000001_20260816_1200/",
		ActualRuntime:   "local-docker", RuntimeVersion: "docker-1", LogsTruncated: false,
	}
	contents := []byte("result")
	digest := sha256.Sum256(contents)
	manifest := resultManifest{
		SchemaVersion: "2", ExperimentID: item.ID, SourceCommit: item.SourceCommit,
		ResultDirectory: item.ResultDirectory, Status: "succeeded",
		StartedAt:  time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 8, 16, 4, 0, 1, 0, time.UTC),
		Runtime:    item.ActualRuntime, RuntimeVersion: item.RuntimeVersion,
		LogsTruncated: false, ExitCode: intPointer(0),
		Environment: &resultManifestEnvironment{
			EnvironmentKey: strings.Repeat("b", 64), BaseImageID: "sha256:base",
			EnvironmentImageID: "sha256:environment", ManifestPaths: []string{"requirements.lock"},
			ManifestHashes: map[string]string{"requirements.lock": strings.Repeat("c", 64)},
			BuilderVersion: "1", CacheHit: false,
		},
		Files: []PreparedResultFile{{
			Path: "summary.txt", SHA256: hex.EncodeToString(digest[:]),
			SizeBytes: int64(len(contents)), Kind: "summary", MediaType: "text/plain",
		}},
	}
	filename, manifestSHA := writeResultBundle(t, manifest, map[string][]byte{
		"summary.txt": contents,
	})
	bundle, err := verifyResultBundle(filename, item, manifestSHA)
	if err != nil {
		t.Fatalf("valid Bundle rejected: %v", err)
	}
	defer bundle.Close()
	if bundle.Manifest.Files[0].MediaType != "text/plain" ||
		!samePreparedFiles(manifest.Files, bundle.Manifest.Files) {
		t.Fatalf("unexpected verified Bundle: %#v", bundle.Manifest)
	}

	item.SourceCommit = strings.Repeat("b", 40)
	if _, err := verifyResultBundle(filename, item, manifestSHA); err == nil {
		t.Fatal("Bundle for another frozen source Commit was accepted")
	}
}

func TestVerifyResultBundleRejectsTraversalAndHashMismatch(t *testing.T) {
	item := Experiment{
		ID:              "00000000-0000-4000-8000-000000000001",
		SourceCommit:    strings.Repeat("a", 40),
		ResultDirectory: "experiments/00000000-0000-4000-8000-000000000001_20260816_1200/",
		ActualRuntime:   "e2b", RuntimeVersion: "e2b-1",
	}
	manifest := resultManifest{
		SchemaVersion: "2", ExperimentID: item.ID, SourceCommit: item.SourceCommit,
		ResultDirectory: item.ResultDirectory, Status: "succeeded",
		StartedAt:  time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 8, 16, 4, 0, 1, 0, time.UTC),
		Runtime:    item.ActualRuntime, RuntimeVersion: item.RuntimeVersion,
		ExitCode: intPointer(0), Files: []PreparedResultFile{{
			Path: "../secret", SHA256: strings.Repeat("a", 64),
			SizeBytes: 1, Kind: "file", MediaType: "application/octet-stream",
		}},
	}
	filename, manifestSHA := writeResultBundle(t, manifest, map[string][]byte{"../secret": {'x'}})
	if _, err := verifyResultBundle(filename, item, manifestSHA); err == nil {
		t.Fatal("Bundle traversal was accepted")
	}
}

func writeResultBundle(
	t *testing.T,
	manifest resultManifest,
	files map[string][]byte,
) (string, string) {
	t.Helper()
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := sha256.Sum256(manifestBytes)
	filename := t.TempDir() + "/execution-bundle.zip"
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(manifestBytes); err != nil {
		t.Fatal(err)
	}
	for name, contents := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return filename, hex.EncodeToString(manifestDigest[:])
}

func intPointer(value int) *int { return &value }
