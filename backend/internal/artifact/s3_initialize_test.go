package artifact

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestEnsureS3BucketRejectsWildcardAndPathOrigins(t *testing.T) {
	storageConfig := S3BlobStoreConfig{
		AccessKey: "access",
		Backend:   "minio",
		Bucket:    "mmdash",
		Endpoint:  "http://localhost:9000",
		Region:    "us-east-1",
		SecretKey: "secret",
	}
	for _, origin := range []string{"*", "https://example.test/path", "file:///tmp"} {
		if err := EnsureS3Bucket(
			context.Background(),
			storageConfig,
			origin,
		); err == nil {
			t.Fatalf("expected invalid origin %q to fail", origin)
		}
	}
}

func TestEnsureS3BucketRealMinIOCORS(t *testing.T) {
	endpoint := os.Getenv("MMDASH_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("MMDASH_TEST_MINIO_ENDPOINT is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := EnsureS3Bucket(ctx, S3BlobStoreConfig{
		AccessKey: envForTest("MMDASH_TEST_MINIO_ACCESS_KEY", "mmdash"),
		Backend:   "minio",
		Bucket:    envForTest("MMDASH_TEST_MINIO_BUCKET", "mmdash-artifact-integration"),
		Endpoint:  endpoint,
		Region:    "us-east-1",
		SecretKey: envForTest("MMDASH_TEST_MINIO_SECRET_KEY", "change-me"),
	}, "http://localhost:3000")
	if err != nil {
		t.Fatalf("ensure real MinIO bucket and CORS: %v", err)
	}
}
