// Package objectstorage initializes and checks the S3-compatible storage boundary.
package objectstorage

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/config"
)

// MinIO is the initialized object storage dependency.
//
// Artifact operations are intentionally added by the Artifact module later;
// the platform owns configuration, credentials, bucket identity, and readiness.
type MinIO struct {
	accessKey string
	bucket    string
	client    *http.Client
	endpoint  *url.URL
	secretKey string
}

// NewMinIO validates configuration and initializes a reusable client.
func NewMinIO(storageConfig config.ObjectStorageConfig) (*MinIO, error) {
	endpoint, err := url.Parse(storageConfig.Endpoint)
	if err != nil || endpoint.Host == "" {
		return nil, fmt.Errorf("parse object storage endpoint")
	}
	return &MinIO{
		accessKey: storageConfig.AccessKey,
		bucket:    storageConfig.Bucket,
		client:    &http.Client{Timeout: 5 * time.Second},
		endpoint:  endpoint,
		secretKey: storageConfig.SecretKey,
	}, nil
}

// Name returns the dependency name used in readiness output.
func (*MinIO) Name() string {
	return "object_storage"
}

// Bucket returns the configured authoritative artifact bucket.
func (storage *MinIO) Bucket() string {
	return storage.bucket
}

// Check verifies the MinIO-compatible service is ready.
func (storage *MinIO) Check(ctx context.Context) error {
	healthURL := *storage.endpoint
	healthURL.Path = strings.TrimRight(healthURL.Path, "/") + "/minio/health/ready"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create object storage health request: %w", err)
	}
	response, err := storage.client.Do(request)
	if err != nil {
		return fmt.Errorf("check object storage readiness: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("object storage readiness returned HTTP %d", response.StatusCode)
	}
	return nil
}
