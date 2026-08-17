package artifact

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

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
