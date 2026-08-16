package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
	"github.com/mmdash/mmdash/box/gateway"
	e2bruntime "github.com/mmdash/mmdash/box/runtimes/e2b"
	localdocker "github.com/mmdash/mmdash/box/runtimes/local-docker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		panic(err)
	}
}

func run(ctx context.Context) error {
	root := getenv("MMDASH_BOX_DATA_ROOT", filepath.Join(os.TempDir(), "mmdash-box"))
	limits := contracts.ResourceLimits{
		CPUMillis: int64Env("MMDASH_BOX_CPU_MILLIS", 1000), MemoryBytes: int64Env("MMDASH_BOX_MEMORY_BYTES", 1<<30),
		TimeoutSecond: int(int64Env("MMDASH_BOX_TIMEOUT_SECONDS", 3600)), DiskBytes: int64Env("MMDASH_BOX_DISK_BYTES", 10<<30),
		PIDs: int(int64Env("MMDASH_BOX_PIDS", 256)), Network: getenv("MMDASH_BOX_NETWORK", "disabled"),
	}
	client := gateway.HTTPClient{BaseURL: getenv("MMDASH_CORE_URL", "http://localhost:8080")}
	runtimes, runtimeFactory, probeErrors, err := configuredRuntimes(ctx, limits, probeRuntimeAdapter)
	if err != nil {
		return err
	}
	for _, probeErr := range probeErrors {
		_, _ = fmt.Fprintf(os.Stderr, "Box Runtime unavailable: %s\n", probeErr)
	}
	config := gateway.Config{
		InstallationID: os.Getenv("MMDASH_BOX_INSTALLATION_ID"), Name: getenv("MMDASH_BOX_NAME", "local-box"), Version: getenv("MMDASH_BOX_VERSION", "dev"),
		BoxID: os.Getenv("MMDASH_BOX_ID"), BoxToken: os.Getenv("MMDASH_BOX_TOKEN"),
		StatePath: getenv("MMDASH_BOX_STATE_PATH", filepath.Join(root, "state.json")), OutputRoot: filepath.Join(root, "outputs"),
		HeartbeatInterval: durationEnv("MMDASH_BOX_HEARTBEAT_INTERVAL", 15*time.Second), ClaimWait: durationEnv("MMDASH_BOX_CLAIM_WAIT", 60*time.Second), RetryDelay: durationEnv("MMDASH_BOX_RETRY_DELAY", 2*time.Second), MaxConcurrent: int(int64Env("MMDASH_BOX_MAX_CONCURRENT", 1)),
		LogBudgetBytes: int64Env("MMDASH_BOX_LOG_BUDGET_BYTES", 256<<20),
		Capabilities:   []contracts.Capability{{Name: "sandbox", Version: "2", Features: []string{"execution-bundle", "durable-log-spool", "offline-resume"}}},
		Runtimes:       runtimes, Limits: limits,
	}
	var workspace gateway.WorkspaceProvider = gateway.TransferWorkspace{Root: filepath.Join(root, "sources")}
	if staticRoot := strings.TrimSpace(os.Getenv("MMDASH_BOX_WORKSPACE")); staticRoot != "" {
		workspace = gateway.StaticWorkspace{Root: staticRoot, Commit: os.Getenv("MMDASH_BOX_WORKSPACE_COMMIT")}
	}
	runner := &gateway.Gateway{Client: client, Config: config, Workspace: workspace, Runtime: runtimeFactory}
	return runner.Run(ctx)
}

type runtimeProbe func(context.Context, sandbox.Runtime) error

type runtimeCandidate struct {
	descriptor contracts.Runtime
	runtime    sandbox.Runtime
}

func probeRuntimeAdapter(ctx context.Context, runtime sandbox.Runtime) error {
	prober, ok := runtime.(sandbox.Prober)
	if !ok {
		return errors.New("Runtime adapter does not implement lifecycle probing")
	}
	return prober.Probe(ctx)
}

func configuredRuntimes(ctx context.Context, limits contracts.ResourceLimits, probe runtimeProbe) ([]contracts.Runtime, gateway.RuntimeFactory, []error, error) {
	localImage := getenv("MMDASH_BOX_LOCAL_IMAGE", "mmdash/sandbox:latest")
	localUser := localDockerUser()
	candidates := []runtimeCandidate{{
		descriptor: contracts.Runtime{Name: "local-docker", Version: "1", Image: localImage},
		runtime:    localdocker.Runtime{Image: localImage, User: localUser},
	}}
	if apiKey := strings.TrimSpace(os.Getenv("E2B_API_KEY")); apiKey != "" {
		domain := getenv("E2B_DOMAIN", "e2b.app")
		provider, err := e2bruntime.NewClient(e2bruntime.Config{
			APIKey:     apiKey,
			Domain:     domain,
			APIURL:     getenv("E2B_API_URL", "https://api."+domain),
			SandboxURL: strings.TrimSpace(os.Getenv("E2B_SANDBOX_URL")),
			User:       getenv("MMDASH_E2B_USER", "user"), AdminUser: getenv("MMDASH_E2B_ADMIN_USER", "root"),
			RequestTimeout: durationEnv("MMDASH_E2B_REQUEST_TIMEOUT", 60*time.Second),
			CleanupTimeout: durationEnv("MMDASH_E2B_CLEANUP_TIMEOUT", 30*time.Second),
			SandboxGrace:   durationEnv("MMDASH_E2B_SANDBOX_GRACE", 60*time.Second),
		})
		if err != nil {
			return nil, nil, nil, err
		}
		template := getenv("MMDASH_E2B_TEMPLATE", "base")
		candidates = append(candidates, runtimeCandidate{
			descriptor: contracts.Runtime{Name: "e2b", Version: "1", Image: template},
			runtime:    e2bruntime.Runtime{Template: template, Client: provider},
		})
	}
	if probe == nil {
		probe = probeRuntimeAdapter
	}
	reported := make([]contracts.Runtime, 0, len(candidates))
	available := make(map[string]sandbox.Runtime, len(candidates))
	probeErrors := make([]error, 0, len(candidates))
	for _, candidate := range candidates {
		if err := probe(ctx, candidate.runtime); err != nil {
			probeErrors = append(probeErrors, fmt.Errorf("%s: %w", candidate.descriptor.Name, err))
			continue
		}
		reported = append(reported, candidate.descriptor)
		available[candidate.descriptor.Name] = candidate.runtime
	}
	if len(reported) == 0 {
		return nil, nil, probeErrors, errors.Join(append([]error{errors.New("no usable Sandbox Runtime was detected")}, probeErrors...)...)
	}
	factory := func(spec contracts.RunSpec) (sandbox.Runtime, error) {
		if err := withinBoxLimits(spec.Limits, limits); err != nil {
			return nil, err
		}
		runtime := available[spec.Runtime]
		if runtime == nil {
			return nil, errors.New("unsupported Sandbox runtime")
		}
		return runtime, nil
	}
	return reported, factory, probeErrors, nil
}

func localDockerUser() string {
	if configured := strings.TrimSpace(os.Getenv("MMDASH_BOX_LOCAL_USER")); configured != "" {
		return configured
	}
	current, err := user.Current()
	if err != nil || !decimal(current.Uid) || !decimal(current.Gid) {
		return ""
	}
	return current.Uid + ":" + current.Gid
}

func decimal(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func withinBoxLimits(requested, available contracts.ResourceLimits) error {
	if requested.CPUMillis > available.CPUMillis || requested.MemoryBytes > available.MemoryBytes ||
		requested.TimeoutSecond > available.TimeoutSecond || requested.DiskBytes > available.DiskBytes || requested.PIDs > available.PIDs {
		return errors.New("frozen Sandbox limits exceed the Box capacity")
	}
	networkRank := map[string]int{"disabled": 0, "restricted": 1, "enabled": 2}
	requestedRank, requestedOK := networkRank[requested.Network]
	availableRank, availableOK := networkRank[available.Network]
	if !requestedOK || !availableOK || requestedRank > availableRank {
		return errors.New("frozen Sandbox network policy exceeds the Box capability")
	}
	return nil
}

func getenv(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
func int64Env(name string, fallback int64) int64 {
	value, err := strconv.ParseInt(os.Getenv(name), 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
func durationEnv(name string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
