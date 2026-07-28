package objectstorage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/platform/config"
)

func TestMinIOInitializationAndReadiness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/minio/health/ready" {
			t.Fatalf("unexpected health path: %s", request.URL.Path)
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	storage, err := NewMinIO(config.ObjectStorageConfig{
		AccessKey: "access",
		Bucket:    "artifacts",
		Endpoint:  server.URL,
		SecretKey: "secret",
	})
	if err != nil {
		t.Fatalf("initialize MinIO: %v", err)
	}
	if storage.Bucket() != "artifacts" {
		t.Fatalf("unexpected bucket: %s", storage.Bucket())
	}
	if err := storage.Check(context.Background()); err != nil {
		t.Fatalf("check MinIO: %v", err)
	}
}
