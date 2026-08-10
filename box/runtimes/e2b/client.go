package e2b

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
)

const (
	defaultDomain                  = "e2b.app"
	defaultEnvdPort                = 49983
	defaultRequestTimeout          = 60 * time.Second
	defaultCleanupTimeout          = 30 * time.Second
	defaultSandboxGrace            = 60 * time.Second
	defaultMaxWorkspaceFiles       = 100_000
	defaultMaxWorkspaceBytes       = int64(10 << 30)
	defaultMaxOutputFiles          = 10_000
	minimumOctetEnvdVersion        = "0.5.7"
	providerUserAgent              = "mmdash-box/e2b-v1"
	allIPv4Traffic                 = "0.0.0.0/0"
	bytesPerMiB              int64 = 1 << 20
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
var sandboxIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

var stableSandboxDomains = map[string]struct{}{
	"e2b.app":         {},
	"e2b.dev":         {},
	"e2b.pro":         {},
	"e2b-staging.dev": {},
}

// Config contains only Box-local provider configuration. None of these values
// are serialized into the Core task contract.
type Config struct {
	APIKey            string
	Domain            string
	APIURL            string
	SandboxURL        string
	User              string
	AdminUser         string
	UserAgent         string
	HTTPClient        *http.Client
	RequestTimeout    time.Duration
	CleanupTimeout    time.Duration
	SandboxGrace      time.Duration
	MaxWorkspaceFiles int
	MaxWorkspaceBytes int64
	MaxOutputFiles    int
}

// ProviderClient implements the official E2B Platform REST and Envd HTTP /
// Connect APIs. A task ID is mapped to its provider sandbox only in memory so
// provider identifiers never escape the Runtime boundary.
type ProviderClient struct {
	apiKey            string
	domain            string
	apiURL            string
	sandboxURL        string
	user              string
	adminUser         string
	userAgent         string
	httpClient        *http.Client
	requestTimeout    time.Duration
	cleanupTimeout    time.Duration
	sandboxGrace      time.Duration
	maxWorkspaceFiles int
	maxWorkspaceBytes int64
	maxOutputFiles    int

	mu       sync.Mutex
	sessions map[string]*executionSession
}

type executionSession struct {
	sandboxID       string
	sandboxURL      string
	envdVersion     string
	envdAccessToken string
	pid             int
	canceled        bool
}

type sandboxConnection struct {
	SandboxID       string
	SandboxURL      string
	EnvdVersion     string
	EnvdAccessToken string
}

func NewClient(config Config) (*ProviderClient, error) {
	config.APIKey = strings.TrimSpace(config.APIKey)
	if config.APIKey == "" {
		return nil, errors.New("E2B API key is required")
	}
	config.Domain = strings.TrimSpace(config.Domain)
	if config.Domain == "" {
		config.Domain = defaultDomain
	}
	if _, err := providerDomain(config.Domain); err != nil {
		return nil, fmt.Errorf("invalid E2B domain: %w", err)
	}
	if config.APIURL == "" {
		config.APIURL = "https://api." + config.Domain
	}
	apiURL, err := providerBaseURL(config.APIURL)
	if err != nil {
		return nil, fmt.Errorf("invalid E2B API URL: %w", err)
	}
	sandboxURL := ""
	if strings.TrimSpace(config.SandboxURL) != "" {
		sandboxURL, err = providerBaseURL(config.SandboxURL)
		if err != nil {
			return nil, fmt.Errorf("invalid E2B Sandbox URL: %w", err)
		}
	}
	if config.User == "" {
		config.User = "user"
	}
	if config.AdminUser == "" {
		config.AdminUser = "root"
	}
	if !usernamePattern.MatchString(config.User) || !usernamePattern.MatchString(config.AdminUser) {
		return nil, errors.New("E2B user names contain unsupported characters")
	}
	if config.UserAgent == "" {
		config.UserAgent = providerUserAgent
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{}
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = defaultRequestTimeout
	}
	if config.CleanupTimeout <= 0 {
		config.CleanupTimeout = defaultCleanupTimeout
	}
	if config.SandboxGrace <= 0 {
		config.SandboxGrace = defaultSandboxGrace
	}
	if config.MaxWorkspaceFiles <= 0 {
		config.MaxWorkspaceFiles = defaultMaxWorkspaceFiles
	}
	if config.MaxWorkspaceBytes <= 0 {
		config.MaxWorkspaceBytes = defaultMaxWorkspaceBytes
	}
	if config.MaxOutputFiles <= 0 {
		config.MaxOutputFiles = defaultMaxOutputFiles
	}
	return &ProviderClient{
		apiKey: config.APIKey, domain: config.Domain, apiURL: apiURL, sandboxURL: sandboxURL,
		user: config.User, adminUser: config.AdminUser, userAgent: config.UserAgent,
		httpClient: config.HTTPClient, requestTimeout: config.RequestTimeout,
		cleanupTimeout: config.CleanupTimeout, sandboxGrace: config.SandboxGrace,
		maxWorkspaceFiles: config.MaxWorkspaceFiles, maxWorkspaceBytes: config.MaxWorkspaceBytes,
		maxOutputFiles: config.MaxOutputFiles, sessions: map[string]*executionSession{},
	}, nil
}

func providerBaseURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("provider URL must be an absolute HTTP(S) origin")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("provider URL must use HTTP or HTTPS")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (client *ProviderClient) Run(ctx context.Context, template string, request sandbox.RunRequest) (result sandbox.RunResult, runErr error) {
	if strings.TrimSpace(template) == "" || request.Spec.Validate() != nil {
		return sandbox.RunResult{}, errors.New("invalid E2B run configuration")
	}
	command, err := sandbox.EntrypointCommand(request.Spec.Entrypoint)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	startedAt := time.Now()
	connection, err := client.createSandbox(ctx, strings.TrimSpace(template), request)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	session := &executionSession{sandboxID: connection.SandboxID, sandboxURL: connection.SandboxURL, envdVersion: connection.EnvdVersion, envdAccessToken: connection.EnvdAccessToken}
	if !client.register(request.ID, session) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), client.cleanupTimeout)
		defer cancel()
		_ = client.killSandbox(cleanupCtx, connection.SandboxID)
		return sandbox.RunResult{}, errors.New("an E2B execution already exists for this task")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), client.cleanupTimeout)
		defer cancel()
		cleanupErr := client.killSandbox(cleanupCtx, connection.SandboxID)
		client.unregister(request.ID, session)
		if cleanupErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("destroy E2B sandbox: %w", cleanupErr))
		}
	}()

	info, err := client.getSandbox(ctx, connection.SandboxID)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	if err := validateProviderCapacity(info, request); err != nil {
		return sandbox.RunResult{}, err
	}
	if !versionAtLeast(connection.EnvdVersion, minimumOctetEnvdVersion) {
		return sandbox.RunResult{}, fmt.Errorf("E2B template envd %s is too old; version %s or newer is required", connection.EnvdVersion, minimumOctetEnvdVersion)
	}
	if err := client.prepareWorkspace(ctx, session, request); err != nil {
		return sandbox.RunResult{}, err
	}

	executionCtx, cancel := context.WithTimeout(ctx, time.Duration(request.Spec.Limits.TimeoutSecond)*time.Second)
	defer cancel()
	limitedCommand := processLimitCommand(command, request.Spec.Limits.PIDs)
	exitCode, err := client.runProcess(executionCtx, session, client.user, limitedCommand, request.Spec.Environment, request.Stdout, request.Stderr, func(pid int) {
		client.setPID(request.ID, session, pid)
	})
	if errors.Is(executionCtx.Err(), context.DeadlineExceeded) {
		client.bestEffortSignal(session, signalKill)
		return sandbox.RunResult{TimedOut: true, ResourceUsage: durationUsage(startedAt)}, nil
	}
	if errors.Is(executionCtx.Err(), context.Canceled) || client.wasCanceled(request.ID, session) {
		return sandbox.RunResult{Canceled: true, ResourceUsage: durationUsage(startedAt)}, nil
	}
	if err != nil {
		return sandbox.RunResult{}, err
	}
	if err := client.collectOutput(ctx, session, request.OutputDir, request.Spec.Limits.DiskBytes); err != nil {
		return sandbox.RunResult{}, err
	}
	usage := durationUsage(startedAt)
	if metrics, metricsErr := client.getSandboxMetrics(ctx, connection.SandboxID, startedAt); metricsErr == nil {
		mergeMetrics(usage, metrics)
	}
	return sandbox.RunResult{ExitCode: exitCode, ResourceUsage: usage}, nil
}

func (client *ProviderClient) Cancel(ctx context.Context, taskID string) error {
	return client.terminate(ctx, taskID, signalTerminate, true)
}

func (client *ProviderClient) Destroy(ctx context.Context, taskID string) error {
	return client.terminate(ctx, taskID, signalKill, true)
}

func (client *ProviderClient) terminate(ctx context.Context, taskID, signal string, canceled bool) error {
	client.mu.Lock()
	session := client.sessions[taskID]
	if session == nil {
		client.mu.Unlock()
		return nil
	}
	if canceled {
		session.canceled = true
	}
	copy := *session
	client.mu.Unlock()
	var signalErr error
	if copy.pid > 0 {
		signalErr = client.sendSignal(ctx, &copy, signal)
	}
	killErr := client.killSandbox(ctx, copy.sandboxID)
	return errors.Join(signalErr, killErr)
}

func (client *ProviderClient) register(taskID string, session *executionSession) bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	if taskID == "" || client.sessions[taskID] != nil {
		return false
	}
	client.sessions[taskID] = session
	return true
}

func (client *ProviderClient) unregister(taskID string, session *executionSession) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.sessions[taskID] == session {
		delete(client.sessions, taskID)
	}
}

func (client *ProviderClient) setPID(taskID string, session *executionSession, pid int) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.sessions[taskID] == session {
		session.pid = pid
	}
}

func (client *ProviderClient) wasCanceled(taskID string, session *executionSession) bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.sessions[taskID] == session && session.canceled
}

func (client *ProviderClient) bestEffortSignal(session *executionSession, signal string) {
	client.mu.Lock()
	copy := *session
	client.mu.Unlock()
	if copy.pid == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), client.cleanupTimeout)
	defer cancel()
	_ = client.sendSignal(ctx, &copy, signal)
}

func processLimitCommand(command []string, pids int) []string {
	limit := fmt.Sprintf("--nproc=%d:%d", pids, pids)
	return append([]string{"/usr/bin/prlimit", limit, "--"}, command...)
}

func validateProviderCapacity(info sandboxInfo, request sandbox.RunRequest) error {
	if int64(info.CPUCount)*1000 < request.Spec.Limits.CPUMillis {
		return fmt.Errorf("E2B template CPU capacity is below the frozen run limit: available=%d required=%d millicores", int64(info.CPUCount)*1000, request.Spec.Limits.CPUMillis)
	}
	if int64(info.MemoryMB)*bytesPerMiB < request.Spec.Limits.MemoryBytes {
		return fmt.Errorf("E2B template memory capacity is below the frozen run limit: available=%d required=%d bytes", int64(info.MemoryMB)*bytesPerMiB, request.Spec.Limits.MemoryBytes)
	}
	if int64(info.DiskSizeMB)*bytesPerMiB < request.Spec.Limits.DiskBytes {
		return fmt.Errorf("E2B template disk capacity is below the frozen run limit: available=%d required=%d bytes", int64(info.DiskSizeMB)*bytesPerMiB, request.Spec.Limits.DiskBytes)
	}
	return nil
}

func durationUsage(startedAt time.Time) map[string]interface{} {
	return map[string]interface{}{"duration_ms": time.Since(startedAt).Milliseconds()}
}

func mergeMetrics(usage map[string]interface{}, metrics []sandboxMetric) {
	var peakCPU float64
	var peakMemory, peakDisk int64
	for _, metric := range metrics {
		if metric.CPUUsedPct > peakCPU {
			peakCPU = metric.CPUUsedPct
		}
		if metric.MemUsed > peakMemory {
			peakMemory = metric.MemUsed
		}
		if metric.DiskUsed > peakDisk {
			peakDisk = metric.DiskUsed
		}
	}
	if len(metrics) > 0 {
		usage["cpu_used_percent_peak"] = peakCPU
		usage["memory_used_bytes_peak"] = peakMemory
		usage["disk_used_bytes_peak"] = peakDisk
	}
}

func writeOutput(writer io.Writer, data []byte) error {
	if writer == nil {
		writer = io.Discard
	}
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}
