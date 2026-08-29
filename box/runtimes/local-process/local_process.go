// Package localprocess executes a frozen Sandbox task as a supervised process
// directly on the Box host. The Runtime is trusted-host by design: it is
// disabled by default, must be selected explicitly by the Experiment, enforces
// timeout, cancellation and the probed resource limits over the complete
// process tree, and provides no container-equivalent isolation.
package localprocess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
)

var taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// probeEnforcement verifies hard limit enforcement on this host. It is a
// package variable so hermetic tests can run without kernel enforcement
// support; production code always uses the real platform probe.
var probeEnforcement = enforceAvailability

// cgroupRootOverride redirects the cgroup v2 root in tests.
var cgroupRootOverride string

// RunnerSubcommand is the re-executed supervisor entry point of the Box
// binary. One runner process exists per task and survives Gateway restarts.
const RunnerSubcommand = "task-runner"

type Runtime struct {
	// StateDir is the durable runner state root, one directory per task.
	StateDir string
	// Python is the interpreter used when no cached environment applies.
	Python string
	// RunnerBinary defaults to the current executable for re-execution.
	RunnerBinary string
	// Environments owns the content-addressed Python environment cache.
	Environments *EnvironmentManager
	// CgroupRoot overrides the cgroup v2 root on Linux.
	Now          func() time.Time
	PollInterval time.Duration
	// RunnerLossGrace is how long follow keeps draining output after the
	// supervisor died without a terminal record before failing with
	// RUNNER_LOST. It covers a runner that is exiting normally right after
	// persisting the terminal state.
	RunnerLossGrace time.Duration

	mu           sync.Mutex
	environments map[string]EnvironmentResult
}

func NewRuntime(stateDir, python string, environments *EnvironmentManager) *Runtime {
	return &Runtime{
		StateDir: stateDir, Python: python, Environments: environments,
		Now:          func() time.Time { return time.Now().UTC() },
		PollInterval: 50 * time.Millisecond,
		environments: map[string]EnvironmentResult{},
	}
}

func (runtime *Runtime) pollInterval() time.Duration {
	if runtime.PollInterval > 0 {
		return runtime.PollInterval
	}
	return 50 * time.Millisecond
}

func (runtime *Runtime) runnerLossGrace() time.Duration {
	if runtime.RunnerLossGrace > 0 {
		return runtime.RunnerLossGrace
	}
	return 2 * time.Second
}

func (runtime *Runtime) now() time.Time {
	if runtime.Now != nil {
		return runtime.Now()
	}
	return time.Now().UTC()
}

// PrepareEnvironment implements sandbox.EnvironmentPreparer. The frozen
// limits are validated here so an unenforceable policy fails the task during
// preparation with a stable error instead of degrading silently.
func (runtime *Runtime) PrepareEnvironment(ctx context.Context, request sandbox.EnvironmentRequest) error {
	if runtime == nil || runtime.StateDir == "" || strings.TrimSpace(runtime.Python) == "" {
		return codedError(ErrCodeRunnerFailed, "Local Process runtime is not configured")
	}
	if err := enforceFrozenLimits(request.Spec); err != nil {
		return err
	}
	if runtime.Environments == nil {
		return nil
	}
	result, err := runtime.Environments.Prepare(ctx, request.ID, request.Workspace,
		request.Spec.EnvironmentSelection, request.System)
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

// environmentFor returns the prepared environment evidence of a task under
// the same mutex that PrepareEnvironment writes with: the Gateway runs tasks
// concurrently, so an unlocked map read would race another task's write.
func (runtime *Runtime) environmentFor(taskID string) EnvironmentResult {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.environments[taskID]
}

// ReleaseEnvironment implements sandbox.EnvironmentReleaser.
func (runtime *Runtime) ReleaseEnvironment(ctx context.Context, taskID string) error {
	if runtime == nil || runtime.Environments == nil {
		return nil
	}
	runtime.mu.Lock()
	delete(runtime.environments, taskID)
	runtime.mu.Unlock()
	return runtime.Environments.Release(ctx, taskID)
}

// enforceFrozenLimits rejects network and resource policies this host cannot
// enforce. The bare-metal Runtime has no network namespace capability, so any
// network policy is refused rather than treated as advisory.
func enforceFrozenLimits(spec contracts.RunSpec) error {
	if spec.Limits.Network != "enabled" {
		return codedError(ErrCodeLimitsNotEnforceable,
			"the local-process Runtime cannot enforce the requested network policy; it can only serve tasks that explicitly request network=enabled on a trusted host")
	}
	limits := contractLimits{
		CPUMillis: spec.Limits.CPUMillis, MemoryBytes: spec.Limits.MemoryBytes, PIDs: spec.Limits.PIDs,
	}
	if err := probeEnforcement(limits); err != nil {
		return codedError(ErrCodeLimitsNotEnforceable, err.Error())
	}
	return nil
}

func (runtime *Runtime) stateDir(taskID string) string {
	return filepath.Join(runtime.StateDir, taskID)
}

func (runtime *Runtime) recordPath(taskID string) string {
	return filepath.Join(runtime.stateDir(taskID), "state.json")
}

func (runtime *Runtime) Run(ctx context.Context, request sandbox.RunRequest) (sandbox.RunResult, error) {
	if runtime == nil || runtime.StateDir == "" || !taskIDPattern.MatchString(request.ID) {
		return sandbox.RunResult{}, errors.New("Local Process state directory and task ID are required")
	}
	if request.Spec.Validate() != nil {
		return sandbox.RunResult{}, errors.New("invalid frozen run specification")
	}
	if !filepath.IsAbs(request.OutputDir) || request.Workspace == "" {
		return sandbox.RunResult{}, errors.New("sandbox paths must be absolute directories")
	}
	record, exists, err := loadTaskRecord(runtime.recordPath(request.ID))
	if err != nil {
		return sandbox.RunResult{}, err
	}
	if exists {
		return runtime.reattach(ctx, request, record)
	}
	return runtime.start(ctx, request)
}

// reattach reconnects to a recorded task after a Gateway restart, or reports
// the stable terminal outcomes a restart cannot recover from.
func (runtime *Runtime) reattach(ctx context.Context, request sandbox.RunRequest, record taskRecord) (sandbox.RunResult, error) {
	currentBoot := bootID()
	if !sameBootID(record.BootID, currentBoot) {
		// A host reboot invalidates every recorded task. The same execution
		// is never replayed automatically.
		return sandbox.RunResult{}, codedError(ErrCodeHostRestarted,
			"the Box host restarted while local-process task "+record.TaskID+" was recorded")
	}
	if taskTerminal(record.State) {
		return runtime.resultFromRecord(record, request)
	}
	supervisedPID := record.TaskPID
	if supervisedPID <= 0 {
		supervisedPID = record.RunnerPID
	}
	if !processAlive(supervisedPID) {
		// The supervisor died without recording a terminal state on the same
		// host session. Terminate any surviving process tree first.
		_ = killTree(supervisedPID)
		return sandbox.RunResult{}, codedError(ErrCodeRunnerLost,
			"the local-process supervisor exited without a terminal record for task "+record.TaskID)
	}
	if record.RunnerPID > 0 && record.RunnerPID != record.TaskPID && !processAlive(record.RunnerPID) {
		// The task process is still alive but its supervisor is gone: nobody
		// enforces the frozen timeout or a later cancel anymore, so the
		// execution is not recoverable and the surviving tree is terminated.
		_ = killTree(record.TaskPID)
		return sandbox.RunResult{}, codedError(ErrCodeRunnerLost,
			"the local-process supervisor exited while task "+record.TaskID+" was still running")
	}
	result, err := runtime.follow(ctx, request, &record)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	return result, nil
}

func (runtime *Runtime) start(ctx context.Context, request sandbox.RunRequest) (sandbox.RunResult, error) {
	if err := enforceFrozenLimits(request.Spec); err != nil {
		return sandbox.RunResult{}, err
	}
	command, err := runtime.taskCommand(request)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	taskDir := runtime.stateDir(request.ID)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return sandbox.RunResult{}, err
	}
	homeDir := filepath.Join(taskDir, "home")
	tmpDir := filepath.Join(taskDir, "tmp")
	for _, directory := range []string{homeDir, tmpDir, request.OutputDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return sandbox.RunResult{}, err
		}
	}
	parametersFile := filepath.Join(taskDir, "parameters.json")
	parameters, err := json.Marshal(request.Spec.Parameters)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	if err := os.WriteFile(parametersFile, parameters, 0o600); err != nil {
		return sandbox.RunResult{}, err
	}
	environment := runtime.environmentFor(request.ID)
	venvDir := environment.VenvDir
	env, err := taskEnvironment(request.Spec.Environment, request.Workspace, request.OutputDir,
		request.Spec.ExperimentID, parametersFile, homeDir, tmpDir, venvDir)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	job := jobSpec{
		SchemaVersion: 1, TaskID: request.ID, ExecutionEpoch: request.Spec.ExecutionEpoch,
		ExperimentID: request.Spec.ExperimentID, Workspace: request.Workspace,
		OutputDir: request.OutputDir, ParametersFile: parametersFile,
		Command: command, Environment: env,
		TimeoutSecond: request.Spec.Limits.TimeoutSecond,
		CPUMillis:     request.Spec.Limits.CPUMillis, MemoryBytes: request.Spec.Limits.MemoryBytes,
		PIDs: request.Spec.Limits.PIDs,
	}
	if cgroupRoot := cgroupV2Root(); cgroupRoot != "" && runtime.cgroupEnabled() {
		job.CgroupPath = filepath.Join(cgroupRoot, "mmdash-task-"+request.ID)
	}
	jobData, err := json.Marshal(job)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	if err := os.WriteFile(filepath.Join(taskDir, "job.json"), jobData, 0o600); err != nil {
		return sandbox.RunResult{}, err
	}
	if err := saveSpoolOffsets(filepath.Join(taskDir, "spool.json"), spoolOffsets{SchemaVersion: 1}); err != nil {
		return sandbox.RunResult{}, err
	}
	runnerBinary := runtime.RunnerBinary
	if runnerBinary == "" {
		runnerBinary, err = os.Executable()
		if err != nil {
			return sandbox.RunResult{}, fmt.Errorf("resolve local-process runner binary: %w", err)
		}
	}
	if err := saveTaskRecord(runtime.recordPath(request.ID), taskRecord{
		SchemaVersion: taskStateSchemaVersion, TaskID: request.ID,
		ExecutionEpoch: request.Spec.ExecutionEpoch, BootID: bootID(),
		State: taskStateStarting, StartedAt: runtime.now().UTC(),
	}); err != nil {
		return sandbox.RunResult{}, err
	}
	// The runner is detached from the Gateway process group so it survives a
	// Gateway restart or terminal signal and keeps supervising the task.
	runner := exec.Command(runnerBinary, RunnerSubcommand,
		"--state-dir", runtime.StateDir, "--task-id", request.ID)
	runner.Env = nil
	runner.Stdout = io.Discard
	runner.Stderr = io.Discard
	runner.SysProcAttr = runnerProcessAttributes()
	if err := runner.Start(); err != nil {
		return sandbox.RunResult{}, fmt.Errorf("start local-process runner: %w", err)
	}
	record := taskRecord{
		SchemaVersion: taskStateSchemaVersion, TaskID: request.ID,
		ExecutionEpoch: request.Spec.ExecutionEpoch, BootID: bootID(),
		RunnerPID: runner.Process.Pid, State: taskStateStarting,
		StartedAt: runtime.now().UTC(),
	}
	// Record the supervisor PID only while the runner has not persisted its
	// own record yet: an unconditional rewrite here could clobber the record
	// of a task that finished before this process resumed scheduling.
	if current, exists, loadErr := loadTaskRecord(runtime.recordPath(request.ID)); loadErr == nil && exists {
		if current.RunnerPID == 0 {
			current.RunnerPID = runner.Process.Pid
			_ = saveTaskRecord(runtime.recordPath(request.ID), current)
		}
		record = current
	} else {
		_ = saveTaskRecord(runtime.recordPath(request.ID), record)
	}
	return runtime.follow(ctx, request, &record)
}

// follow streams the runner-owned task output into the Gateway spool until
// the durable record reaches a terminal state. A supervisor that dies without
// a terminal record fails with RUNNER_LOST after a grace period instead of
// being followed forever: a dead supervisor enforces neither timeout nor
// cancellation.
func (runtime *Runtime) follow(ctx context.Context, request sandbox.RunRequest, record *taskRecord) (sandbox.RunResult, error) {
	taskDir := runtime.stateDir(request.ID)
	spoolPath := filepath.Join(taskDir, "spool.json")
	offsets := loadSpoolOffsets(spoolPath)
	canceled := false
	supervisorDeadSince := time.Time{}
	for {
		if ctx.Err() != nil && !canceled {
			// The Gateway is stopping or Core requested a stop: cancel the
			// supervised task and keep draining its output.
			_ = os.WriteFile(cancelSentinelPath(taskDir), []byte(request.ID), 0o600)
			canceled = true
		}
		progress, err := emitOutput(taskDir, &offsets, request.Stdout, request.Stderr)
		if err != nil {
			return sandbox.RunResult{}, err
		}
		if progress {
			_ = saveSpoolOffsets(spoolPath, offsets)
		}
		current, exists, err := loadTaskRecord(runtime.recordPath(request.ID))
		if err != nil {
			return sandbox.RunResult{}, err
		}
		if !exists {
			return sandbox.RunResult{}, codedError(ErrCodeRunnerFailed,
				"the local-process task record disappeared for task "+request.ID)
		}
		*record = current
		if taskTerminal(current.State) {
			progress, _ = emitOutput(taskDir, &offsets, request.Stdout, request.Stderr)
			if progress {
				_ = saveSpoolOffsets(spoolPath, offsets)
			}
			// The runner exits right after persisting its terminal record;
			// reap it so the detached supervisor never lingers as a zombie.
			reapProcess(current.RunnerPID)
			break
		}
		if current.RunnerPID > 0 && !processAlive(current.RunnerPID) {
			if supervisorDeadSince.IsZero() {
				supervisorDeadSince = runtime.now()
			} else if runtime.now().Sub(supervisorDeadSince) >= runtime.runnerLossGrace() {
				_ = killTree(current.TaskPID)
				return sandbox.RunResult{}, codedError(ErrCodeRunnerLost,
					"the local-process supervisor exited while task "+current.TaskID+" was still running")
			}
		} else {
			supervisorDeadSince = time.Time{}
		}
		sleepMilli(int(runtime.pollInterval() / time.Millisecond))
	}
	return runtime.resultFromRecord(*record, request)
}

// emitOutput forwards newly written task output to the Gateway writers. It
// returns whether any bytes were emitted so spool offsets persist only on
// progress.
func emitOutput(taskDir string, offsets *spoolOffsets, stdout, stderr io.Writer) (bool, error) {
	progress := false
	stdoutPath := filepath.Join(taskDir, "task-stdout.log")
	stderrPath := filepath.Join(taskDir, "task-stderr.log")
	stdoutBytes, err := emitFile(stdoutPath, offsets.StdoutBytes, stdout)
	if err != nil {
		return progress, err
	}
	if stdoutBytes > offsets.StdoutBytes {
		offsets.StdoutBytes = stdoutBytes
		progress = true
	}
	stderrBytes, err := emitFile(stderrPath, offsets.StderrBytes, stderr)
	if err != nil {
		return progress, err
	}
	if stderrBytes > offsets.StderrBytes {
		offsets.StderrBytes = stderrBytes
		progress = true
	}
	return progress, nil
}

func emitFile(path string, offset int64, destination io.Writer) (int64, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return offset, nil
	}
	if err != nil {
		return offset, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}
	written, err := io.Copy(destination, file)
	if err != nil {
		return offset + written, err
	}
	return offset + written, nil
}

func (runtime *Runtime) resultFromRecord(record taskRecord, request sandbox.RunRequest) (sandbox.RunResult, error) {
	result := sandbox.RunResult{ExitCode: 0}
	if record.ExitCode != nil {
		result.ExitCode = *record.ExitCode
	}
	switch record.State {
	case taskStateTimedOut:
		result.TimedOut = true
	case taskStateCanceled:
		result.Canceled = true
	case taskStateFailed:
		// A failed record means the supervisor could not run the task at
		// all; reporting it as a zero-exit success would fabricate a pass.
		detail := record.LastError
		if detail == "" {
			detail = "no launch failure reason was recorded"
		}
		return sandbox.RunResult{}, codedError(ErrCodeRunnerFailed,
			"local-process task "+record.TaskID+" could not be started: "+detail)
	}
	result.ResourceUsage = runtime.resourceUsage(request, record)
	return result, nil
}

func (runtime *Runtime) resourceUsage(request sandbox.RunRequest, record taskRecord) map[string]interface{} {
	environment := runtime.environmentFor(request.ID)
	fields := map[string]interface{}{
		"provider":                    "local-process",
		"environment_key":             environment.EnvironmentKey,
		"interpreter_identity":        environment.InterpreterIdentity,
		"environment_identity":        environment.EnvironmentIdentity,
		"cache_hit":                   environment.CacheHit,
		"builder_version":             environment.BuilderVersion,
		"environment_manifest_paths":  environment.ManifestPaths,
		"environment_manifest_hashes": environment.ManifestHashes,
		"resolved_dependencies":       environment.ResolvedDependencies,
		"environment_best_effort":     environment.BestEffort,
		"runner_boot_id":              record.BootID,
	}
	return fields
}

// taskCommand maps the frozen entrypoint onto host paths. A prepared cached
// environment replaces the base interpreter with the environment's interpreter.
func (runtime *Runtime) taskCommand(request sandbox.RunRequest) ([]string, error) {
	kind, file, err := sandbox.ParseEntrypoint(request.Spec.Entrypoint)
	if err != nil {
		return nil, err
	}
	script := filepath.Join(request.Workspace, filepath.FromSlash(file))
	environment := runtime.environmentFor(request.ID)
	interpreter := runtime.Python
	if environment.VenvDir != "" {
		interpreter = venvPython(runtime.Python, environment.VenvDir)
	}
	switch kind {
	case "python", "python3":
		return []string{interpreter, script}, nil
	case "node":
		return []string{"node", script}, nil
	case "go":
		return []string{"go", "run", script}, nil
	case "binary":
		return []string{script}, nil
	default:
		return nil, fmt.Errorf("unsupported entrypoint kind %q", kind)
	}
}

// Cancel asks the supervisor to terminate the complete process tree. It is
// safe to call for unknown, finished or never-started tasks.
func (runtime *Runtime) Cancel(_ context.Context, id string) error {
	if runtime == nil || !taskIDPattern.MatchString(id) {
		return errors.New("invalid local-process task ID")
	}
	taskDir := runtime.stateDir(id)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(cancelSentinelPath(taskDir), []byte(id), 0o600)
}

// Destroy releases the environment reference and removes the durable state of
// a terminal task. It never deletes a live execution: callers invoke Destroy
// only after a terminal outcome.
func (runtime *Runtime) Destroy(ctx context.Context, id string) error {
	if runtime == nil || !taskIDPattern.MatchString(id) {
		return errors.New("invalid local-process task ID")
	}
	if runtime.Environments != nil {
		_ = runtime.ReleaseEnvironment(ctx, id)
	}
	record, exists, err := loadTaskRecord(runtime.recordPath(id))
	if err == nil && exists && !taskTerminal(record.State) {
		// Defensive: a live task must never lose its supervision state.
		return errors.New("refusing to destroy local-process state of a running task")
	}
	_ = os.RemoveAll(runtime.stateDir(id))
	if runtime.Environments != nil && cgroupV2Root() != "" {
		removeCgroup(filepath.Join(cgroupV2Root(), "mmdash-task-"+id))
	}
	return nil
}

// Probe proves the interpreter, the runner re-execution surface and the
// platform enforcement before the Runtime is advertised to Core. A host that
// cannot enforce the frozen limits does not advertise this Runtime at all.
func (runtime *Runtime) Probe(ctx context.Context) error {
	if runtime == nil || runtime.StateDir == "" || strings.TrimSpace(runtime.Python) == "" {
		return errors.New("Local Process state directory and Python interpreter are required")
	}
	runnerBinary := runtime.RunnerBinary
	if runnerBinary == "" {
		resolved, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve local-process runner binary: %w", err)
		}
		runnerBinary = resolved
	}
	limits := contractLimits{CPUMillis: 1000, MemoryBytes: 1 << 20, PIDs: 64}
	if err := probeEnforcement(limits); err != nil {
		return err
	}
	// A trivial supervised execution proves interpreter, runner, supervision,
	// timeout bookkeeping and terminal record writing end to end.
	probeID := "mmdash-probe-" + fmt.Sprint(runtime.now().UnixNano())
	defer os.RemoveAll(runtime.stateDir(probeID))
	if err := os.MkdirAll(runtime.stateDir(probeID), 0o700); err != nil {
		return err
	}
	job := jobSpec{
		SchemaVersion: 1, TaskID: probeID,
		Command:       []string{runtime.Python, "-c", "pass"},
		Workspace:     runtime.stateDir(probeID),
		Environment:   []string{"PATH=" + os.Getenv("PATH")},
		TimeoutSecond: 30, CPUMillis: limits.CPUMillis,
		MemoryBytes: limits.MemoryBytes, PIDs: limits.PIDs,
	}
	jobData, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runtime.stateDir(probeID), "job.json"), jobData, 0o600); err != nil {
		return err
	}
	probeRunner := exec.Command(runnerBinary, RunnerSubcommand,
		"--state-dir", runtime.StateDir, "--task-id", probeID)
	probeRunner.Env = nil
	probeRunner.Stdout = io.Discard
	probeRunner.Stderr = io.Discard
	probeRunner.SysProcAttr = runnerProcessAttributes()
	if err := probeRunner.Run(); err != nil {
		return fmt.Errorf("probe local-process runner lifecycle: %w", err)
	}
	record, exists, err := loadTaskRecord(runtime.recordPath(probeID))
	if err != nil || !exists || record.State != taskStateExited {
		return errors.New("probe local-process runner did not record a terminal state")
	}
	if runtime.Environments != nil {
		_ = runtime.Environments.GC(context.WithoutCancel(ctx), runtime.now().UTC())
	}
	return nil
}

// Features reports the probed enforcement description for the Runtime
// descriptor advertised to Core.
func (runtime *Runtime) Features() []string {
	return platformRunnerFeatures()
}

func (runtime *Runtime) cgroupEnabled() bool {
	// On Windows there is no cgroup path; on Unix the runner applies the
	// cgroup subtree only when the platform reports cgroup v2 availability.
	return cgroupV2Root() != ""
}

// runnerProcessAttributes detaches the runner from the Gateway process group
// on Unix. Windows runners are separate processes by construction.
func runnerProcessAttributes() *syscall.SysProcAttr {
	return platformRunnerProcessAttributes()
}
