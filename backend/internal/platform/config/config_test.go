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
	if config.Project.InvitationExpiryBatchSize != 100 ||
		config.Project.InvitationExpiryPollInterval != 30*time.Second {
		t.Fatalf("unexpected Project invitation expiry defaults: %+v", config.Project)
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
		config.Repo.CommitLease != 90*time.Second ||
		config.Repo.WriteTimeout != 45*time.Second ||
		config.Repo.ReconcileInterval != 15*time.Minute ||
		config.Repo.MaxTextBytes != 1024*1024 {
		t.Fatalf("unexpected Repo defaults: %+v", config.Repo)
	}
	if config.Version != "0.1.0" {
		t.Fatalf("unexpected service version: %s", config.Version)
	}
	if config.InternalURL != "http://localhost:8080" {
		t.Fatalf("unexpected internal Core URL: %s", config.InternalURL)
	}
	if config.Notion.OAuthRedirectURI != "http://localhost:3000/api/integrations/notion/oauth/callback" {
		t.Fatalf("unexpected Notion OAuth redirect URI: %s", config.Notion.OAuthRedirectURI)
	}
	if config.Agent.GatewayURL != "http://localhost:3002/mcp" ||
		config.Agent.Runtime.AllowLoopback || config.Agent.Runtime.AllowPrivate ||
		config.Agent.Management.AllowLoopback || config.Agent.Management.AllowPrivate ||
		config.Agent.Runtime.ConnectTimeout != 5*time.Second ||
		config.Agent.ManagementMinimumInterval != 250*time.Millisecond ||
		config.Agent.Management.MaxResponseBytes != 4*1024*1024 {
		t.Fatalf("unexpected Agent connector defaults: %+v", config.Agent)
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

	_, err = Load(mapLookup(map[string]string{
		"DATABASE_URL":              "postgres://localhost/mmdash",
		"NOTION_OAUTH_CLIENT_ID":    "client-id-without-secret",
		"OBJECT_STORAGE_ACCESS_KEY": "access",
		"OBJECT_STORAGE_ENDPOINT":   "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY": "secret",
	}))
	if err == nil {
		t.Fatal("expected incomplete Notion OAuth credentials to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"AGENT_RUNTIME_ALLOW_PRIVATE": "sometimes",
		"DATABASE_URL":                "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY":   "access",
		"OBJECT_STORAGE_ENDPOINT":     "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY":   "secret",
	}))
	if err == nil {
		t.Fatal("expected invalid Agent private-network policy to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"AGENT_MANAGEMENT_ALLOWED_PORTS": "443,70000",
		"DATABASE_URL":                   "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY":      "access",
		"OBJECT_STORAGE_ENDPOINT":        "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY":      "secret",
	}))
	if err == nil {
		t.Fatal("expected invalid Agent management port policy to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"AGENT_MANAGEMENT_MINIMUM_INTERVAL": "0s",
		"DATABASE_URL":                      "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY":         "access",
		"OBJECT_STORAGE_ENDPOINT":           "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY":         "secret",
	}))
	if err == nil {
		t.Fatal("expected non-positive Agent management interval to fail")
	}

	_, err = Load(mapLookup(map[string]string{
		"AGENT_MCP_GATEWAY_URL":     "https://user:secret@example.test/mcp",
		"DATABASE_URL":              "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY": "access",
		"OBJECT_STORAGE_ENDPOINT":   "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY": "secret",
	}))
	if err == nil {
		t.Fatal("expected credential-bearing Agent Gateway URL to fail")
	}
}

func TestLoadAppliesExplicitAgentConnectorPolicy(t *testing.T) {
	loaded, err := Load(mapLookup(map[string]string{
		"AGENT_MCP_GATEWAY_URL":                 "https://mmdash.example/mcp",
		"AGENT_RUNTIME_ALLOW_LOOPBACK":          "true",
		"AGENT_RUNTIME_ALLOW_PRIVATE":           "true",
		"AGENT_RUNTIME_ALLOWED_PORTS":           "443,8642,443",
		"AGENT_RUNTIME_CONNECT_TIMEOUT":         "2s",
		"AGENT_RUNTIME_MAX_REDIRECTS":           "2",
		"AGENT_RUNTIME_MAX_RESPONSE_BYTES":      "2097152",
		"AGENT_RUNTIME_REQUEST_TIMEOUT":         "20s",
		"AGENT_RUNTIME_RESPONSE_HEADER_TIMEOUT": "4s",
		"AGENT_MANAGEMENT_ALLOWED_PORTS":        "443,9119",
		"AGENT_MANAGEMENT_MINIMUM_INTERVAL":     "750ms",
		"DATABASE_URL":                          "postgres://localhost/mmdash",
		"OBJECT_STORAGE_ACCESS_KEY":             "access",
		"OBJECT_STORAGE_ENDPOINT":               "http://localhost:9000",
		"OBJECT_STORAGE_SECRET_KEY":             "secret",
	}))
	if err != nil {
		t.Fatalf("load explicit Agent policy: %v", err)
	}
	if !loaded.Agent.Runtime.AllowLoopback || !loaded.Agent.Runtime.AllowPrivate ||
		len(loaded.Agent.Runtime.AllowedPorts) != 2 ||
		loaded.Agent.Runtime.ConnectTimeout != 2*time.Second ||
		loaded.Agent.Runtime.RequestTimeout != 20*time.Second ||
		loaded.Agent.Runtime.ResponseHeaderTimeout != 4*time.Second ||
		loaded.Agent.Runtime.MaxRedirects != 2 ||
		loaded.Agent.Runtime.MaxResponseBytes != 2*1024*1024 ||
		loaded.Agent.ManagementMinimumInterval != 750*time.Millisecond {
		t.Fatalf("unexpected explicit Agent runtime policy: %+v", loaded.Agent.Runtime)
	}
}

func mapLookup(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
