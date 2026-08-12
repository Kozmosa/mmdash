// Package sandbox implements the fixed-entrypoint Sandbox capability.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mmdash/mmdash/box/contracts"
)

type Runtime interface {
	Run(context.Context, RunRequest) (RunResult, error)
	Cancel(context.Context, string) error
}

// Destroyer is implemented by runtimes that own an external execution
// environment. Gateway callers invoke it after every terminal outcome.
type Destroyer interface {
	Destroy(context.Context, string) error
}

type RunRequest struct {
	ID        string
	Spec      contracts.RunSpec
	Workspace string
	OutputDir string
	Stdout    io.Writer
	Stderr    io.Writer
}

type RunResult struct {
	ExitCode      int
	TimedOut      bool
	Canceled      bool
	ResourceUsage map[string]interface{}
}

type Capability struct {
	Runtime Runtime
}

func (capability Capability) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	if capability.Runtime == nil || request.Spec.Validate() != nil {
		return RunResult{}, errors.New("sandbox is not configured")
	}
	if !filepath.IsAbs(request.OutputDir) || !filepath.IsAbs(request.Workspace) || request.Workspace == "" || request.OutputDir == "" {
		return RunResult{}, errors.New("sandbox paths must be absolute directories")
	}
	if err := ensureOutputDirectory(request.OutputDir); err != nil {
		return RunResult{}, err
	}
	return capability.Runtime.Run(ctx, request)
}

func EntrypointCommand(entrypoint string) ([]string, error) {
	if strings.ContainsAny(entrypoint, " \t\r\n;&|$`()<>\"") {
		return nil, errors.New("entrypoint contains shell syntax")
	}
	parts := strings.SplitN(entrypoint, ":", 2)
	if len(parts) != 2 || parts[1] == "" || strings.ContainsRune(parts[1], 0) {
		return nil, errors.New("invalid fixed entrypoint")
	}
	file := filepath.ToSlash(filepath.Clean(parts[1]))
	if file == "." || strings.HasPrefix(file, "../") || file == ".." || filepath.IsAbs(file) {
		return nil, errors.New("entrypoint escapes workspace")
	}
	switch parts[0] {
	case "python", "python3":
		return []string{"python3", filepath.Join("/workspace", file)}, nil
	case "node":
		return []string{"node", filepath.Join("/workspace", file)}, nil
	case "go":
		return []string{"go", "run", filepath.Join("/workspace", file)}, nil
	case "binary":
		return []string{filepath.Join("/workspace", file)}, nil
	default:
		return nil, fmt.Errorf("unsupported entrypoint kind %q", parts[0])
	}
}

func ensureOutputDirectory(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("output directory must be absolute")
	}
	return os.MkdirAll(path, 0o700)
}
