// Package sandbox implements the fixed-entrypoint Sandbox capability.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mmdash/mmdash/box/contracts"
)

// ErrExecutionDetached means the Gateway process stopped observing a still
// running recoverable Runtime. It is connectivity/local-controller state, not
// an experiment failure; a restarted Gateway must attach again by task ID.
var ErrExecutionDetached = errors.New("sandbox execution remains active and detached")

type Runtime interface {
	Run(context.Context, RunRequest) (RunResult, error)
	Cancel(context.Context, string) error
}

// Prober is implemented by Runtime adapters that can prove their external
// dependencies and a minimal create/run/destroy lifecycle are usable. A Box
// must advertise only runtimes whose probe succeeds.
type Prober interface {
	Probe(context.Context) error
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
	// System receives controller-owned preparation and lifecycle output. It is
	// intentionally separate from the experiment's stdout/stderr streams.
	System io.Writer
	Stdout io.Writer
	Stderr io.Writer
}

// EnvironmentRequest is passed to optional Runtime adapters before the
// execution container is started. The workspace has already been transferred
// and validated by Box at this point. Adapters must never clone a repository.
type EnvironmentRequest struct {
	ID        string
	Spec      contracts.RunSpec
	Workspace string
	System    io.Writer
	// SystemFields is optional because some embedders only expose a plain
	// writer. Gateway uses it to retain an auditable structured environment
	// record in the durable system log.
	SystemFields func(message string, fields map[string]interface{}) error
}

// EnvironmentPreparer lets a Runtime build or select a reproducible execution
// environment before Gateway reports the task as running. Runtime adapters
// that use a preconfigured immutable image do not need to implement it.
type EnvironmentPreparer interface {
	PrepareEnvironment(context.Context, EnvironmentRequest) error
}

// EnvironmentReleaser releases any durable environment reference acquired for
// a task. It is called for preparation failures and terminal cleanup.
type EnvironmentReleaser interface {
	ReleaseEnvironment(context.Context, string) error
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
	kind, file, err := ParseEntrypoint(entrypoint)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "python", "python3":
		return []string{"python3", path.Join("/workspace", file)}, nil
	case "node":
		return []string{"node", path.Join("/workspace", file)}, nil
	case "go":
		return []string{"go", "run", path.Join("/workspace", file)}, nil
	case "binary":
		return []string{path.Join("/workspace", file)}, nil
	default:
		return nil, fmt.Errorf("unsupported entrypoint kind %q", kind)
	}
}

// ParseEntrypoint validates the frozen fixed entrypoint syntax and splits it
// into its kind and workspace-relative file so non-container Runtimes can map
// it onto host paths themselves.
func ParseEntrypoint(entrypoint string) (kind, file string, err error) {
	if strings.ContainsAny(entrypoint, " \t\r\n;&|$`()<>\"") {
		return "", "", errors.New("entrypoint contains shell syntax")
	}
	parts := strings.SplitN(entrypoint, ":", 2)
	if len(parts) != 2 || parts[1] == "" || strings.ContainsRune(parts[1], 0) {
		return "", "", errors.New("invalid fixed entrypoint")
	}
	relative := filepath.ToSlash(filepath.Clean(parts[1]))
	if relative == "." || strings.HasPrefix(relative, "../") || relative == ".." || filepath.IsAbs(relative) {
		return "", "", errors.New("entrypoint escapes workspace")
	}
	return parts[0], relative, nil
}

func ensureOutputDirectory(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("output directory must be absolute")
	}
	return os.MkdirAll(path, 0o700)
}
