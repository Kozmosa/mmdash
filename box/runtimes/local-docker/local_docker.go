// Package localdocker runs only the fixed Sandbox command in a pre-defined image.
package localdocker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
)

type Runtime struct {
	Image string
	User  string
}

func (runtime Runtime) Run(ctx context.Context, request sandbox.RunRequest) (sandbox.RunResult, error) {
	if runtime.Image == "" {
		return sandbox.RunResult{}, errors.New("local Docker image is required")
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
	args, err := buildArgs(runtime.Image, runtime.User, request, command)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	commandContext, cancel := context.WithTimeout(ctx, time.Duration(request.Spec.Limits.TimeoutSecond)*time.Second)
	defer cancel()
	process := exec.CommandContext(commandContext, "docker", args...)
	process.Stdout = request.Stdout
	process.Stderr = request.Stderr
	err = process.Run()
	result := sandbox.RunResult{ResourceUsage: map[string]interface{}{}}
	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		return result, nil
	}
	if errors.Is(commandContext.Err(), context.Canceled) {
		result.Canceled = true
		return result, nil
	}
	if err != nil {
		if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		}
		return result, nil
	}
	return result, nil
}

func buildArgs(image, user string, request sandbox.RunRequest, command []string) ([]string, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return nil, errors.New("sandbox command is required")
	}
	args := []string{
		"run", "--rm", "--init", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--cpus", fmt.Sprintf("%.3f", float64(request.Spec.Limits.CPUMillis)/1000),
		"--memory", strconv.FormatInt(request.Spec.Limits.MemoryBytes, 10),
		"--pids-limit", strconv.Itoa(request.Spec.Limits.PIDs),
		"--network", networkMode(request.Spec.Limits.Network),
		"--storage-opt", "size=" + strconv.FormatInt(request.Spec.Limits.DiskBytes, 10),
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=67108864",
		"-v", request.Workspace + ":/workspace:ro",
		"-v", request.OutputDir + ":/output:rw",
	}
	for key, value := range request.Spec.Environment {
		if key == "" || strings.ContainsAny(key, "=\x00 \t\r\n") {
			return nil, errors.New("invalid environment variable name")
		}
		args = append(args, "--env", key+"="+value)
	}
	if user != "" {
		args = append(args, "--user", user)
	}
	// Always replace an image-defined ENTRYPOINT so the reviewed Sandbox argv
	// is executed directly instead of being appended to an unrelated process.
	args = append(args, "--entrypoint", command[0], image)
	args = append(args, command[1:]...)
	return args, nil
}

func (Runtime) Cancel(context.Context, string) error  { return nil }
func (Runtime) Destroy(context.Context, string) error { return nil }

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
