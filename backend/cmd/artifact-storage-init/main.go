package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mmdash/mmdash/backend/internal/artifact"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "artifact storage initialization failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return artifact.EnsureS3Bucket(ctx, artifact.S3BlobStoreConfig{
		AccessKey:      os.Getenv("OBJECT_STORAGE_ACCESS_KEY"),
		Backend:        envOrDefault("ARTIFACT_STORAGE_BACKEND", "minio"),
		Bucket:         envOrDefault("OBJECT_STORAGE_BUCKET", "mmdash"),
		Endpoint:       os.Getenv("OBJECT_STORAGE_ENDPOINT"),
		PublicEndpoint: os.Getenv("OBJECT_STORAGE_PUBLIC_ENDPOINT"),
		Region:         envOrDefault("OBJECT_STORAGE_REGION", "us-east-1"),
		SecretKey:      os.Getenv("OBJECT_STORAGE_SECRET_KEY"),
	}, os.Getenv("ARTIFACT_WEB_ORIGIN"))
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
