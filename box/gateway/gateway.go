// Package gateway implements the outbound-only Box controller. Core remains
// authoritative, while this process durably spools execution state, logs and
// result bundles so a temporary network outage never cancels a Runtime.
package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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

const (
	maximumClaimWait = 60 * time.Second
	defaultLogBudget = int64(256 << 20)
	maximumLogChunk  = 20_000
)

type Config struct {
	InstallationID    string
	Name              string
	Version           string
	BoxID             string
	BoxToken          string
	StatePath         string
	OutputRoot        string
	HeartbeatInterval time.Duration
	ClaimWait         time.Duration
	RetryDelay        time.Duration
	MaxConcurrent     int
	LogBudgetBytes    int64
	Capabilities      []contracts.Capability
	Runtimes          []contracts.Runtime
	Limits            contracts.ResourceLimits
}

type WorkspaceProvider interface {
	Prepare(context.Context, contracts.RunSpec) (workspace string, cleanup func(), err error)
}

// StaticWorkspace is retained for explicit offline development fixtures. A
// production Gateway uses TransferWorkspace and never receives Git secrets.
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

type executionControl struct {
	cancel           context.CancelFunc
	runtime          sandbox.Runtime
	stopRequested    bool
	cleanupRequested bool
}

type Gateway struct {
	Client    CoreClient
	Config    Config
	Workspace WorkspaceProvider
	Runtime   RuntimeFactory
	Stdout    io.Writer
	Stderr    io.Writer

	mu      sync.Mutex
	syncMu  sync.Mutex
	state   persistedState
	running map[string]*executionControl
}

func (gateway *Gateway) Run(ctx context.Context) error {
	if err := gateway.initialize(); err != nil {
		return err
	}
	if err := gateway.ensureIdentity(ctx); err != nil {
		return err
	}
	gateway.resumeLocalTasks(ctx)

	permanentErrors := make(chan error, 2)
	go gateway.heartbeatLoop(ctx, permanentErrors)
	go gateway.syncLoop(ctx)
	for {
		gateway.resumeLocalTasks(ctx)
		if gateway.hasCapacity() {
			task, err := gateway.Client.Claim(ctx, gateway.Config.BoxID, gateway.Config.BoxToken, gateway.Config.ClaimWait)
			if err != nil && !isRetryableCoreError(err) {
				return err
			}
			if err != nil {
				timer := time.NewTimer(gateway.Config.RetryDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case permanentErr := <-permanentErrors:
					timer.Stop()
					return permanentErr
				case <-timer.C:
				}
			}
			if task != nil {
				gateway.startTask(ctx, *task)
			}
		} else {
			timer := time.NewTimer(gateway.Config.RetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case err := <-permanentErrors:
				timer.Stop()
				return err
			case <-timer.C:
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-permanentErrors:
			return err
		default:
		}
	}
}

func (gateway *Gateway) heartbeatLoop(ctx context.Context, permanentErrors chan<- error) {
	ticker := time.NewTicker(gateway.Config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		if err := gateway.tryHeartbeat(ctx); err != nil && !isRetryableCoreError(err) {
			select {
			case permanentErrors <- err:
			case <-ctx.Done():
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (gateway *Gateway) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(gateway.Config.RetryDelay)
	defer ticker.Stop()
	for {
		gateway.syncAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (gateway *Gateway) initialize() error {
	if gateway.Client == nil || gateway.Workspace == nil || gateway.Runtime == nil {
		return errors.New("Box Gateway is not configured")
	}
	if strings.TrimSpace(gateway.Config.Name) == "" || strings.TrimSpace(gateway.Config.Version) == "" ||
		len(gateway.Config.Capabilities) == 0 || len(gateway.Config.Runtimes) == 0 {
		return errors.New("Box identity and probed capabilities are required")
	}
	if err := gateway.Config.Limits.Validate(); err != nil {
		return err
	}
	if gateway.Config.OutputRoot == "" || !filepath.IsAbs(gateway.Config.OutputRoot) {
		return errors.New("an absolute output root is required")
	}
	if gateway.Config.HeartbeatInterval <= 0 {
		gateway.Config.HeartbeatInterval = 15 * time.Second
	}
	if gateway.Config.ClaimWait <= 0 || gateway.Config.ClaimWait > maximumClaimWait {
		gateway.Config.ClaimWait = maximumClaimWait
	}
	if gateway.Config.RetryDelay <= 0 {
		gateway.Config.RetryDelay = 2 * time.Second
	}
	if gateway.Config.MaxConcurrent <= 0 {
		gateway.Config.MaxConcurrent = 1
	}
	if gateway.Config.LogBudgetBytes <= 0 {
		gateway.Config.LogBudgetBytes = defaultLogBudget
	}
	if gateway.Stdout == nil {
		gateway.Stdout = os.Stdout
	}
	if gateway.Stderr == nil {
		gateway.Stderr = os.Stderr
	}
	if err := os.MkdirAll(gateway.Config.OutputRoot, 0o700); err != nil {
		return err
	}
	state, err := loadState(gateway.Config.StatePath)
	if err != nil {
		return err
	}
	if gateway.Config.InstallationID != "" {
		state.InstallationID = gateway.Config.InstallationID
	}
	if state.InstallationID == "" {
		state.InstallationID = "box-installation-" + randomSuffix()
	}
	if gateway.Config.BoxID != "" {
		state.BoxID = gateway.Config.BoxID
	}
	if gateway.Config.BoxToken != "" {
		state.BoxToken = gateway.Config.BoxToken
	}
	gateway.state = state
	gateway.Config.InstallationID = state.InstallationID
	gateway.Config.BoxID = state.BoxID
	gateway.Config.BoxToken = state.BoxToken
	gateway.running = map[string]*executionControl{}
	return gateway.persist()
}

func (gateway *Gateway) ensureIdentity(ctx context.Context) error {
	if gateway.Config.BoxID != "" && gateway.Config.BoxToken != "" {
		return nil
	}
	authorization, err := gateway.Client.StartDeviceAuthorization(ctx)
	if err != nil {
		return err
	}
	verification := authorization.VerificationURIComplete
	if verification == "" {
		verification = authorization.VerificationURI
	}
	_, _ = fmt.Fprintf(gateway.Stdout, "请在浏览器打开 %s 并确认 Box 登录（验证码：%s）\n", verification, authorization.UserCode)
	interval := time.Duration(authorization.Interval) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}
	var grant contracts.BoxRegistrationGrant
	for {
		if !authorization.ExpiresAt.IsZero() && !time.Now().Before(authorization.ExpiresAt) {
			return errors.New("Box device authorization expired")
		}
		grant, err = gateway.Client.ExchangeDeviceAuthorization(ctx, authorization.DeviceCode)
		if err == nil {
			break
		}
		var pending interface{ AuthorizationPending() bool }
		if !errors.As(err, &pending) || !pending.AuthorizationPending() {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	registration, err := gateway.Client.Register(ctx, grant.RegistrationGrant, gateway.registrationInput())
	if err != nil {
		return err
	}
	gateway.mu.Lock()
	gateway.Config.BoxID, gateway.Config.BoxToken = registration.BoxID, registration.Token
	gateway.state.BoxID, gateway.state.BoxToken = registration.BoxID, registration.Token
	err = gateway.persistLocked()
	gateway.mu.Unlock()
	return err
}

func (gateway *Gateway) registrationInput() RegistrationInput {
	return RegistrationInput{
		InstallationID: gateway.Config.InstallationID, Name: gateway.Config.Name,
		Version: gateway.Config.Version, Capabilities: gateway.Config.Capabilities,
		Runtimes: gateway.Config.Runtimes, Limits: gateway.Config.Limits,
	}
}

func (gateway *Gateway) tryHeartbeat(ctx context.Context) error {
	gateway.mu.Lock()
	load := contracts.Load{RunningTasks: len(gateway.running), Capacity: gateway.Config.MaxConcurrent}
	gateway.mu.Unlock()
	return gateway.Client.Heartbeat(ctx, gateway.Config.BoxID, gateway.Config.BoxToken, gateway.registrationInput(), load)
}

func (gateway *Gateway) hasCapacity() bool {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	return len(gateway.running) < gateway.Config.MaxConcurrent
}

func (gateway *Gateway) startTask(parent context.Context, task contracts.Task) {
	if task.TaskID == "" || task.ExecutionEpoch == "" || task.BoxID != gateway.Config.BoxID {
		gateway.writeTaskError(task.TaskID, errors.New("Core returned an invalid Box task identity"))
		return
	}
	gateway.mu.Lock()
	if _, exists := gateway.state.Tasks[task.TaskID]; exists {
		gateway.mu.Unlock()
		return
	}
	state := &taskState{Task: task, Phase: "preparing", NextSequence: 1, Logs: []contracts.LogEntry{}}
	gateway.state.Tasks[task.TaskID] = state
	_ = gateway.persistLocked()
	gateway.mu.Unlock()
	gateway.launchTask(parent, task.TaskID)
}

func (gateway *Gateway) resumeLocalTasks(parent context.Context) {
	gateway.mu.Lock()
	ids := make([]string, 0, len(gateway.state.Tasks))
	for id, state := range gateway.state.Tasks {
		if !state.RuntimeFinished {
			ids = append(ids, id)
		}
	}
	gateway.mu.Unlock()
	for _, id := range ids {
		gateway.launchTask(parent, id)
	}
}

func (gateway *Gateway) launchTask(parent context.Context, taskID string) {
	gateway.mu.Lock()
	if _, exists := gateway.running[taskID]; exists || len(gateway.running) >= gateway.Config.MaxConcurrent {
		gateway.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(parent)
	control := &executionControl{cancel: cancel}
	gateway.running[taskID] = control
	gateway.mu.Unlock()
	go func() {
		defer func() {
			gateway.mu.Lock()
			current := gateway.running[taskID]
			cleanupRequested := current == control && current.cleanupRequested
			if current == control {
				delete(gateway.running, taskID)
			}
			gateway.mu.Unlock()
			if cleanupRequested {
				gateway.cleanupTask(taskID)
			}
		}()
		if err := gateway.execute(runCtx, taskID); err != nil &&
			!errors.Is(err, context.Canceled) && !errors.Is(err, sandbox.ErrExecutionDetached) {
			gateway.writeTaskError(taskID, err)
		}
	}()
}

func (gateway *Gateway) execute(ctx context.Context, taskID string) error {
	gateway.mu.Lock()
	local := gateway.state.Tasks[taskID]
	if local == nil {
		gateway.mu.Unlock()
		return errors.New("local Box task state is missing")
	}
	task := local.Task
	recovering := local.RuntimeStarted
	gateway.mu.Unlock()

	spec, err := decodeRunSpec(task.RunSpec)
	if err != nil {
		gateway.failTask(taskID, "preparing", "INVALID_RUN_SPEC", err.Error(), false, nil)
		return nil
	}
	if spec.ExecutionEpoch != task.ExecutionEpoch || spec.Runtime != task.ActualRuntime || spec.RuntimeVersion != task.RuntimeVersion {
		gateway.failTask(taskID, "preparing", "INVALID_RUN_SPEC", "task identity does not match the frozen run specification", false, nil)
		return nil
	}
	if !recovering {
		gateway.queueStatus(taskID, statusCallback{Status: "preparing", OccurredAt: time.Now().UTC()})
	}
	workspace, cleanup, err := gateway.Workspace.Prepare(ctx, spec)
	if err != nil {
		gateway.failTask(taskID, "preparing", "WORKSPACE_UNAVAILABLE", err.Error(), true, nil)
		return nil
	}
	defer cleanup()
	output := filepath.Join(gateway.Config.OutputRoot, taskID)
	logsRoot := filepath.Join(output, "logs")
	if err := os.MkdirAll(logsRoot, 0o700); err != nil {
		gateway.failTask(taskID, "preparing", "OUTPUT_UNAVAILABLE", err.Error(), true, nil)
		return nil
	}
	stdoutFile, err := os.OpenFile(filepath.Join(logsRoot, "stdout.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		gateway.failTask(taskID, "preparing", "OUTPUT_UNAVAILABLE", err.Error(), true, nil)
		return nil
	}
	stderrFile, err := os.OpenFile(filepath.Join(logsRoot, "stderr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		_ = stdoutFile.Close()
		gateway.failTask(taskID, "preparing", "OUTPUT_UNAVAILABLE", err.Error(), true, nil)
		return nil
	}
	closeLogs := func() { _ = stdoutFile.Close(); _ = stderrFile.Close() }
	defer closeLogs()
	runtime, err := gateway.Runtime(spec)
	if err != nil {
		gateway.failTask(taskID, "preparing", "RUNTIME_UNAVAILABLE", err.Error(), false, nil)
		return nil
	}
		if preparer, ok := runtime.(sandbox.EnvironmentPreparer); ok {
			if err := preparer.PrepareEnvironment(ctx, sandbox.EnvironmentRequest{
				ID: taskID, Spec: spec, Workspace: workspace,
				System: io.MultiWriter(gateway.logWriter(taskID, "system"), stdoutFile),
				SystemFields: func(message string, fields map[string]interface{}) error {
					_, err := gateway.logWriter(taskID, "system").WriteFields([]byte(message), fields)
					return err
				},
			}); err != nil {
				if releaser, releaseOK := runtime.(sandbox.EnvironmentReleaser); releaseOK {
					_ = releaser.ReleaseEnvironment(context.Background(), taskID)
				}
				code := "ENVIRONMENT_UNAVAILABLE"
				var coded interface{ EnvironmentCode() string }
				if errors.As(err, &coded) && coded.EnvironmentCode() != "" {
					code = coded.EnvironmentCode()
				}
				var stable interface{ ErrorCode() string }
				if errors.As(err, &stable) && stable.ErrorCode() != "" {
					code = stable.ErrorCode()
				}
				gateway.failTask(taskID, "preparing", code, err.Error(), true, nil)
				return nil
			}
		}
	if gateway.attachRuntime(taskID, runtime) {
		cancelCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = runtime.Cancel(cancelCtx, taskID)
		cancel()
	}
	if !recovering {
		now := time.Now().UTC()
		gateway.mu.Lock()
		if state := gateway.state.Tasks[taskID]; state != nil {
			state.RuntimeStarted = true
			state.StartedAt = now
			_ = gateway.persistLocked()
		}
		gateway.mu.Unlock()
		gateway.queueStatus(taskID, statusCallback{Status: "running", OccurredAt: now})
	}
	result, runErr := runtime.Run(ctx, sandbox.RunRequest{
		ID: taskID, Spec: spec, Workspace: workspace, OutputDir: output,
		Stdout: io.MultiWriter(gateway.logWriter(taskID, "stdout"), stdoutFile),
		Stderr: io.MultiWriter(gateway.logWriter(taskID, "stderr"), stderrFile),
	})
	closeLogs()
	if errors.Is(runErr, sandbox.ErrExecutionDetached) {
		return sandbox.ErrExecutionDetached
	}
	cleanupResult := map[string]interface{}{"runtime_destroyed": true}
	if destroyer, ok := runtime.(sandbox.Destroyer); ok {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		destroyErr := destroyer.Destroy(cleanupCtx, taskID)
		cancel()
		if destroyErr != nil {
			cleanupResult = map[string]interface{}{"runtime_destroyed": false, "error": boundedMessage(destroyErr.Error())}
		}
	}
	if runErr != nil {
		// Runtimes can surface stable failure codes for restart-recovery and
		// enforcement outcomes; the Gateway keeps them intact so Experiment
		// operators see the exact cause instead of a generic runtime failure.
		code := "RUNTIME_FAILED"
		var stable interface{ ErrorCode() string }
		if errors.As(runErr, &stable) && stable.ErrorCode() != "" {
			code = stable.ErrorCode()
		}
		gateway.failTask(taskID, "running", code, runErr.Error(), false, cleanupResult)
		return nil
	}
	if result.TimedOut {
		gateway.terminalTask(taskID, "timed_out", result.ExitCode, "running", "TIMED_OUT", "sandbox deadline exceeded", false, cleanupResult, result.ResourceUsage)
		return nil
	}
	if result.Canceled {
		gateway.terminalTask(taskID, "canceled", result.ExitCode, "running", "CANCELED", "sandbox canceled", false, cleanupResult, result.ResourceUsage)
		return nil
	}
	if result.ExitCode != 0 {
		gateway.terminalTask(taskID, "failed", result.ExitCode, "running", "NON_ZERO_EXIT", fmt.Sprintf("sandbox exited with code %d", result.ExitCode), false, cleanupResult, result.ResourceUsage)
		return nil
	}
	finishedAt := time.Now().UTC()
	gateway.queueStatus(taskID, statusCallback{Status: "uploading", OccurredAt: finishedAt, ExitCode: &result.ExitCode, ResourceUsage: result.ResourceUsage})
	gateway.mu.Lock()
	state := gateway.state.Tasks[taskID]
	startedAt := state.StartedAt
	logsTruncated := state.LogsTruncated
	gateway.mu.Unlock()
	environment, err := manifestEnvironment(spec.Runtime, result.ResourceUsage)
	if err != nil {
		gateway.failTask(taskID, "uploading", "MANIFEST_INVALID", err.Error(), false, cleanupResult)
		return nil
	}
	manifest, err := sandbox.GenerateManifest(output, sandbox.ManifestInput{
		ExperimentID: spec.ExperimentID, SourceCommit: spec.SourceCommit,
		ResultDirectory: spec.ResultContract.Directory, Status: "succeeded",
		StartedAt: startedAt, FinishedAt: finishedAt, Runtime: spec.Runtime,
		RuntimeVersion: spec.RuntimeVersion, LogsTruncated: logsTruncated,
		ExitCode: &result.ExitCode, Environment: environment,
	}, spec.Limits.DiskBytes)
	if err != nil {
		gateway.failTask(taskID, "uploading", "MANIFEST_INVALID", err.Error(), false, cleanupResult)
		return nil
	}
	bundlePath := filepath.Join(gateway.Config.OutputRoot, taskID+"-execution-bundle.zip")
	bundle, err := sandbox.BuildArtifactZip(output, bundlePath, manifest)
	if err != nil {
		gateway.failTask(taskID, "uploading", "BUNDLE_INVALID", err.Error(), false, cleanupResult)
		return nil
	}
	manifestBytes, _ := json.Marshal(manifest)
	manifestDigest := sha256.Sum256(manifestBytes)
	gateway.mu.Lock()
	if state := gateway.state.Tasks[taskID]; state != nil {
		state.RuntimeFinished = true
		state.Phase = "uploading"
		state.BundlePath = bundlePath
		state.BundleSHA256 = bundle.SHA256
		state.BundleSize = bundle.Size
		state.ManifestSHA256 = hex.EncodeToString(manifestDigest[:])
		state.ResultPending = true
		_ = gateway.persistLocked()
	}
	gateway.mu.Unlock()
	gateway.syncAll(context.Background())
	return nil
}

func manifestEnvironment(runtimeName string, usage map[string]interface{}) (*contracts.ManifestEnvironment, error) {
	provider := "local-docker"
	if runtimeName == "local-process" {
		provider = "local-process"
	} else if runtimeName != "local-docker" {
		return nil, nil
	}
	stringValue := func(name string) (string, bool) {
		value, ok := usage[name].(string)
		return value, ok && strings.TrimSpace(value) != ""
	}
	environmentKey, keyOK := stringValue("environment_key")
	builderVersion, builderOK := stringValue("builder_version")
	cacheHit, cacheOK := usage["cache_hit"].(bool)
	paths, pathsOK := usage["environment_manifest_paths"].([]string)
	hashes, hashesOK := usage["environment_manifest_hashes"].(map[string]string)
	baseIdentity, baseOK := stringValue("base_image_id")
	environmentIdentity, environmentOK := stringValue("image_id")
	if runtimeName == "local-process" {
		// Provider-neutral evidence: the interpreter identity plays the base
		// role and the built environment identity the environment role.
		baseIdentity, baseOK = stringValue("interpreter_identity")
		environmentIdentity, environmentOK = stringValue("environment_identity")
	}
	if !keyOK || !baseOK || !environmentOK || !builderOK || !cacheOK || !pathsOK || !hashesOK {
		return nil, errors.New("sandbox environment evidence is incomplete")
	}
	environment := &contracts.ManifestEnvironment{
		Provider: provider, EnvironmentKey: environmentKey,
		BaseImageID: baseIdentity, EnvironmentImageID: environmentIdentity,
		ManifestPaths: paths, ManifestHashes: hashes,
		BuilderVersion: builderVersion, CacheHit: cacheHit,
	}
	if runtimeName == "local-process" {
		if dependencies, ok := usage["resolved_dependencies"].([]string); ok {
			environment.ResolvedDependencies = dependencies
		}
	}
	return environment, nil
}

func decodeRunSpec(value map[string]interface{}) (contracts.RunSpec, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return contracts.RunSpec{}, err
	}
	var spec contracts.RunSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return contracts.RunSpec{}, err
	}
	if err := spec.Validate(); err != nil {
		return contracts.RunSpec{}, err
	}
	return spec, nil
}

func (gateway *Gateway) failTask(taskID, stage, code, message string, retryable bool, cleanup map[string]interface{}) {
	gateway.terminalTask(taskID, "failed", 0, stage, code, message, retryable, cleanup, nil)
}

func (gateway *Gateway) terminalTask(taskID, status string, exitCode int, stage, code, message string, retryable bool, cleanup, usage map[string]interface{}) {
	now := time.Now().UTC()
	gateway.mu.Lock()
	state := gateway.state.Tasks[taskID]
	if state == nil {
		gateway.mu.Unlock()
		return
	}
	state.RuntimeFinished = true
	state.TerminalOnly = true
	failure := &contracts.Failure{
		Stage: stage, Code: code, Message: boundedMessage(message), FailedAt: now,
		BoxID: gateway.Config.BoxID, Runtime: state.Task.ActualRuntime,
		Attempt: state.Task.Attempt, Retryable: retryable, CleanupResult: cleanup,
	}
	if failure.CleanupResult == nil {
		failure.CleanupResult = map[string]interface{}{}
	}
	callback := statusCallback{Status: status, OccurredAt: now, Failure: failure, ResourceUsage: usage}
	if status != "failed" || exitCode != 0 {
		callback.ExitCode = &exitCode
	}
	state.Phase = status
	state.PendingStatuses = append(state.PendingStatuses, callback)
	_ = gateway.persistLocked()
	gateway.mu.Unlock()
	gateway.syncAll(context.Background())
}

func (gateway *Gateway) queueStatus(taskID string, callback statusCallback) {
	gateway.mu.Lock()
	if state := gateway.state.Tasks[taskID]; state != nil {
		state.Phase = callback.Status
		state.PendingStatuses = append(state.PendingStatuses, callback)
		_ = gateway.persistLocked()
	}
	gateway.mu.Unlock()
	gateway.syncAll(context.Background())
}

func (gateway *Gateway) syncAll(ctx context.Context) {
	gateway.syncMu.Lock()
	defer gateway.syncMu.Unlock()
	gateway.mu.Lock()
	ids := make([]string, 0, len(gateway.state.Tasks))
	for id := range gateway.state.Tasks {
		ids = append(ids, id)
	}
	gateway.mu.Unlock()
	for _, id := range ids {
		if err := gateway.syncTask(ctx, id); err != nil && !isRetryableCoreError(err) {
			gateway.writeTaskError(id, err)
		}
	}
}

func (gateway *Gateway) syncTask(ctx context.Context, taskID string) error {
	gateway.mu.Lock()
	state := gateway.state.Tasks[taskID]
	if state == nil {
		gateway.mu.Unlock()
		return nil
	}
	request := contracts.ResumeRequest{
		ExecutionEpoch: state.Task.ExecutionEpoch, LocalPhase: state.Phase,
		LastLocalSequence: state.NextSequence - 1, BundleState: state.bundleState(),
		AcknowledgedCallbacks: []string{},
	}
	gateway.mu.Unlock()
	resume, err := gateway.Client.Resume(ctx, gateway.Config.BoxID, gateway.Config.BoxToken, taskID, request)
	if err != nil {
		return err
	}
	if resume.Action != "continue" {
		gateway.applyStopAction(taskID, resume.Action)
		if resume.Action == "cleanup" || resume.Action == "stop_failed" || resume.Action == "stop_canceled" {
			return nil
		}
		return fmt.Errorf("Core returned unsupported resume action %q", resume.Action)
	}
	gateway.mu.Lock()
	if state := gateway.state.Tasks[taskID]; state != nil && resume.AcceptedThroughSequence > state.Acknowledged {
		state.Acknowledged = resume.AcceptedThroughSequence
		_ = gateway.persistLocked()
	}
	gateway.mu.Unlock()
	if err := gateway.flushStatuses(ctx, taskID); err != nil {
		return err
	}
	if err := gateway.flushLogs(ctx, taskID); err != nil {
		return err
	}
	if err := gateway.flushBundle(ctx, taskID); err != nil {
		return err
	}
	gateway.mu.Lock()
	state = gateway.state.Tasks[taskID]
	terminalComplete := state != nil && state.TerminalOnly && len(state.PendingStatuses) == 0
	gateway.mu.Unlock()
	if terminalComplete {
		gateway.cleanupTask(taskID)
	}
	return nil
}

func (gateway *Gateway) flushStatuses(ctx context.Context, taskID string) error {
	for {
		gateway.mu.Lock()
		state := gateway.state.Tasks[taskID]
		if state == nil || len(state.PendingStatuses) == 0 {
			gateway.mu.Unlock()
			return nil
		}
		callback := state.PendingStatuses[0]
		task := state.Task
		gateway.mu.Unlock()
		if err := gateway.Client.Status(ctx, gateway.Config.BoxID, gateway.Config.BoxToken, taskID,
			task.ExecutionEpoch, task.RuntimeVersion, callback.Status, callback.OccurredAt,
			callback.ExitCode, callback.Failure, callback.ResourceUsage, callback.Summary); err != nil {
			return err
		}
		gateway.mu.Lock()
		if state := gateway.state.Tasks[taskID]; state != nil && len(state.PendingStatuses) > 0 {
			state.PendingStatuses = state.PendingStatuses[1:]
			_ = gateway.persistLocked()
		}
		gateway.mu.Unlock()
	}
}

func (gateway *Gateway) flushLogs(ctx context.Context, taskID string) error {
	gateway.mu.Lock()
	state := gateway.state.Tasks[taskID]
	if state == nil {
		gateway.mu.Unlock()
		return nil
	}
	entries := make([]contracts.LogEntry, 0, 200)
	for _, entry := range state.Logs {
		if entry.Sequence > state.Acknowledged {
			entries = append(entries, entry)
			if len(entries) == 200 {
				break
			}
		}
	}
	if len(entries) == 0 && (!state.LogsTruncated || state.TruncationAcknowledged) {
		gateway.mu.Unlock()
		return nil
	}
	batch := contracts.LogBatch{
		ExecutionEpoch: state.Task.ExecutionEpoch, Entries: entries,
		LogsTruncated: state.LogsTruncated, TruncatedAt: state.TruncatedAt,
	}
	if len(entries) > 0 {
		batch.FirstSequence = entries[0].Sequence
	} else {
		batch.FirstSequence = state.Acknowledged + 1
	}
	gateway.mu.Unlock()
	ack, err := gateway.Client.Logs(ctx, gateway.Config.BoxID, gateway.Config.BoxToken, taskID, batch)
	if err != nil {
		return err
	}
	gateway.mu.Lock()
	if state := gateway.state.Tasks[taskID]; state != nil && ack.AcceptedThroughSequence > state.Acknowledged {
		state.Acknowledged = ack.AcceptedThroughSequence
	}
	if state := gateway.state.Tasks[taskID]; state != nil {
		if state.LogsTruncated {
			state.TruncationAcknowledged = true
		}
		remaining := state.Logs[:0]
		for _, entry := range state.Logs {
			if entry.Sequence > state.Acknowledged {
				remaining = append(remaining, entry)
			}
		}
		state.Logs = remaining
		_ = gateway.persistLocked()
	}
	gateway.mu.Unlock()
	if len(entries) == 200 {
		return gateway.flushLogs(ctx, taskID)
	}
	return nil
}

func (gateway *Gateway) flushBundle(ctx context.Context, taskID string) error {
	gateway.mu.Lock()
	state := gateway.state.Tasks[taskID]
	if state == nil || state.BundlePath == "" || !state.ResultPending {
		gateway.mu.Unlock()
		return nil
	}
	task := state.Task
	path, sha, size := state.BundlePath, state.BundleSHA256, state.BundleSize
	artifact := state.Artifact
	manifestSHA := state.ManifestSHA256
	gateway.mu.Unlock()
	if artifact == nil {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		uploaded, err := gateway.Client.UploadArtifact(ctx, gateway.Config.BoxID, gateway.Config.BoxToken,
			taskID, task.ExecutionEpoch, file, size, sha)
		_ = file.Close()
		if err != nil {
			return err
		}
		artifact = &uploaded
		gateway.mu.Lock()
		if state := gateway.state.Tasks[taskID]; state != nil {
			state.Artifact = artifact
			_ = gateway.persistLocked()
		}
		gateway.mu.Unlock()
	}
	if err := gateway.Client.Result(ctx, gateway.Config.BoxID, gateway.Config.BoxToken, taskID,
		task.ExecutionEpoch, manifestSHA, *artifact); err != nil {
		return err
	}
	gateway.cleanupTask(taskID)
	return nil
}

func (gateway *Gateway) attachRuntime(taskID string, runtime sandbox.Runtime) bool {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	control := gateway.running[taskID]
	if control == nil {
		return false
	}
	control.runtime = runtime
	return control.stopRequested
}

func (gateway *Gateway) applyStopAction(taskID, action string) {
	gateway.mu.Lock()
	control := gateway.running[taskID]
	var runtime sandbox.Runtime
	if control != nil {
		control.stopRequested = true
		control.cleanupRequested = true
		runtime = control.runtime
	}
	gateway.mu.Unlock()
	if control != nil {
		if runtime != nil {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := runtime.Cancel(cancelCtx, taskID); err != nil {
				gateway.writeTaskError(taskID, fmt.Errorf("cancel runtime: %w", err))
			}
			cancel()
		}
		control.cancel()
	} else {
		gateway.cleanupTask(taskID)
	}
	if action != "continue" {
		gateway.writeTaskError(taskID, fmt.Errorf("Core requested %s", action))
	}
}

func (gateway *Gateway) cleanupTask(taskID string) {
	gateway.mu.Lock()
	state := gateway.state.Tasks[taskID]
	if state == nil {
		gateway.mu.Unlock()
		return
	}
	bundlePath := state.BundlePath
	delete(gateway.state.Tasks, taskID)
	_ = gateway.persistLocked()
	gateway.mu.Unlock()
	if bundlePath != "" {
		_ = os.Remove(bundlePath)
	}
	_ = os.RemoveAll(filepath.Join(gateway.Config.OutputRoot, taskID))
}

type logWriter struct {
	gateway *Gateway
	taskID  string
	stream  string
}

func (writer logWriter) Write(contents []byte) (int, error) {
	return writer.write(contents, nil)
}

func (writer logWriter) WriteFields(contents []byte, fields map[string]interface{}) (int, error) {
	return writer.write(contents, fields)
}

func (writer logWriter) write(contents []byte, fields map[string]interface{}) (int, error) {
	original := len(contents)
	message := strings.ToValidUTF8(string(contents), "")
	if len(message) > maximumLogChunk {
		message = message[:maximumLogChunk]
	}
	writer.gateway.mu.Lock()
	state := writer.gateway.state.Tasks[writer.taskID]
	if state == nil {
		writer.gateway.mu.Unlock()
		return original, nil
	}
	if state.LogsTruncated || state.LogBytes+int64(len(message)) > writer.gateway.Config.LogBudgetBytes {
		if !state.LogsTruncated {
			now := time.Now().UTC()
			state.LogsTruncated = true
			state.TruncatedAt = &now
			_ = writer.gateway.persistLocked()
		}
		writer.gateway.mu.Unlock()
		return original, nil
	}
	entry := contracts.LogEntry{
		Sequence: state.NextSequence, Stream: writer.stream,
		OccurredAt: time.Now().UTC(), Message: message, Fields: fields,
	}
	state.NextSequence++
	state.LogBytes += int64(len(message))
	state.Logs = append(state.Logs, entry)
	err := writer.gateway.persistLocked()
	writer.gateway.mu.Unlock()
	if err != nil {
		return 0, err
	}
	go writer.gateway.syncAll(context.Background())
	return original, nil
}

func (gateway *Gateway) logWriter(taskID, stream string) logWriter {
	return logWriter{gateway: gateway, taskID: taskID, stream: stream}
}

func (gateway *Gateway) persist() error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	return gateway.persistLocked()
}

func (gateway *Gateway) persistLocked() error {
	return saveState(gateway.Config.StatePath, gateway.state)
}

func (gateway *Gateway) writeTaskError(taskID string, err error) {
	writer := gateway.Stderr
	if writer == nil {
		writer = io.Discard
	}
	_, _ = fmt.Fprintf(writer, "Box task %s: %s\n", taskID, boundedMessage(err.Error()))
}

func boundedMessage(message string) string {
	message = strings.TrimSpace(strings.ToValidUTF8(message, ""))
	if len(message) > 2_000 {
		message = message[:2_000]
	}
	return message
}

func isRetryableCoreError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var temporary interface{ Temporary() bool }
	return errors.As(err, &temporary) && temporary.Temporary()
}

func randomSuffix() string {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data)
}
