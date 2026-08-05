package config

import (
	"testing"
	"time"
)

func TestLoadReturnsValidatedConfiguration(t *testing.T) {
	environment := map[string]string{
		"DATABASE_URL":              "postgres://mmdash:test@localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY": "access",
		"OBJECT_STORAGE_ENDPOINT":   "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY": "secret",
	}

	config, err := Load(mapLookup(environment))
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}

	if config.Addr != ":8080" {
		t.Fatalf("unexpected address: %s", config.Addr)
	}
	if config.Database.MaxOpenConns != 20 {
		t.Fatalf("unexpected max open connections: %d", config.Database.MaxOpenConns)
	}
	if config.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected shutdown timeout: %s", config.ShutdownTimeout)
	}
	if config.Outbox.PollInterval != 500*time.Millisecond {
		t.Fatalf("unexpected Outbox poll interval: %s", config.Outbox.PollInterval)
	}
	if config.Progress.ReminderBatchSize != 20 ||
		config.Progress.ReminderLease != 30*time.Second ||
		config.Progress.ReminderPollInterval != time.Second ||
		config.Progress.ReminderRetryDelay != 2*time.Second {
		t.Fatalf("unexpected Progress reminder defaults: %+v", config.Progress)
	}
	if config.Artifact.StorageBackend != "minio" ||
		config.Artifact.MultipartPartBytes != 16*1024*1024 ||
		config.Artifact.UploadMaxBytes != 10*1024*1024*1024 ||
		config.Artifact.PreviewOutputMaxBytes != 4*1024*1024 ||
		config.Artifact.MultipartURLTTL != 15*time.Minute ||
		config.ObjectStorage.PublicEndpoint != "" {
		t.Fatalf("unexpected Artifact defaults: %+v %+v", config.Artifact, config.ObjectStorage)
	}
	if config.Repo.MaxConcurrentGit != 4 ||
		config.Repo.CommandTimeout != 2*time.Minute ||
		config.Repo.MaxTextBytes != 1024*1024 {
		t.Fatalf("unexpected Repo defaults: %+v", config.Repo)
	}
	if config.Version != "0.1.0" {
		t.Fatalf("unexpected service version: %s", config.Version)
	}
	if config.InternalURL != "http://localhost:8080" {
		t.Fatalf("unexpected internal Core URL: %s", config.InternalURL)
	}
	if config.Auth.AccessTokenTTL != 24*time.Hour ||
		config.Auth.SessionTTL != 30*24*time.Hour ||
		config.Auth.DeviceAuthorizationTTL != 10*time.Minute ||
		config.Auth.DevicePollInterval != 5*time.Second {
		t.Fatalf("unexpected Auth defaults: %+v", config.Auth)
	}
}

func TestLoadRejectsMissingAndInvalidConfiguration(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{}))
	if err == nil {
		t.Fatal("expected missing configuration to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"DATABASE_URL":              "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY": "access",
		"OBJECT_STORAGE_ENDPOINT":   "ftp://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY": "secret",
	}))
	if err == nil {
		t.Fatal("expected invalid object storage endpoint to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"DATABASE_URL":                   "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY":      "access",
		"OBJECT_STORAGE_ENDPOINT":        "http://localhost:9000",
		"OBJECT_STORAGE_PUBLIC_ENDPOINT": "https://storage.example.test/path",
		"OBJECT_STORAGE_SECRET_KEY":      "secret",
	}))
	if err == nil {
		t.Fatal("expected object storage public endpoint path to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"DATABASE_URL":              "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY": "access",
		"OBJECT_STORAGE_ENDPOINT":   "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY": "secret",
		"SETTINGS_ENCRYPTION_KEY":   "too-short",
	}))
	if err == nil {
		t.Fatal("expected short settings encryption key to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"DATABASE_URL":              "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY": "access",
		"OBJECT_STORAGE_ENDPOINT":   "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY": "secret",
		"OUTBOX_POLL_INTERVAL":      "invalid",
	}))
	if err == nil {
		t.Fatal("expected invalid Outbox interval to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"DATABASE_URL":                    "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY":       "access",
		"OBJECT_STORAGE_ENDPOINT":         "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY":       "secret",
		"PROGRESS_REMINDER_BATCH_SIZE":    "0",
		"PROGRESS_REMINDER_POLL_INTERVAL": "0s",
	}))
	if err == nil {
		t.Fatal("expected invalid Progress reminder processor configuration to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"DATABASE_URL":              "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY": "access",
		"OBJECT_STORAGE_ENDPOINT":   "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY": "secret",
		"REPO_MAX_CONCURRENT_GIT":   "0",
	}))
	if err == nil {
		t.Fatal("expected invalid Repo concurrency to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"ARTIFACT_STORAGE_BACKEND":    "local",
		"ARTIFACT_LOCAL_STORAGE_ROOT": t.TempDir(),
		"DATABASE_URL":                "postgres://localhost/mmdash",
	}))
	if err != nil {
		t.Fatalf("expected Local storage without S3 credentials to load: %v", err)
	}

	_, err = Load(mapLookup(map[string]string{
		"ARTIFACT_MULTIPART_PART_BYTES": "1048576",
		"DATABASE_URL":                  "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY":     "access",
		"OBJECT_STORAGE_ENDPOINT":       "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY":     "secret",
	}))
	if err == nil {
		t.Fatal("expected undersized multipart parts to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"ARTIFACT_MULTIPART_SESSION_TTL": "2h",
		"ARTIFACT_STAGING_TTL":           "1h",
		"DATABASE_URL":                   "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY":      "access",
		"OBJECT_STORAGE_ENDPOINT":        "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY":      "secret",
	}))
	if err == nil {
		t.Fatal("expected staging TTL shorter than session TTL to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"ARTIFACT_PREVIEW_OUTPUT_MAX_BYTES": "0",
		"DATABASE_URL":                      "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY":         "access",
		"OBJECT_STORAGE_ENDPOINT":           "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY":         "secret",
	}))
	if err == nil {
		t.Fatal("expected invalid preview output limit to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"CORE_INTERNAL_URL":         "ftp://core:8080",
		"DATABASE_URL":              "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY": "access",
		"OBJECT_STORAGE_ENDPOINT":   "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY": "secret",
	}))
	if err == nil {
		t.Fatal("expected invalid internal Core URL to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"AUTH_DEVICE_AUTHORIZATION_TTL": "5s",
		"AUTH_DEVICE_POLL_INTERVAL":     "5s",
		"DATABASE_URL":                  "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY":     "access",
		"OBJECT_STORAGE_ENDPOINT":       "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY":     "secret",
	}))
	if err == nil {
		t.Fatal("expected device authorization TTL no longer than its poll interval to fail")
	}
}

func mapLookup(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
