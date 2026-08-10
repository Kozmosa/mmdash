package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
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
	if err := run(ctx); err != nil {
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
	runtimes, runtimeFactory, err := configuredRuntimes(limits)
	if err != nil {
		return err
	}
	config := gateway.Config{
		ProjectID: os.Getenv("MMDASH_BOX_PROJECT_ID"), Name: getenv("MMDASH_BOX_NAME", "local-box"), Version: getenv("MMDASH_BOX_VERSION", "dev"),
		RegistrationToken: os.Getenv("MMDASH_BOX_REGISTRATION_TOKEN"), BoxID: os.Getenv("MMDASH_BOX_ID"), BoxToken: os.Getenv("MMDASH_BOX_TOKEN"),
		StatePath: getenv("MMDASH_BOX_STATE_PATH", filepath.Join(root, "state.json")), WorkspaceRoot: os.Getenv("MMDASH_BOX_WORKSPACE"), OutputRoot: filepath.Join(root, "outputs"),
		HeartbeatInterval: durationEnv("MMDASH_BOX_HEARTBEAT_INTERVAL", 15*time.Second), ClaimInterval: durationEnv("MMDASH_BOX_CLAIM_INTERVAL", 2*time.Second), Lease: durationEnv("MMDASH_BOX_LEASE", time.Minute), MaxConcurrent: int(int64Env("MMDASH_BOX_MAX_CONCURRENT", 1)),
		Capabilities: []contracts.Capability{{Name: "sandbox", Version: "1", Features: []string{"manifest", "artifact.zip"}}},
		Runtimes:     runtimes, Limits: limits,
	}
	workspace := gateway.StaticWorkspace{Root: config.WorkspaceRoot, Commit: os.Getenv("MMDASH_BOX_WORKSPACE_COMMIT")}
	runner := &gateway.Gateway{Client: client, Config: config, Workspace: workspace, Runtime: runtimeFactory}
	return runner.Run(ctx)
}

func configuredRuntimes(limits contracts.ResourceLimits) ([]contracts.Runtime, gateway.RuntimeFactory, error) {
	localImage := getenv("MMDASH_BOX_LOCAL_IMAGE", "mmdash/sandbox:latest")
	reported := []contracts.Runtime{{Name: "local-docker", Version: "1", Image: localImage}}
	var remote sandbox.Runtime
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
			return nil, nil, err
		}
		template := getenv("MMDASH_E2B_TEMPLATE", "base")
		remote = e2bruntime.Runtime{Template: template, Client: provider}
		reported = append(reported, contracts.Runtime{Name: "e2b", Version: "1", Image: template})
	}
	factory := func(spec contracts.RunSpec) (sandbox.Runtime, error) {
		if err := withinBoxLimits(spec.Limits, limits); err != nil {
			return nil, err
		}
		switch spec.Runtime {
		case "local-docker":
			return localdocker.Runtime{Image: localImage}, nil
		case "e2b":
			if remote == nil {
				return nil, errors.New("E2B runtime is not configured")
			}
			return remote, nil
		default:
			return nil, errors.New("unsupported Sandbox runtime")
		}
	}
	return reported, factory, nil
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
