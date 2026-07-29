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
	if config.Repo.MaxConcurrentGit != 4 ||
		config.Repo.CommandTimeout != 2*time.Minute ||
		config.Repo.MaxTextBytes != 1024*1024 {
		t.Fatalf("unexpected Repo defaults: %+v", config.Repo)
	}
	if config.Version != "0.1.0" {
		t.Fatalf("unexpected service version: %s", config.Version)
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
		"DATABASE_URL":              "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY": "access",
		"OBJECT_STORAGE_ENDPOINT":   "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY": "secret",
		"REPO_MAX_CONCURRENT_GIT":   "0",
	}))
	if err == nil {
		t.Fatal("expected invalid Repo concurrency to fail")
	}
}

func mapLookup(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
