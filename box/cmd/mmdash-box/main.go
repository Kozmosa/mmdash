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
	config := gateway.Config{
		ProjectID: os.Getenv("MMDASH_BOX_PROJECT_ID"), Name: getenv("MMDASH_BOX_NAME", "local-box"), Version: getenv("MMDASH_BOX_VERSION", "dev"),
		RegistrationToken: os.Getenv("MMDASH_BOX_REGISTRATION_TOKEN"), BoxID: os.Getenv("MMDASH_BOX_ID"), BoxToken: os.Getenv("MMDASH_BOX_TOKEN"),
		StatePath: getenv("MMDASH_BOX_STATE_PATH", filepath.Join(root, "state.json")), WorkspaceRoot: os.Getenv("MMDASH_BOX_WORKSPACE"), OutputRoot: filepath.Join(root, "outputs"),
		HeartbeatInterval: durationEnv("MMDASH_BOX_HEARTBEAT_INTERVAL", 15*time.Second), ClaimInterval: durationEnv("MMDASH_BOX_CLAIM_INTERVAL", 2*time.Second), Lease: durationEnv("MMDASH_BOX_LEASE", time.Minute), MaxConcurrent: int(int64Env("MMDASH_BOX_MAX_CONCURRENT", 1)),
		Capabilities: []contracts.Capability{{Name: "sandbox", Version: "1", Features: []string{"manifest", "artifact.zip"}}},
		Runtimes:     []contracts.Runtime{{Name: "local-docker", Version: "1", Image: getenv("MMDASH_BOX_LOCAL_IMAGE", "mmdash/sandbox:latest")}}, Limits: limits,
	}
	workspace := gateway.StaticWorkspace{Root: config.WorkspaceRoot, Commit: os.Getenv("MMDASH_BOX_WORKSPACE_COMMIT")}
	runner := &gateway.Gateway{Client: client, Config: config, Workspace: workspace, Runtime: func(spec contracts.RunSpec) (sandbox.Runtime, error) {
		if spec.Runtime != "local-docker" {
			return nil, errors.New("E2B runtime requires an explicitly configured provider adapter")
		}
		return localdocker.Runtime{Image: getenv("MMDASH_BOX_LOCAL_IMAGE", "mmdash/sandbox:latest")}, nil
	}}
	return runner.Run(ctx)
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
