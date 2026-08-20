// Package localdocker runs a fixed Sandbox command in a durable named
// container. The container survives a Gateway process restart and is attached
// again by task ID; it is removed only after a terminal outcome is recorded.
package localdocker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
)

var taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type Runtime struct {
	Image        string
	User         string
	Environment  *EnvironmentManager
	mu           sync.Mutex
	environments map[string]EnvironmentResult
}

func (runtime *Runtime) Run(ctx context.Context, request sandbox.RunRequest) (sandbox.RunResult, error) {
	if runtime == nil || runtime.Image == "" || !taskIDPattern.MatchString(request.ID) {
		return sandbox.RunResult{}, errors.New("local Docker image and task ID are required")
	}
	if request.Spec.Validate() != nil {
		return sandbox.RunResult{}, errors.New("invalid frozen run specification")
	}
	command, err := sandbox.EntrypointCommand(request.Spec.Entrypoint)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	if err := ensureOutput(request.OutputDir); err != nil {
		return sandbox.RunResult{}, err
	}
	image := runtime.Image
	environment := EnvironmentResult{Image: image, ImageID: image, BuilderVersion: EnvironmentBuilderVersion}
	if runtime.Environment != nil {
		environment = runtime.environment(request.ID)
		if environment.Image == "" {
			return sandbox.RunResult{}, errors.New("Local Docker environment was not prepared")
		}
		image = environment.Image
	}
	name := containerName(request.ID)
	state, exitCode, exists, err := inspectContainer(ctx, name)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	if exists && state == "exited" {
		return sandbox.RunResult{ExitCode: exitCode, ResourceUsage: environmentFields(environment)}, nil
	}
	created := false
	if !exists {
		args, err := buildArgs(image, runtime.User, request, command)
		if err != nil {
			return sandbox.RunResult{}, err
		}
		if output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
			return sandbox.RunResult{}, fmt.Errorf("create local Docker sandbox: %w: %s", err, boundedDockerOutput(output))
		}
		created = true
	}
	executionCtx, cancel := context.WithTimeout(ctx, time.Duration(request.Spec.Limits.TimeoutSecond)*time.Second)
	defer cancel()
	args := []string{"attach", "--no-stdin", name}
	if created {
		args = []string{"start", "--attach", name}
	}
	process := exec.CommandContext(executionCtx, "docker", args...)
	process.Stdout = request.Stdout
	process.Stderr = request.Stderr
	runErr := process.Run()
	if errors.Is(executionCtx.Err(), context.DeadlineExceeded) {
		_ = stopContainer(context.Background(), name, 10*time.Second)
		return sandbox.RunResult{TimedOut: true, ResourceUsage: environmentFields(environment)}, nil
	}
	if errors.Is(executionCtx.Err(), context.Canceled) {
		status, code, stillExists, inspectErr := inspectContainer(context.Background(), name)
		if inspectErr == nil && stillExists && status == "exited" {
			return sandbox.RunResult{ExitCode: code, Canceled: true, ResourceUsage: environmentFields(environment)}, nil
		}
		return sandbox.RunResult{}, sandbox.ErrExecutionDetached
	}
	status, code, stillExists, inspectErr := inspectContainer(ctx, name)
	if inspectErr != nil {
		return sandbox.RunResult{}, inspectErr
	}
	if !stillExists || status != "exited" {
		if runErr != nil {
			return sandbox.RunResult{}, fmt.Errorf("attach local Docker sandbox: %w", runErr)
		}
		return sandbox.RunResult{}, errors.New("local Docker sandbox did not reach a terminal state")
	}
	return sandbox.RunResult{ExitCode: code, ResourceUsage: environmentFields(environment)}, nil
}

func (runtime *Runtime) PrepareEnvironment(ctx context.Context, request sandbox.EnvironmentRequest) error {
	if runtime == nil || runtime.Environment == nil {
		return nil
	}
	result, err := runtime.Environment.Prepare(ctx, request)
	if err != nil {
		return err
	}
	runtime.mu.Lock()
	if runtime.environments == nil {
		runtime.environments = map[string]EnvironmentResult{}
	}
	runtime.environments[request.ID] = result
	runtime.mu.Unlock()
	return nil
}

func (runtime *Runtime) ReleaseEnvironment(ctx context.Context, taskID string) error {
	if runtime == nil || runtime.Environment == nil {
		return nil
	}
	runtime.mu.Lock()
	delete(runtime.environments, taskID)
	runtime.mu.Unlock()
	return runtime.Environment.Release(ctx, taskID)
}

func (runtime *Runtime) environment(taskID string) EnvironmentResult {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.environments == nil {
		runtime.environments = map[string]EnvironmentResult{}
	}
	return runtime.environments[taskID]
}

func environmentFields(environment EnvironmentResult) map[string]interface{} {
	return map[string]interface{}{
		"environment_key":             environment.EnvironmentKey,
		"base_image_id":               environment.BaseImageID,
		"image_id":                    environment.ImageID,
		"cache_hit":                   environment.CacheHit,
		"builder_version":             environment.BuilderVersion,
		"environment_manifest_paths":  environment.ManifestPaths,
		"environment_manifest_hashes": environment.ManifestHashes,
	}
}

func buildArgs(image, user string, request sandbox.RunRequest, command []string) ([]string, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" || !taskIDPattern.MatchString(request.ID) {
		return nil, errors.New("sandbox command and task ID are required")
	}
	args := []string{
		"create", "--name", containerName(request.ID), "--init", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--cpus", fmt.Sprintf("%.3f", float64(request.Spec.Limits.CPUMillis)/1000),
		"--memory", strconv.FormatInt(request.Spec.Limits.MemoryBytes, 10),
		"--pids-limit", strconv.Itoa(request.Spec.Limits.PIDs),
		"--network", networkMode(request.Spec.Limits.Network),
	}
	// Docker Desktop on Windows commonly uses an overlay filesystem without
	// XFS project quotas, so --storage-opt=size=... is rejected by the daemon.
	// Keep the other frozen limits enforced and let the Gateway's output/log
	// budget guard the remaining disk usage on that platform.
	if runtime.GOOS != "windows" {
		args = append(args, "--storage-opt", "size="+strconv.FormatInt(request.Spec.Limits.DiskBytes, 10))
	}
	args = append(args,
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=67108864",
		"-v", request.Workspace+":/workspace:ro",
		"-v", request.OutputDir+":/output:rw",
	)
	for key, value := range request.Spec.Environment {
		if key == "" || strings.ContainsAny(key, "=\x00 \t\r\n") {
			return nil, errors.New("invalid environment variable name")
		}
		args = append(args, "--env", key+"="+value)
	}
	if user != "" {
		args = append(args, "--user", user)
	}
	args = append(args, "--entrypoint", command[0], image)
	args = append(args, command[1:]...)
	return args, nil
}

func (runtime *Runtime) Probe(ctx context.Context) error {
	if runtime == nil || strings.TrimSpace(runtime.Image) == "" {
		return errors.New("local Docker image is required")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(probeCtx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
		return fmt.Errorf("probe Docker daemon: %w: %s", err, boundedDockerOutput(output))
	}
	name := "mmdash-probe-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if output, err := exec.CommandContext(probeCtx, "docker", "create", "--name", name, "--network", "none", "--entrypoint", "/bin/true", runtime.Image).CombinedOutput(); err != nil {
		return fmt.Errorf("probe local Docker image: %w: %s", err, boundedDockerOutput(output))
	}
	defer exec.CommandContext(context.Background(), "docker", "rm", "--force", name).Run()
	if output, err := exec.CommandContext(probeCtx, "docker", "start", "--attach", name).CombinedOutput(); err != nil {
		return fmt.Errorf("probe local Docker lifecycle: %w: %s", err, boundedDockerOutput(output))
	}
	if runtime.Environment != nil {
		_ = runtime.Environment.GC(context.Background(), time.Now().UTC())
	}
	return nil
}

func (*Runtime) Cancel(ctx context.Context, id string) error {
	if !taskIDPattern.MatchString(id) {
		return errors.New("invalid local Docker task ID")
	}
	return stopContainer(ctx, containerName(id), 30*time.Second)
}

func (runtime *Runtime) Destroy(ctx context.Context, id string) error {
	if !taskIDPattern.MatchString(id) {
		return errors.New("invalid local Docker task ID")
	}
	output, err := exec.CommandContext(ctx, "docker", "rm", "--force", containerName(id)).CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(output)), "no such container") {
		return fmt.Errorf("remove local Docker sandbox: %w: %s", err, boundedDockerOutput(output))
	}
	if runtime != nil && runtime.Environment != nil {
		return runtime.ReleaseEnvironment(ctx, id)
	}
	return nil
}

func inspectContainer(ctx context.Context, name string) (status string, exitCode int, exists bool, err error) {
	output, commandErr := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Status}} {{.State.ExitCode}}", name).CombinedOutput()
	if commandErr != nil {
		if strings.Contains(strings.ToLower(string(output)), "no such object") {
			return "", 0, false, nil
		}
		return "", 0, false, fmt.Errorf("inspect local Docker sandbox: %w: %s", commandErr, boundedDockerOutput(output))
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 {
		return "", 0, false, errors.New("invalid local Docker inspect response")
	}
	code, parseErr := strconv.Atoi(fields[1])
	if parseErr != nil {
		return "", 0, false, errors.New("invalid local Docker exit code")
	}
	return fields[0], code, true, nil
}

func stopContainer(ctx context.Context, name string, timeout time.Duration) error {
	stopCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := exec.CommandContext(stopCtx, "docker", "stop", "--time", "10", name).CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(output)), "no such container") {
		return fmt.Errorf("stop local Docker sandbox: %w: %s", err, boundedDockerOutput(output))
	}
	return nil
}

func containerName(taskID string) string { return "mmdash-task-" + taskID }

func boundedDockerOutput(output []byte) string {
	value := strings.TrimSpace(strings.ToValidUTF8(string(output), ""))
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}

func networkMode(value string) string {
	if value == "enabled" {
		return "bridge"
	}
	return "none"
}

func ensureOutput(path string) error {
	if path == "" {
		return errors.New("output directory is required")
	}
	return nil
}
