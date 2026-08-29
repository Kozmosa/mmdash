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

func TestVerifyResultBundleAcceptsProviderEnvironmentEvidence(t *testing.T) {
	// The Box Gateway stamps provider-neutral environment evidence with an
	// explicit provider and the resolved dependency set. Core must accept
	// these fields for every environment-preparing Runtime.
	for _, runtimeName := range []string{"local-docker", "local-process"} {
		item := Experiment{
			ID:              "00000000-0000-4000-8000-000000000001",
			SourceCommit:    strings.Repeat("a", 40),
			ResultDirectory: "experiments/00000000-0000-4000-8000-000000000001_20260816_1200/",
			ActualRuntime:   runtimeName, RuntimeVersion: "runtime-1", LogsTruncated: false,
		}
		manifest := resultManifest{
			SchemaVersion: "2", ExperimentID: item.ID, SourceCommit: item.SourceCommit,
			ResultDirectory: item.ResultDirectory, Status: "succeeded",
			StartedAt:  time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC),
			FinishedAt: time.Date(2026, 8, 16, 4, 0, 1, 0, time.UTC),
			Runtime:    item.ActualRuntime, RuntimeVersion: item.RuntimeVersion,
			LogsTruncated: false, ExitCode: intPointer(0),
			Environment: &resultManifestEnvironment{
				Provider:       runtimeName,
				EnvironmentKey: strings.Repeat("b", 64), BaseImageID: "interpreter:3.13.1",
				EnvironmentImageID:   "venv:" + strings.Repeat("d", 64),
				ManifestPaths:        []string{"requirements.lock"},
				ManifestHashes:       map[string]string{"requirements.lock": strings.Repeat("c", 64)},
				ResolvedDependencies: []string{"numpy==2.5.2", "matplotlib==3.11.1"},
				BuilderVersion:       "1", CacheHit: false,
			},
		}
		filename, manifestSHA := writeResultBundle(t, manifest, nil)
		bundle, err := verifyResultBundle(filename, item, manifestSHA)
		if err != nil {
			t.Fatalf("runtime %s Bundle with provider evidence rejected: %v", runtimeName, err)
		}
		_ = bundle.Close()
	}
}

func TestVerifyResultBundleRequiresLocalProcessEnvironmentEvidence(t *testing.T) {
	item := Experiment{
		ID:              "00000000-0000-4000-8000-000000000002",
		SourceCommit:    strings.Repeat("a", 40),
		ResultDirectory: "experiments/00000000-0000-4000-8000-000000000002_20260816_1200/",
		ActualRuntime:   "local-process", RuntimeVersion: "1",
	}
	manifest := resultManifest{
		SchemaVersion: "2", ExperimentID: item.ID, SourceCommit: item.SourceCommit,
		ResultDirectory: item.ResultDirectory, Status: "succeeded",
		StartedAt:  time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 8, 16, 4, 0, 1, 0, time.UTC),
		Runtime:    item.ActualRuntime, RuntimeVersion: item.RuntimeVersion,
		ExitCode: intPointer(0),
	}
	filename, manifestSHA := writeResultBundle(t, manifest, nil)
	if _, err := verifyResultBundle(filename, item, manifestSHA); err == nil {
		t.Fatal("local-process Bundle without environment evidence was accepted")
	}
	if validResultEnvironment("local-process", nil) {
		t.Fatal("validResultEnvironment must require evidence for local-process")
	}
	if validResultEnvironment("e2b", nil) != true {
		t.Fatal("e2b Bundles remain valid without environment evidence")
	}
}

func TestValidResultEnvironmentRejectsInvalidProviderAndDependencies(t *testing.T) {
	base := func() *resultManifestEnvironment {
		return &resultManifestEnvironment{
			EnvironmentKey: strings.Repeat("b", 64), BaseImageID: "interpreter:3.13.1",
			EnvironmentImageID: "venv:" + strings.Repeat("d", 64),
			ManifestPaths:      []string{"requirements.lock"},
			ManifestHashes:     map[string]string{"requirements.lock": strings.Repeat("c", 64)},
			BuilderVersion:     "1",
		}
	}
	if !validResultEnvironment("local-process", base()) {
		t.Fatal("evidence without optional fields must stay valid")
	}
	unknownProvider := base()
	unknownProvider.Provider = "e2b"
	if validResultEnvironment("local-process", unknownProvider) {
		t.Fatal("unknown provider was accepted")
	}
	blankDependency := base()
	blankDependency.ResolvedDependencies = []string{"  "}
	if validResultEnvironment("local-process", blankDependency) {
		t.Fatal("blank resolved dependency was accepted")
	}
	oversized := base()
	oversized.ResolvedDependencies = make([]string, 2001)
	for index := range oversized.ResolvedDependencies {
		oversized.ResolvedDependencies[index] = "pkg==1.0"
	}
	if validResultEnvironment("local-process", oversized) {
		t.Fatal("more than 2000 resolved dependencies were accepted")
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
