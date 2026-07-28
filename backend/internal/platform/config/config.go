// Package config loads and validates Core Server configuration.
package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config contains the complete Core Server process configuration.
type Config struct {
	Addr            string
	Auth            AuthConfig
	Database        DatabaseConfig
	ObjectStorage   ObjectStorageConfig
	OpenAPIPath     string
	Outbox          OutboxConfig
	Settings        SettingsConfig
	ShutdownTimeout time.Duration
	StartupTimeout  time.Duration
}

// AuthConfig configures bootstrap login and session signing.
type AuthConfig struct {
	BootstrapDisplayName string
	BootstrapEmail       string
	BootstrapPassword    string
	JWTSecret            string
	SessionTTL           time.Duration
}

// DatabaseConfig configures the PostgreSQL connection pool.
type DatabaseConfig struct {
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxIdleTime time.Duration
	ConnMaxLifetime time.Duration
	URL             string
}

// ObjectStorageConfig configures the S3-compatible object storage boundary.
type ObjectStorageConfig struct {
	AccessKey string
	Bucket    string
	Endpoint  string
	SecretKey string
}

// OutboxConfig configures durable event publication and delivery.
type OutboxConfig struct {
	DeliveryLease time.Duration
	EventLease    time.Duration
	PollInterval  time.Duration
	RetryDelay    time.Duration
}

// SettingsConfig configures encryption for persisted module secrets.
type SettingsConfig struct {
	EncryptionKey string
}

// LookupEnv matches os.LookupEnv and keeps configuration tests deterministic.
type LookupEnv func(string) (string, bool)

// Load builds a validated Config from environment variables.
func Load(lookup LookupEnv) (Config, error) {
	config := Config{
		Addr: envOrDefault(lookup, "CORE_ADDR", ":8080"),
		Auth: AuthConfig{
			BootstrapDisplayName: envOrDefault(lookup, "AUTH_BOOTSTRAP_DISPLAY_NAME", "mmdash Admin"),
			BootstrapEmail:       envOrDefault(lookup, "AUTH_BOOTSTRAP_EMAIL", "admin@mmdash.local"),
			BootstrapPassword:    envOrDefault(lookup, "AUTH_BOOTSTRAP_PASSWORD", "mmdash-local-admin"),
			JWTSecret:            envOrDefault(lookup, "AUTH_JWT_SECRET", "development-auth-jwt-secret-change-me"),
			SessionTTL:           durationOrDefault(lookup, "AUTH_SESSION_TTL", 24*time.Hour),
		},
		Database: DatabaseConfig{
			ConnMaxIdleTime: durationOrDefault(lookup, "DATABASE_CONN_MAX_IDLE_TIME", 5*time.Minute),
			ConnMaxLifetime: durationOrDefault(lookup, "DATABASE_CONN_MAX_LIFETIME", 30*time.Minute),
			MaxIdleConns:    intOrDefault(lookup, "DATABASE_MAX_IDLE_CONNS", 5),
			MaxOpenConns:    intOrDefault(lookup, "DATABASE_MAX_OPEN_CONNS", 20),
			URL:             envOrDefault(lookup, "DATABASE_URL", ""),
		},
		ObjectStorage: ObjectStorageConfig{
			AccessKey: envOrDefault(lookup, "OBJECT_STORAGE_ACCESS_KEY", ""),
			Bucket:    envOrDefault(lookup, "OBJECT_STORAGE_BUCKET", "mmdash"),
			Endpoint:  envOrDefault(lookup, "OBJECT_STORAGE_ENDPOINT", ""),
			SecretKey: envOrDefault(lookup, "OBJECT_STORAGE_SECRET_KEY", ""),
		},
		OpenAPIPath: envOrDefault(lookup, "CORE_OPENAPI_PATH", "contracts/openapi/core.yaml"),
		Outbox: OutboxConfig{
			DeliveryLease: durationOrDefault(lookup, "OUTBOX_DELIVERY_LEASE", 30*time.Second),
			EventLease:    durationOrDefault(lookup, "OUTBOX_EVENT_LEASE", 30*time.Second),
			PollInterval:  durationOrDefault(lookup, "OUTBOX_POLL_INTERVAL", 500*time.Millisecond),
			RetryDelay:    durationOrDefault(lookup, "OUTBOX_RETRY_DELAY", 2*time.Second),
		},
		Settings: SettingsConfig{
			EncryptionKey: envOrDefault(
				lookup,
				"SETTINGS_ENCRYPTION_KEY",
				"development-settings-encryption-key-change-me",
			),
		},
		ShutdownTimeout: durationOrDefault(lookup, "CORE_SHUTDOWN_TIMEOUT", 10*time.Second),
		StartupTimeout:  durationOrDefault(lookup, "CORE_STARTUP_TIMEOUT", 15*time.Second),
	}

	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Validate rejects unsafe or incomplete process configuration.
func (config Config) Validate() error {
	if strings.TrimSpace(config.Addr) == "" {
		return fmt.Errorf("CORE_ADDR must not be empty")
	}
	if strings.TrimSpace(config.Database.URL) == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if !strings.Contains(config.Auth.BootstrapEmail, "@") {
		return fmt.Errorf("AUTH_BOOTSTRAP_EMAIL must be an email address")
	}
	if len(config.Auth.BootstrapPassword) < 12 {
		return fmt.Errorf("AUTH_BOOTSTRAP_PASSWORD must contain at least 12 characters")
	}
	if len(config.Auth.JWTSecret) < 32 {
		return fmt.Errorf("AUTH_JWT_SECRET must contain at least 32 characters")
	}
	if config.Auth.SessionTTL <= 0 {
		return fmt.Errorf("AUTH_SESSION_TTL must be positive")
	}
	if config.Database.MaxOpenConns < 1 {
		return fmt.Errorf("DATABASE_MAX_OPEN_CONNS must be positive")
	}
	if config.Database.MaxIdleConns < 0 || config.Database.MaxIdleConns > config.Database.MaxOpenConns {
		return fmt.Errorf("DATABASE_MAX_IDLE_CONNS must be between zero and the open connection limit")
	}
	if config.Database.ConnMaxIdleTime <= 0 || config.Database.ConnMaxLifetime <= 0 {
		return fmt.Errorf("database connection lifetimes must be positive")
	}
	endpoint, err := url.Parse(config.ObjectStorage.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return fmt.Errorf("OBJECT_STORAGE_ENDPOINT must be an HTTP(S) URL")
	}
	if strings.TrimSpace(config.ObjectStorage.AccessKey) == "" {
		return fmt.Errorf("OBJECT_STORAGE_ACCESS_KEY is required")
	}
	if strings.TrimSpace(config.ObjectStorage.SecretKey) == "" {
		return fmt.Errorf("OBJECT_STORAGE_SECRET_KEY is required")
	}
	if strings.TrimSpace(config.ObjectStorage.Bucket) == "" {
		return fmt.Errorf("OBJECT_STORAGE_BUCKET is required")
	}
	if strings.TrimSpace(config.OpenAPIPath) == "" {
		return fmt.Errorf("CORE_OPENAPI_PATH must not be empty")
	}
	if config.Outbox.DeliveryLease <= 0 ||
		config.Outbox.EventLease <= 0 ||
		config.Outbox.PollInterval <= 0 ||
		config.Outbox.RetryDelay <= 0 {
		return fmt.Errorf("Outbox durations must be positive")
	}
	if len(config.Settings.EncryptionKey) < 32 {
		return fmt.Errorf("SETTINGS_ENCRYPTION_KEY must contain at least 32 characters")
	}
	if config.StartupTimeout <= 0 || config.ShutdownTimeout <= 0 {
		return fmt.Errorf("Core timeouts must be positive")
	}
	return nil
}

func envOrDefault(lookup LookupEnv, key, fallback string) string {
	if value, ok := lookup(key); ok && value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(lookup LookupEnv, key string, fallback time.Duration) time.Duration {
	value, ok := lookup(key)
	if !ok || value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return -1
	}
	return parsed
}

func intOrDefault(lookup LookupEnv, key string, fallback int) int {
	value, ok := lookup(key)
	if !ok || value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return parsed
}
