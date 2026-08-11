// Package config loads and validates Core Server configuration.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config contains the complete Core Server process configuration.
type Config struct {
	Addr            string
	Agent           AgentConfig
	Artifact        ArtifactConfig
	Auth            AuthConfig
	Database        DatabaseConfig
	InternalURL     string
	Notion          NotionConfig
	ObjectStorage   ObjectStorageConfig
	OpenAPIPath     string
	Outbox          OutboxConfig
	Progress        ProgressConfig
	Project         ProjectConfig
	PublicURL       string
	Repo            RepoConfig
	Settings        SettingsConfig
	Version         string
	ShutdownTimeout time.Duration
	StartupTimeout  time.Duration
}

// NotionConfig configures the registered public Notion integration. Project
// access and refresh tokens are never process configuration; they are stored
// encrypted in Project Settings after each user authorization.
type NotionConfig struct {
	OAuthClientID     string
	OAuthClientSecret string
	OAuthRedirectURI  string
}

// AgentConfig configures the product Agent runtime boundary. Connector policy
// is deployment-owned and cannot be weakened by per-project Agent settings.
type AgentConfig struct {
	GatewayURL                string
	Management                AgentConnectorConfig
	ManagementMinimumInterval time.Duration
	Runtime                   AgentConnectorConfig
}

// AgentConnectorConfig limits one class of Hermes outbound connections.
type AgentConnectorConfig struct {
	AllowLoopback         bool
	AllowPrivate          bool
	AllowedPorts          []int
	ConnectTimeout        time.Duration
	MaxRedirects          int
	MaxResponseBytes      int64
	RequestTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
}

// ArtifactConfig configures multipart limits and local storage behavior.
type ArtifactConfig struct {
	LocalStorageRoot      string
	MultipartPartBytes    int64
	MultipartSessionTTL   time.Duration
	MultipartURLTTL       time.Duration
	PreviewOutputMaxBytes int64
	StagingSweepInterval  time.Duration
	StagingTTL            time.Duration
	StorageBackend        string
	UploadMaxBytes        int64
}

// AuthConfig configures bootstrap login and session signing.
type AuthConfig struct {
	AccessTokenTTL         time.Duration
	BootstrapDisplayName   string
	BootstrapEmail         string
	BootstrapPassword      string
	DeviceAuthorizationTTL time.Duration
	DevicePollInterval     time.Duration
	JWTSecret              string
	SessionTTL             time.Duration
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
	AccessKey      string
	Bucket         string
	Endpoint       string
	PublicEndpoint string
	Region         string
	SecretKey      string
}

// OutboxConfig configures durable event publication and delivery.
type OutboxConfig struct {
	DeliveryLease time.Duration
	EventLease    time.Duration
	PollInterval  time.Duration
	RetryDelay    time.Duration
}

// ProgressConfig configures the in-Core reminder and automatic tracking processors.
type ProgressConfig struct {
	EvaluatorMode        string
	ReminderBatchSize    int
	ReminderLease        time.Duration
	ReminderPollInterval time.Duration
	ReminderRetryDelay   time.Duration
	TrackingLease        time.Duration
	TrackingPollInterval time.Duration
	TrackingRetryDelay   time.Duration
}

// ProjectConfig configures Project-owned lifecycle processors.
type ProjectConfig struct {
	InvitationExpiryBatchSize    int
	InvitationExpiryPollInterval time.Duration
}

// RepoConfig configures the managed Git runtime and synchronization loop.
type RepoConfig struct {
	AskPassPath       string
	CheckoutTTL       time.Duration
	CloneTimeout      time.Duration
	CommandTimeout    time.Duration
	DisconnectGrace   time.Duration
	LocalAllowedRoots []string
	MaxConcurrentGit  int
	MaxTextBytes      int64
	StorageRoot       string
	SyncLease         time.Duration
	SyncPollInterval  time.Duration
}

// SettingsConfig configures encryption for persisted module secrets.
type SettingsConfig struct {
	EncryptionKey string
}

// LookupEnv matches os.LookupEnv and keeps configuration tests deterministic.
type LookupEnv func(string) (string, bool)

// Load builds a validated Config from environment variables.
func Load(lookup LookupEnv) (Config, error) {
	runtimeConnector, err := loadAgentConnector(
		lookup, "AGENT_RUNTIME", []int{80, 443, 8642},
	)
	if err != nil {
		return Config{}, err
	}
	managementConnector, err := loadAgentConnector(
		lookup, "AGENT_MANAGEMENT", []int{80, 443, 9119},
	)
	if err != nil {
		return Config{}, err
	}
	config := Config{
		Addr: envOrDefault(lookup, "CORE_ADDR", ":8080"),
		Agent: AgentConfig{
			GatewayURL: envOrDefault(
				lookup, "AGENT_MCP_GATEWAY_URL", "http://localhost:3002/mcp",
			),
			Management: managementConnector,
			ManagementMinimumInterval: durationOrDefault(
				lookup, "AGENT_MANAGEMENT_MINIMUM_INTERVAL", 250*time.Millisecond,
			),
			Runtime: runtimeConnector,
		},
		Artifact: ArtifactConfig{
			LocalStorageRoot: envOrDefault(
				lookup,
				"ARTIFACT_LOCAL_STORAGE_ROOT",
				"/var/lib/mmdash/artifacts",
			),
			MultipartPartBytes: int64OrDefault(
				lookup,
				"ARTIFACT_MULTIPART_PART_BYTES",
				16*1024*1024,
			),
			MultipartSessionTTL: durationOrDefault(
				lookup,
				"ARTIFACT_MULTIPART_SESSION_TTL",
				24*time.Hour,
			),
			MultipartURLTTL: durationOrDefault(
				lookup,
				"ARTIFACT_MULTIPART_URL_TTL",
				15*time.Minute,
			),
			PreviewOutputMaxBytes: int64OrDefault(
				lookup,
				"ARTIFACT_PREVIEW_OUTPUT_MAX_BYTES",
				4*1024*1024,
			),
			StagingSweepInterval: durationOrDefault(
				lookup,
				"ARTIFACT_STAGING_SWEEP_INTERVAL",
				5*time.Minute,
			),
			StagingTTL: durationOrDefault(
				lookup,
				"ARTIFACT_STAGING_TTL",
				24*time.Hour,
			),
			StorageBackend: envOrDefault(
				lookup,
				"ARTIFACT_STORAGE_BACKEND",
				"minio",
			),
			UploadMaxBytes: int64OrDefault(
				lookup,
				"ARTIFACT_UPLOAD_MAX_BYTES",
				10*1024*1024*1024,
			),
		},
		Auth: AuthConfig{
			AccessTokenTTL:         durationOrDefault(lookup, "AUTH_ACCESS_TOKEN_TTL", 24*time.Hour),
			BootstrapDisplayName:   envOrDefault(lookup, "AUTH_BOOTSTRAP_DISPLAY_NAME", "mmdash Admin"),
			BootstrapEmail:         envOrDefault(lookup, "AUTH_BOOTSTRAP_EMAIL", "admin@mmdash.local"),
			BootstrapPassword:      envOrDefault(lookup, "AUTH_BOOTSTRAP_PASSWORD", "mmdash-local-admin"),
			JWTSecret:              envOrDefault(lookup, "AUTH_JWT_SECRET", "development-auth-jwt-secret-change-me"),
			DeviceAuthorizationTTL: durationOrDefault(lookup, "AUTH_DEVICE_AUTHORIZATION_TTL", 10*time.Minute),
			DevicePollInterval:     durationOrDefault(lookup, "AUTH_DEVICE_POLL_INTERVAL", 5*time.Second),
			SessionTTL:             durationOrDefault(lookup, "AUTH_SESSION_TTL", 30*24*time.Hour),
		},
		Database: DatabaseConfig{
			ConnMaxIdleTime: durationOrDefault(lookup, "DATABASE_CONN_MAX_IDLE_TIME", 5*time.Minute),
			ConnMaxLifetime: durationOrDefault(lookup, "DATABASE_CONN_MAX_LIFETIME", 30*time.Minute),
			MaxIdleConns:    intOrDefault(lookup, "DATABASE_MAX_IDLE_CONNS", 5),
			MaxOpenConns:    intOrDefault(lookup, "DATABASE_MAX_OPEN_CONNS", 20),
			URL:             envOrDefault(lookup, "DATABASE_URL", ""),
		},
		ObjectStorage: ObjectStorageConfig{
			AccessKey:      envOrDefault(lookup, "OBJECT_STORAGE_ACCESS_KEY", ""),
			Bucket:         envOrDefault(lookup, "OBJECT_STORAGE_BUCKET", "mmdash"),
			Endpoint:       envOrDefault(lookup, "OBJECT_STORAGE_ENDPOINT", ""),
			PublicEndpoint: envOrDefault(lookup, "OBJECT_STORAGE_PUBLIC_ENDPOINT", ""),
			Region:         envOrDefault(lookup, "OBJECT_STORAGE_REGION", "us-east-1"),
			SecretKey:      envOrDefault(lookup, "OBJECT_STORAGE_SECRET_KEY", ""),
		},
		OpenAPIPath: envOrDefault(lookup, "CORE_OPENAPI_PATH", "contracts/openapi/core.yaml"),
		InternalURL: envOrDefault(lookup, "CORE_INTERNAL_URL", "http://localhost:8080"),
		PublicURL:   envOrDefault(lookup, "MMDASH_PUBLIC_URL", "http://localhost:3000"),
		Notion: NotionConfig{
			OAuthClientID:     envOrDefault(lookup, "NOTION_OAUTH_CLIENT_ID", ""),
			OAuthClientSecret: envOrDefault(lookup, "NOTION_OAUTH_CLIENT_SECRET", ""),
			OAuthRedirectURI:  envOrDefault(lookup, "NOTION_OAUTH_REDIRECT_URI", ""),
		},
		Outbox: OutboxConfig{
			DeliveryLease: durationOrDefault(lookup, "OUTBOX_DELIVERY_LEASE", 30*time.Second),
			EventLease:    durationOrDefault(lookup, "OUTBOX_EVENT_LEASE", 30*time.Second),
			PollInterval:  durationOrDefault(lookup, "OUTBOX_POLL_INTERVAL", 500*time.Millisecond),
			RetryDelay:    durationOrDefault(lookup, "OUTBOX_RETRY_DELAY", 2*time.Second),
		},
		Progress: ProgressConfig{
			EvaluatorMode:        envOrDefault(lookup, "MMDASH_PROGRESS_EVALUATOR_MODE", "core_agent"),
			ReminderBatchSize:    intOrDefault(lookup, "PROGRESS_REMINDER_BATCH_SIZE", 20),
			ReminderLease:        durationOrDefault(lookup, "PROGRESS_REMINDER_LEASE", 30*time.Second),
			ReminderPollInterval: durationOrDefault(lookup, "PROGRESS_REMINDER_POLL_INTERVAL", time.Second),
			ReminderRetryDelay:   durationOrDefault(lookup, "PROGRESS_REMINDER_RETRY_DELAY", 2*time.Second),
			TrackingLease:        durationOrDefault(lookup, "PROGRESS_TRACKING_LEASE", 2*time.Minute),
			TrackingPollInterval: durationOrDefault(lookup, "PROGRESS_TRACKING_POLL_INTERVAL", time.Second),
			TrackingRetryDelay:   durationOrDefault(lookup, "PROGRESS_TRACKING_RETRY_DELAY", 30*time.Second),
		},
		Project: ProjectConfig{
			InvitationExpiryBatchSize: intOrDefault(
				lookup,
				"PROJECT_INVITATION_EXPIRY_BATCH_SIZE",
				100,
			),
			InvitationExpiryPollInterval: durationOrDefault(
				lookup,
				"PROJECT_INVITATION_EXPIRY_POLL_INTERVAL",
				30*time.Second,
			),
		},
		Repo: RepoConfig{
			AskPassPath:       envOrDefault(lookup, "REPO_ASKPASS_PATH", "mmdash-git-askpass"),
			CheckoutTTL:       durationOrDefault(lookup, "REPO_CHECKOUT_TTL", time.Hour),
			CloneTimeout:      durationOrDefault(lookup, "REPO_CLONE_TIMEOUT", 15*time.Minute),
			CommandTimeout:    durationOrDefault(lookup, "REPO_COMMAND_TIMEOUT", 2*time.Minute),
			DisconnectGrace:   durationOrDefault(lookup, "REPO_DISCONNECT_GRACE", 24*time.Hour),
			LocalAllowedRoots: pathList(lookup, "REPO_LOCAL_ALLOWED_ROOTS"),
			MaxConcurrentGit:  intOrDefault(lookup, "REPO_MAX_CONCURRENT_GIT", 4),
			MaxTextBytes:      int64OrDefault(lookup, "REPO_MAX_TEXT_BYTES", 1024*1024),
			StorageRoot:       envOrDefault(lookup, "REPO_STORAGE_ROOT", "/var/lib/mmdash/repos"),
			SyncLease:         durationOrDefault(lookup, "REPO_SYNC_LEASE", 20*time.Minute),
			SyncPollInterval:  durationOrDefault(lookup, "REPO_SYNC_POLL_INTERVAL", 2*time.Second),
		},
		Settings: SettingsConfig{
			EncryptionKey: envOrDefault(
				lookup,
				"SETTINGS_ENCRYPTION_KEY",
				"development-settings-encryption-key-change-me",
			),
		},
		Version:         envOrDefault(lookup, "MMDASH_VERSION", "0.1.0"),
		ShutdownTimeout: durationOrDefault(lookup, "CORE_SHUTDOWN_TIMEOUT", 10*time.Second),
		StartupTimeout:  durationOrDefault(lookup, "CORE_STARTUP_TIMEOUT", 15*time.Second),
	}
	if config.Notion.OAuthRedirectURI == "" {
		config.Notion.OAuthRedirectURI = strings.TrimRight(config.PublicURL, "/") + "/api/integrations/notion/oauth/callback"
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
	if err := validateAgentGatewayURL(config.Agent.GatewayURL); err != nil {
		return err
	}
	if err := validateAgentConnector("AGENT_RUNTIME", config.Agent.Runtime); err != nil {
		return err
	}
	if err := validateAgentConnector("AGENT_MANAGEMENT", config.Agent.Management); err != nil {
		return err
	}
	if config.Agent.ManagementMinimumInterval <= 0 {
		return fmt.Errorf("AGENT_MANAGEMENT_MINIMUM_INTERVAL must be positive")
	}
	if config.Artifact.StorageBackend != "local" &&
		config.Artifact.StorageBackend != "minio" &&
		config.Artifact.StorageBackend != "s3" {
		return fmt.Errorf("ARTIFACT_STORAGE_BACKEND must be local, minio, or s3")
	}
	if !filepath.IsAbs(config.Artifact.LocalStorageRoot) &&
		!strings.HasPrefix(config.Artifact.LocalStorageRoot, "/") {
		return fmt.Errorf("ARTIFACT_LOCAL_STORAGE_ROOT must be absolute")
	}
	if config.Artifact.UploadMaxBytes < 1 ||
		config.Artifact.UploadMaxBytes > 5*1024*1024*1024*1024 {
		return fmt.Errorf("ARTIFACT_UPLOAD_MAX_BYTES must be between 1 byte and 5 TiB")
	}
	if config.Artifact.PreviewOutputMaxBytes < 1 ||
		config.Artifact.PreviewOutputMaxBytes > 64*1024*1024 {
		return fmt.Errorf(
			"ARTIFACT_PREVIEW_OUTPUT_MAX_BYTES must be between 1 byte and 64 MiB",
		)
	}
	if config.Artifact.MultipartPartBytes < 5*1024*1024 ||
		config.Artifact.MultipartPartBytes > 5*1024*1024*1024 {
		return fmt.Errorf("ARTIFACT_MULTIPART_PART_BYTES must be between 5 MiB and 5 GiB")
	}
	if config.Artifact.MultipartURLTTL < time.Second ||
		config.Artifact.MultipartURLTTL > 7*24*time.Hour {
		return fmt.Errorf("ARTIFACT_MULTIPART_URL_TTL must be between one second and seven days")
	}
	if config.Artifact.MultipartSessionTTL <= 0 ||
		config.Artifact.StagingTTL <= 0 ||
		config.Artifact.StagingSweepInterval <= 0 {
		return fmt.Errorf("Artifact multipart and staging durations must be positive")
	}
	if config.Artifact.StagingTTL < config.Artifact.MultipartSessionTTL {
		return fmt.Errorf("ARTIFACT_STAGING_TTL must not be shorter than the upload session TTL")
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
	if config.Auth.SessionTTL <= 0 || config.Auth.AccessTokenTTL <= 0 || config.Auth.AccessTokenTTL > config.Auth.SessionTTL {
		return fmt.Errorf("AUTH_ACCESS_TOKEN_TTL must be positive and no longer than AUTH_SESSION_TTL")
	}
	if config.Auth.DeviceAuthorizationTTL <= config.Auth.DevicePollInterval || config.Auth.DevicePollInterval < time.Second || config.Auth.DevicePollInterval > 30*time.Second {
		return fmt.Errorf("device authorization durations are invalid")
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
	if config.Artifact.StorageBackend != "local" {
		endpoint, err := url.Parse(config.ObjectStorage.Endpoint)
		if err != nil ||
			endpoint.Host == "" ||
			endpoint.User != nil ||
			(endpoint.Path != "" && endpoint.Path != "/") ||
			endpoint.RawQuery != "" ||
			endpoint.Fragment != "" ||
			(endpoint.Scheme != "http" && endpoint.Scheme != "https") {
			return fmt.Errorf("OBJECT_STORAGE_ENDPOINT must be an HTTP(S) origin")
		}
		publicEndpoint := config.ObjectStorage.PublicEndpoint
		if publicEndpoint == "" {
			publicEndpoint = config.ObjectStorage.Endpoint
		}
		publicURL, err := url.Parse(publicEndpoint)
		if err != nil ||
			publicURL.Host == "" ||
			publicURL.User != nil ||
			(publicURL.Path != "" && publicURL.Path != "/") ||
			publicURL.RawQuery != "" ||
			publicURL.Fragment != "" ||
			(publicURL.Scheme != "http" && publicURL.Scheme != "https") {
			return fmt.Errorf("OBJECT_STORAGE_PUBLIC_ENDPOINT must be an HTTP(S) origin")
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
		if strings.TrimSpace(config.ObjectStorage.Region) == "" {
			return fmt.Errorf("OBJECT_STORAGE_REGION is required")
		}
	}
	if strings.TrimSpace(config.OpenAPIPath) == "" {
		return fmt.Errorf("CORE_OPENAPI_PATH must not be empty")
	}
	publicURL, err := url.Parse(config.PublicURL)
	if err != nil || publicURL.Host == "" ||
		(publicURL.Scheme != "http" && publicURL.Scheme != "https") {
		return fmt.Errorf("MMDASH_PUBLIC_URL must be an HTTP(S) URL")
	}
	redirectURI, err := url.Parse(config.Notion.OAuthRedirectURI)
	if err != nil || redirectURI.Host == "" || redirectURI.User != nil ||
		(redirectURI.Scheme != "http" && redirectURI.Scheme != "https") {
		return fmt.Errorf("NOTION_OAUTH_REDIRECT_URI must be an HTTP(S) URL")
	}
	clientIDSet := strings.TrimSpace(config.Notion.OAuthClientID) != ""
	clientSecretSet := strings.TrimSpace(config.Notion.OAuthClientSecret) != ""
	if clientIDSet != clientSecretSet {
		return fmt.Errorf("NOTION_OAUTH_CLIENT_ID and NOTION_OAUTH_CLIENT_SECRET must be configured together")
	}
	internalURL, err := url.Parse(config.InternalURL)
	if err != nil || internalURL.Host == "" ||
		(internalURL.Scheme != "http" && internalURL.Scheme != "https") {
		return fmt.Errorf("CORE_INTERNAL_URL must be an HTTP(S) URL")
	}
	if config.Outbox.DeliveryLease <= 0 ||
		config.Outbox.EventLease <= 0 ||
		config.Outbox.PollInterval <= 0 ||
		config.Outbox.RetryDelay <= 0 {
		return fmt.Errorf("Outbox durations must be positive")
	}
	if config.Progress.ReminderBatchSize < 1 || config.Progress.ReminderBatchSize > 1000 {
		return fmt.Errorf("PROGRESS_REMINDER_BATCH_SIZE must be between 1 and 1000")
	}
	if config.Progress.ReminderLease <= 0 ||
		config.Progress.ReminderPollInterval <= 0 ||
		config.Progress.ReminderRetryDelay <= 0 {
		return fmt.Errorf("Progress reminder durations must be positive")
	}
	if config.Progress.TrackingLease <= 0 ||
		config.Progress.TrackingPollInterval <= 0 ||
		config.Progress.TrackingRetryDelay <= 0 {
		return fmt.Errorf("Progress tracking durations must be positive")
	}
	if config.Progress.EvaluatorMode != "core_agent" && config.Progress.EvaluatorMode != "mock" {
		return fmt.Errorf("MMDASH_PROGRESS_EVALUATOR_MODE must be core_agent or mock")
	}
	if config.Project.InvitationExpiryBatchSize < 1 || config.Project.InvitationExpiryBatchSize > 1000 {
		return fmt.Errorf("PROJECT_INVITATION_EXPIRY_BATCH_SIZE must be between 1 and 1000")
	}
	if config.Project.InvitationExpiryPollInterval <= 0 {
		return fmt.Errorf("PROJECT_INVITATION_EXPIRY_POLL_INTERVAL must be positive")
	}
	if strings.TrimSpace(config.Repo.StorageRoot) == "" {
		return fmt.Errorf("REPO_STORAGE_ROOT must not be empty")
	}
	if strings.TrimSpace(config.Repo.AskPassPath) == "" {
		return fmt.Errorf("REPO_ASKPASS_PATH must not be empty")
	}
	if config.Repo.MaxConcurrentGit < 1 {
		return fmt.Errorf("REPO_MAX_CONCURRENT_GIT must be positive")
	}
	if config.Repo.MaxTextBytes < 1 ||
		config.Repo.MaxTextBytes > 64*1024*1024 {
		return fmt.Errorf("REPO_MAX_TEXT_BYTES must be between 1 byte and 64 MiB")
	}
	if config.Repo.CommandTimeout <= 0 ||
		config.Repo.CloneTimeout <= 0 ||
		config.Repo.SyncPollInterval <= 0 ||
		config.Repo.SyncLease <= 0 ||
		config.Repo.CheckoutTTL <= 0 ||
		config.Repo.DisconnectGrace <= 0 {
		return fmt.Errorf("Repo durations must be positive")
	}
	for _, root := range config.Repo.LocalAllowedRoots {
		if strings.TrimSpace(root) == "" {
			return fmt.Errorf("REPO_LOCAL_ALLOWED_ROOTS contains an empty path")
		}
	}
	if len(config.Settings.EncryptionKey) < 32 {
		return fmt.Errorf("SETTINGS_ENCRYPTION_KEY must contain at least 32 characters")
	}
	if strings.TrimSpace(config.Version) == "" || len(config.Version) > 100 {
		return fmt.Errorf("MMDASH_VERSION must contain 1 to 100 characters")
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

func int64OrDefault(lookup LookupEnv, key string, fallback int64) int64 {
	value, ok := lookup(key)
	if !ok || value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return parsed
}

func loadAgentConnector(
	lookup LookupEnv,
	prefix string,
	defaultPorts []int,
) (AgentConnectorConfig, error) {
	allowLoopback, err := boolOrDefault(lookup, prefix+"_ALLOW_LOOPBACK", false)
	if err != nil {
		return AgentConnectorConfig{}, err
	}
	allowPrivate, err := boolOrDefault(lookup, prefix+"_ALLOW_PRIVATE", false)
	if err != nil {
		return AgentConnectorConfig{}, err
	}
	ports, err := intListOrDefault(lookup, prefix+"_ALLOWED_PORTS", defaultPorts)
	if err != nil {
		return AgentConnectorConfig{}, err
	}
	return AgentConnectorConfig{
		AllowLoopback: allowLoopback,
		AllowPrivate:  allowPrivate,
		AllowedPorts:  ports,
		ConnectTimeout: durationOrDefault(
			lookup, prefix+"_CONNECT_TIMEOUT", 5*time.Second,
		),
		MaxRedirects: intOrDefault(lookup, prefix+"_MAX_REDIRECTS", 3),
		MaxResponseBytes: int64OrDefault(
			lookup, prefix+"_MAX_RESPONSE_BYTES", 4*1024*1024,
		),
		RequestTimeout: durationOrDefault(
			lookup, prefix+"_REQUEST_TIMEOUT", 30*time.Second,
		),
		ResponseHeaderTimeout: durationOrDefault(
			lookup, prefix+"_RESPONSE_HEADER_TIMEOUT", 10*time.Second,
		),
	}, nil
}

func boolOrDefault(lookup LookupEnv, key string, fallback bool) (bool, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return parsed, nil
}

func intListOrDefault(
	lookup LookupEnv,
	key string,
	fallback []int,
) ([]int, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return append([]int(nil), fallback...), nil
	}
	parts := strings.Split(value, ",")
	result := make([]int, 0, len(parts))
	seen := map[int]bool{}
	for _, part := range parts {
		port, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("%s must contain comma-separated TCP ports", key)
		}
		if !seen[port] {
			seen[port] = true
			result = append(result, port)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%s must contain at least one TCP port", key)
	}
	return result, nil
}

func validateAgentGatewayURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("AGENT_MCP_GATEWAY_URL must be an HTTP(S) URL without credentials, query, or fragment")
	}
	return nil
}

func validateAgentConnector(prefix string, connector AgentConnectorConfig) error {
	if len(connector.AllowedPorts) == 0 {
		return fmt.Errorf("%s_ALLOWED_PORTS must contain at least one TCP port", prefix)
	}
	for _, port := range connector.AllowedPorts {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%s_ALLOWED_PORTS contains an invalid TCP port", prefix)
		}
	}
	if connector.ConnectTimeout <= 0 || connector.ResponseHeaderTimeout <= 0 ||
		connector.RequestTimeout <= 0 {
		return fmt.Errorf("%s connector timeouts must be positive", prefix)
	}
	if connector.MaxRedirects < 1 || connector.MaxRedirects > 10 {
		return fmt.Errorf("%s_MAX_REDIRECTS must be between 1 and 10", prefix)
	}
	if connector.MaxResponseBytes < 1024 || connector.MaxResponseBytes > 64*1024*1024 {
		return fmt.Errorf("%s_MAX_RESPONSE_BYTES must be between 1 KiB and 64 MiB", prefix)
	}
	return nil
}

func pathList(lookup LookupEnv, key string) []string {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.FieldsFunc(value, func(character rune) bool {
		return character == rune(os.PathListSeparator) || character == ','
	})
	roots := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			roots = append(roots, trimmed)
		}
	}
	return roots
}
