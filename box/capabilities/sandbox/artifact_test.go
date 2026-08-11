package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateManifestOwnsIdentityStatusAndFileMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tables"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "summary.md"), []byte("accepted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tables", "result.csv"), []byte("x,y\n1,2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, manifestFilename), []byte(`{"experiment_id":"forged","status":"failed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := GenerateManifest(root, "experiment-1", 0, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ExperimentID != "experiment-1" || manifest.Status != "succeeded" || manifest.ExitCode == nil || *manifest.ExitCode != 0 || manifest.Summary != "accepted\n" {
		t.Fatalf("unexpected manifest header: %#v", manifest)
	}
	if len(manifest.Files) != 2 || manifest.Files[0].Path != "summary.md" || manifest.Files[0].Kind != "summary" || manifest.Files[1].Path != "tables/result.csv" || manifest.Files[1].Kind != "table" {
		t.Fatalf("unexpected generated files: %#v", manifest.Files)
	}
	for _, file := range manifest.Files {
		if file.SHA256 == "" || file.Size == 0 {
			t.Fatalf("missing generated metadata: %#v", file)
		}
	}
}

func TestGenerateManifestRejectsUnsafeOrOversizedOutput(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "large.bin"), []byte("too large"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateManifest(root, "experiment-1", 0, 1); err == nil {
		t.Fatal("oversized output was accepted")
	}
	if err := os.Remove(filepath.Join(root, "large.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "unsafe")); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateManifest(root, "experiment-1", 0, 1<<20); err == nil {
		t.Fatal("output symlink was accepted")
	}
}
