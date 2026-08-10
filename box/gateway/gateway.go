// Package gateway implements the standalone Box process. It is an outbound
// Core client plus a bounded task executor; the Core database is never opened
// by this module.
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
)

type Config struct {
	ProjectID         string
	Name              string
	Version           string
	RegistrationToken string
	BoxID             string
	BoxToken          string
	StatePath         string
	WorkspaceRoot     string
	OutputRoot        string
	HeartbeatInterval time.Duration
	ClaimInterval     time.Duration
	Lease             time.Duration
	MaxConcurrent     int
	Capabilities      []contracts.Capability
	Runtimes          []contracts.Runtime
	Limits            contracts.ResourceLimits
}

type WorkspaceProvider interface {
	Prepare(context.Context, contracts.RunSpec) (workspace string, cleanup func(), err error)
}

// StaticWorkspace represents a Repo-owned detached checkout mounted into the
// Box.  The operator must provide the commit marker produced with that
// checkout; the Box never follows a branch or performs Git operations.
type StaticWorkspace struct {
	Root   string
	Commit string
}

func (provider StaticWorkspace) Prepare(_ context.Context, spec contracts.RunSpec) (string, func(), error) {
	if provider.Root == "" || provider.Commit == "" || spec.SourceCommit == "" || provider.Commit != spec.SourceCommit {
		return "", nil, errors.New("workspace is not pinned to the requested commit")
	}
	info, err := os.Stat(provider.Root)
	if err != nil || !info.IsDir() {
		return "", nil, errors.New("configured workspace is unavailable")
	}
	marker, err := os.ReadFile(filepath.Join(provider.Root, ".mmdash-commit"))
	if err != nil || strings.TrimSpace(string(marker)) != spec.SourceCommit {
		return "", nil, errors.New("workspace commit marker does not match the frozen commit")
	}
	return provider.Root, func() {}, nil
}

type RuntimeFactory func(contracts.RunSpec) (sandbox.Runtime, error)

type Gateway struct {
	Client    CoreClient
	Config    Config
	Workspace WorkspaceProvider
	Runtime   RuntimeFactory
	Stdout    io.Writer
	Stderr    io.Writer
	mu        sync.Mutex
	running   map[string]struct{}
}

func (gateway *Gateway) Run(ctx context.Context) error {
	if err := gateway.validate(); err != nil {
		return err
	}
	if gateway.Stdout == nil {
		gateway.Stdout = io.Discard
	}
	if gateway.Stderr == nil {
		gateway.Stderr = io.Discard
	}
	if gateway.Config.HeartbeatInterval <= 0 {
		gateway.Config.HeartbeatInterval = 15 * time.Second
	}
	if gateway.Config.ClaimInterval <= 0 {
		gateway.Config.ClaimInterval = 2 * time.Second
	}
	if gateway.Config.Lease <= 0 {
		gateway.Config.Lease = time.Minute
	}
	if gateway.Config.MaxConcurrent <= 0 {
		gateway.Config.MaxConcurrent = 1
	}
	gateway.running = map[string]struct{}{}
	if err := gateway.restore(); err != nil {
		return err
	}
	if gateway.Config.BoxID == "" || gateway.Config.BoxToken == "" {
		registration, err := gateway.Client.Register(ctx, gateway.Config.RegistrationToken, RegistrationInput{ProjectID: gateway.Config.ProjectID, Name: gateway.Config.Name, Version: gateway.Config.Version, Capabilities: gateway.Config.Capabilities, Runtimes: gateway.Config.Runtimes, Limits: gateway.Config.Limits, Idempotency: gateway.Config.Name + "-" + gateway.Config.Version})
		if err != nil {
			return err
		}
		gateway.Config.BoxID, gateway.Config.BoxToken = registration.BoxID, registration.Token
		if err := gateway.persist(); err != nil {
			return err
		}
	}
	if err := gateway.heartbeat(ctx); err != nil {
		return err
	}
	heartbeat := time.NewTicker(gateway.Config.HeartbeatInterval)
	claims := time.NewTicker(gateway.Config.ClaimInterval)
	defer heartbeat.Stop()
	defer claims.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-heartbeat.C:
			if err := gateway.heartbeat(ctx); err != nil {
				return err
			}
		case <-claims.C:
			if err := gateway.claimOne(ctx); err != nil {
				return err
			}
		}
	}
}

func (gateway *Gateway) validate() error {
	if gateway.Client == nil || gateway.Workspace == nil || gateway.Runtime == nil {
		return errors.New("Box Gateway is not configured")
	}
	if gateway.Config.ProjectID == "" || gateway.Config.Name == "" || gateway.Config.Version == "" || len(gateway.Config.Capabilities) == 0 || len(gateway.Config.Runtimes) == 0 {
		return errors.New("Box identity and capabilities are required")
	}
	if err := gateway.Config.Limits.Validate(); err != nil {
		return err
	}
	if gateway.Config.OutputRoot == "" || !filepath.IsAbs(gateway.Config.OutputRoot) {
		return errors.New("an absolute output root is required")
	}
	return os.MkdirAll(gateway.Config.OutputRoot, 0o700)
}

func (gateway *Gateway) heartbeat(ctx context.Context) error {
	gateway.mu.Lock()
	load := contracts.Load{RunningTasks: len(gateway.running), Capacity: gateway.Config.MaxConcurrent}
	gateway.mu.Unlock()
	return gateway.Client.Heartbeat(ctx, gateway.Config.BoxID, gateway.Config.BoxToken, RegistrationInput{Version: gateway.Config.Version, Capabilities: gateway.Config.Capabilities, Runtimes: gateway.Config.Runtimes, Limits: gateway.Config.Limits}, load)
}

func (gateway *Gateway) claimOne(ctx context.Context) error {
	gateway.mu.Lock()
	if len(gateway.running) >= gateway.Config.MaxConcurrent {
		gateway.mu.Unlock()
		return nil
	}
	gateway.mu.Unlock()
	task, err := gateway.Client.Claim(ctx, gateway.Config.BoxID, gateway.Config.BoxToken, gateway.Config.Lease)
	if err != nil || task == nil {
		return err
	}
	gateway.mu.Lock()
	if _, exists := gateway.running[task.TaskID]; exists {
		gateway.mu.Unlock()
		return nil
	}
	gateway.running[task.TaskID] = struct{}{}
	gateway.mu.Unlock()
	go func() {
		defer func() { gateway.mu.Lock(); delete(gateway.running, task.TaskID); gateway.mu.Unlock() }()
		_ = gateway.execute(ctx, *task)
	}()
	return nil
}

func (gateway *Gateway) execute(parent context.Context, task contracts.Task) error {
	spec := contracts.RunSpec{}
	encoded, _ := json.Marshal(task.RunSpec)
	if err := json.Unmarshal(encoded, &spec); err != nil || spec.Validate() != nil {
		return gateway.reportFailure(parent, task, "INVALID_RUN_SPEC", "the frozen run specification is invalid")
	}
	if err := gateway.Client.Status(parent, gateway.Config.BoxID, gateway.Config.BoxToken, task.TaskID, "preparing", nil, "", "", nil, ""); err != nil {
		return err
	}
	workspace, cleanup, err := gateway.Workspace.Prepare(parent, spec)
	if err != nil {
		return gateway.reportFailure(parent, task, "WORKSPACE_UNAVAILABLE", err.Error())
	}
	defer cleanup()
	output := filepath.Join(gateway.Config.OutputRoot, task.TaskID)
	if err := os.MkdirAll(output, 0o700); err != nil {
		return gateway.reportFailure(parent, task, "OUTPUT_UNAVAILABLE", err.Error())
	}
	defer os.RemoveAll(output)
	archivePath := filepath.Join(gateway.Config.OutputRoot, task.TaskID+".artifact.zip")
	defer os.Remove(archivePath)
	runtime, err := gateway.Runtime(spec)
	if err != nil {
		return gateway.reportFailure(parent, task, "RUNTIME_UNAVAILABLE", err.Error())
	}
	runCtx, cancel := context.WithCancel(parent)
	defer cancel()
	if destroyer, ok := runtime.(sandbox.Destroyer); ok {
		defer func() { _ = destroyer.Destroy(context.Background(), task.TaskID) }()
	}
	renewDone := make(chan struct{})
	defer close(renewDone)
	go func() {
		ticker := time.NewTicker(gateway.Config.Lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-renewDone:
				return
			case <-runCtx.Done():
				return
			case <-ticker.C:
				cancelRequested, err := gateway.Client.Renew(runCtx, gateway.Config.BoxID, gateway.Config.BoxToken, task.TaskID, gateway.Config.Lease)
				if err != nil || cancelRequested {
					_ = runtime.Cancel(context.Background(), task.TaskID)
					cancel()
					return
				}
			}
		}
	}()
	if err := gateway.Client.Status(parent, gateway.Config.BoxID, gateway.Config.BoxToken, task.TaskID, "running", nil, "", "", nil, ""); err != nil {
		return err
	}
	result, runErr := runtime.Run(runCtx, sandbox.RunRequest{ID: task.TaskID, Spec: spec, Workspace: workspace, OutputDir: output, Stdout: gateway.logWriter(runCtx, task.TaskID, "info"), Stderr: gateway.logWriter(runCtx, task.TaskID, "error")})
	if runErr != nil {
		return gateway.reportFailure(parent, task, "RUNTIME_FAILED", runErr.Error())
	}
	if result.TimedOut {
		return gateway.Client.Status(parent, gateway.Config.BoxID, gateway.Config.BoxToken, task.TaskID, "timed_out", &result.ExitCode, "TIMED_OUT", "sandbox deadline exceeded", result.ResourceUsage, "")
	}
	if result.Canceled {
		return gateway.Client.Status(parent, gateway.Config.BoxID, gateway.Config.BoxToken, task.TaskID, "canceled", &result.ExitCode, "CANCELED", "sandbox canceled", result.ResourceUsage, "")
	}
	manifest, err := loadManifest(output)
	if err != nil {
		return gateway.reportFailure(parent, task, "MANIFEST_INVALID", err.Error())
	}
	artifact, err := sandbox.BuildArtifactZip(output, archivePath, manifest)
	if err != nil {
		return gateway.reportFailure(parent, task, "ARTIFACT_INVALID", err.Error())
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return gateway.reportFailure(parent, task, "ARTIFACT_UNAVAILABLE", err.Error())
	}
	archiveInfo, err := archive.Stat()
	if err != nil {
		_ = archive.Close()
		return gateway.reportFailure(parent, task, "ARTIFACT_UNAVAILABLE", err.Error())
	}
	artifact, err = gateway.Client.UploadArtifact(parent, gateway.Config.BoxID, gateway.Config.BoxToken, task.TaskID, archive, archiveInfo.Size(), artifact.SHA256)
	_ = archive.Close()
	if err != nil {
		return err
	}
	if err := gateway.Client.Result(parent, gateway.Config.BoxID, gateway.Config.BoxToken, task.TaskID, manifest, artifact); err != nil {
		return err
	}
	return nil
}

func (gateway *Gateway) reportFailure(ctx context.Context, task contracts.Task, code, message string) error {
	return gateway.Client.Status(ctx, gateway.Config.BoxID, gateway.Config.BoxToken, task.TaskID, "failed", nil, code, message, nil, "")
}

type logWriter struct {
	ctx           context.Context
	gateway       *Gateway
	taskID, level string
}

func (writer logWriter) Write(contents []byte) (int, error) {
	message := string(contents)
	if len(message) > 20_000 {
		message = message[:20_000]
	}
	if err := writer.gateway.Client.Log(writer.ctx, writer.gateway.Config.BoxID, writer.gateway.Config.BoxToken, writer.taskID, contracts.Log{Level: writer.level, Message: message}); err != nil {
		return 0, err
	}
	return len(contents), nil
}
func (gateway *Gateway) logWriter(ctx context.Context, taskID, level string) io.Writer {
	return logWriter{ctx: ctx, gateway: gateway, taskID: taskID, level: level}
}

func loadManifest(root string) (contracts.Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return contracts.Manifest{}, err
	}
	var manifest contracts.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return contracts.Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return contracts.Manifest{}, err
	}
	return manifest, nil
}

type persistedState struct {
	BoxID    string `json:"box_id"`
	BoxToken string `json:"box_token"`
}

func (gateway *Gateway) restore() error {
	if gateway.Config.StatePath == "" {
		return nil
	}
	data, err := os.ReadFile(gateway.Config.StatePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	if gateway.Config.BoxID == "" {
		gateway.Config.BoxID = state.BoxID
	}
	if gateway.Config.BoxToken == "" {
		gateway.Config.BoxToken = state.BoxToken
	}
	return nil
}

func (gateway *Gateway) persist() error {
	if gateway.Config.StatePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(gateway.Config.StatePath), 0o700); err != nil {
		return err
	}
	data, _ := json.Marshal(persistedState{BoxID: gateway.Config.BoxID, BoxToken: gateway.Config.BoxToken})
	temporary := gateway.Config.StatePath + ".tmp-" + randomSuffix()
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, gateway.Config.StatePath)
}

func randomSuffix() string {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data)
}
