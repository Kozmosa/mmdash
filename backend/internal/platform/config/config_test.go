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
}

func mapLookup(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
