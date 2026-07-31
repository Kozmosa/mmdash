package artifact

import (
	"fmt"

	"github.com/mmdash/mmdash/backend/internal/platform/config"
)

// NewBlobStore selects exactly one process-configured storage adapter.
func NewBlobStore(
	artifactConfig config.ArtifactConfig,
	storageConfig config.ObjectStorageConfig,
) (BlobStore, error) {
	switch artifactConfig.StorageBackend {
	case "local":
		return NewLocalBlobStore(artifactConfig.LocalStorageRoot)
	case "minio", "s3":
		return NewS3BlobStore(S3BlobStoreConfig{
			AccessKey:      storageConfig.AccessKey,
			Backend:        artifactConfig.StorageBackend,
			Bucket:         storageConfig.Bucket,
			Endpoint:       storageConfig.Endpoint,
			PublicEndpoint: storageConfig.PublicEndpoint,
			Region:         storageConfig.Region,
			SecretKey:      storageConfig.SecretKey,
		})
	default:
		return nil, fmt.Errorf("unsupported Artifact storage backend")
	}
}
