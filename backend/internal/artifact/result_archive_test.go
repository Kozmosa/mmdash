package artifact

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestArchiveExperimentResultAssignsAndRepairsManagedFolder(t *testing.T) {
	experimentID := "00000000-0000-4000-8000-000000000001"
	resultContents := []byte("summary")
	manifest := map[string]interface{}{
		"schema_version": "2", "experiment_id": experimentID, "status": "succeeded",
		"files": []map[string]interface{}{{
			"path": "summary.md", "sha256": articleTemplateSHA(resultContents),
			"size_bytes": len(resultContents), "kind": "summary",
		}},
	}
	archivePath := writeZip(t, manifest, map[string][]byte{"summary.md": resultContents})
	contents, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read result archive: %v", err)
	}
	store := newArticleTemplateStore()
	storage, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	service := Service{
		Generator: &articleTemplateIDs{}, MaxUploadBytes: 20 * 1024 * 1024,
		MultipartPartBytes: MultipartMinPartBytes, Storage: storage, Store: store,
	}
	folderPath := []string{"experiment", strings.Repeat("a", 40) + "_20260902T010203.000000Z"}
	sha := articleTemplateSHA(contents)
	first, err := service.ArchiveExperimentResult(
		context.Background(), "project-result", experimentID, "system", folderPath,
		sha, int64(len(contents)), bytes.NewReader(contents),
	)
	if err != nil || first.CurrentVersion == nil || first.Artifact.FolderID == nil {
		t.Fatalf("archive result: %#v %v", first, err)
	}
	leafID := *first.Artifact.FolderID
	legacyKey := artifactIDKey("project-result", first.Artifact.ID)
	legacy := store.details[legacyKey]
	legacy.Artifact.FolderID = nil
	store.details[legacyKey] = legacy

	retried, err := service.ArchiveExperimentResult(
		context.Background(), "project-result", experimentID, "system", folderPath,
		sha, int64(len(contents)), failingReader{},
	)
	if err != nil || retried.Artifact.ID != first.Artifact.ID ||
		retried.Artifact.FolderID == nil || *retried.Artifact.FolderID != leafID {
		t.Fatalf("idempotent folder repair: %#v %v", retried, err)
	}
}

func TestArchiveExperimentFileUsesTheExperimentManagedFolder(t *testing.T) {
	contents := []byte("oversized result")
	store := newArticleTemplateStore()
	storage, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	service := Service{
		Generator: &articleTemplateIDs{}, MaxUploadBytes: 20 * 1024 * 1024,
		MultipartPartBytes: MultipartMinPartBytes, Storage: storage, Store: store,
	}
	folderPath := []string{"experiment", strings.Repeat("b", 40) + "_20260902T010203.000000Z"}
	detail, err := service.ArchiveExperimentFile(
		context.Background(), "project-result", "experiment-1", "system", folderPath,
		"data/large.bin", "application/octet-stream", articleTemplateSHA(contents),
		int64(len(contents)), bytes.NewReader(contents),
	)
	if err != nil || detail.Artifact.FolderID == nil ||
		detail.CurrentVersion == nil || detail.CurrentVersion.Filename != "large.bin" {
		t.Fatalf("archive large result: %#v %v", detail, err)
	}
	wantLeaf := "managed-folder-2"
	if *detail.Artifact.FolderID != wantLeaf {
		t.Fatalf("large result used folder %q, want %q", *detail.Artifact.FolderID, wantLeaf)
	}
}

func TestValidateArtifactZipChecksManifestHashesAndPaths(t *testing.T) {
	experimentID := "00000000-0000-4000-8000-000000000001"
	contents := []byte("summary")
	digest := sha256.Sum256(contents)
	manifest := map[string]interface{}{
		"schema_version": "2", "experiment_id": experimentID, "status": "succeeded",
		"files": []map[string]interface{}{{"path": "summary.md", "sha256": hex.EncodeToString(digest[:]), "size_bytes": len(contents), "kind": "summary"}},
	}
	filename := writeZip(t, manifest, map[string][]byte{"summary.md": contents})
	if err := validateArtifactZip(filename, experimentID); err != nil {
		t.Fatalf("valid artifact rejected: %v", err)
	}

	badManifest := map[string]interface{}{
		"schema_version": "2", "experiment_id": experimentID, "status": "succeeded",
		"files": []map[string]interface{}{{"path": "../secret", "sha256": strings.Repeat("a", 64), "size_bytes": 1}},
	}
	badFilename := writeZip(t, badManifest, map[string][]byte{"../secret": []byte("x")})
	if err := validateArtifactZip(badFilename, experimentID); err == nil {
		t.Fatal("zip-slip artifact was accepted")
	}
}

func TestStageResultInputRejectsSizeAndHashMismatch(t *testing.T) {
	filename, err := stageResultInput(bytes.NewReader([]byte("data")), 4, strings.Repeat("a", 64))
	if err == nil || filename != "" {
		t.Fatalf("hash mismatch result: %q %v", filename, err)
	}
	filename, err = stageResultInput(bytes.NewReader([]byte("data-extra")), 4, hex.EncodeToString(sha256Bytes([]byte("data"))))
	if err == nil || filename != "" {
		t.Fatalf("extra bytes result: %q %v", filename, err)
	}
}

func writeZip(t *testing.T, manifest map[string]interface{}, files map[string][]byte) string {
	t.Helper()
	filename := t.TempDir() + "/artifact.zip"
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	manifestBytes, _ := json.Marshal(manifest)
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
			_ = file.Close()
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return filename
}

func sha256Bytes(value []byte) []byte {
	digest := sha256.Sum256(value)
	return digest[:]
}
