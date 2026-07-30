package artifact

import (
	"path/filepath"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/platform/config"
)

func TestNewBlobStoreSelectsConfiguredAdapter(t *testing.T) {
	local, err := NewBlobStore(config.ArtifactConfig{
		LocalStorageRoot: filepath.Join(t.TempDir(), "artifacts"),
		StorageBackend:   "local",
	}, config.ObjectStorageConfig{})
	if err != nil {
		t.Fatalf("initialize Local store: %v", err)
	}
	if local.Backend() != "local" {
		t.Fatalf("unexpected Local backend: %s", local.Backend())
	}

	remote, err := NewBlobStore(config.ArtifactConfig{
		StorageBackend: "s3",
	}, config.ObjectStorageConfig{
		AccessKey: "access",
		Bucket:    "mmdash-artifacts",
		Endpoint:  "https://s3.example.test",
		Region:    "us-east-1",
		SecretKey: "secret",
	})
	if err != nil {
		t.Fatalf("initialize S3 store: %v", err)
	}
	if remote.Backend() != "s3" {
		t.Fatalf("unexpected remote backend: %s", remote.Backend())
	}
}
