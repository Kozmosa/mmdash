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
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
)

// TestMain dispatches the re-executed supervisor and the supervised task
// helpers so the process supervision tests run without any external tooling.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == RunnerSubcommand {
		if err := RunTaskRunner(os.Args[2:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	// The probe and the interpreter version checks invoke the "python"
	// interpreter with -c; the test binary answers both without a real
	// Python installation.
	if len(os.Args) > 2 && os.Args[1] == "-c" {
		os.Exit(fakePythonMain(os.Args[2:]))
	}
	if os.Getenv("MMDASH_SLEEP_HELPER") == "1" {
		time.Sleep(120 * time.Second)
		os.Exit(0)
	}
	if os.Getenv("MMDASH_TASK_HELPER") == "1" {
		runTaskHelper()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakePythonMain emulates the interpreter invocations the environment
// pipeline and the probe issue.
func fakePythonMain(args []string) int {
	if len(args) >= 2 && args[0] == "-c" {
		if strings.Contains(args[1], "version_info") {
			fmt.Println("3.12.7")
			return 0
		}
		return 0
	}
	return 2
}

// runTaskHelper is the supervised "experiment program". Its behaviour is
// selected through the frozen task environment.
func runTaskHelper() {
	mode := os.Getenv("MMDASH_HELPER_MODE")
	switch mode {
	case "echo":
		fmt.Println("hello-from-task")
		fmt.Println("workspace=" + os.Getenv("MMDASH_WORKSPACE"))
		fmt.Println("output=" + os.Getenv("MMDASH_OUTPUT_DIR"))
		fmt.Println("experiment=" + os.Getenv("MMDASH_EXPERIMENT_ID"))
		if data, err := os.ReadFile(os.Getenv("MMDASH_PARAMETERS_FILE")); err == nil {
			fmt.Println("parameters=" + string(data))
		}
	case "echo-slow":
		fmt.Println("line-one")
		time.Sleep(1500 * time.Millisecond)
		fmt.Println("line-two")
	case "fail":
		fmt.Fprintln(os.Stderr, "deliberate failure")
		os.Exit(7)
	case "tree":
		// Spawn an own child so the supervision covers grandchildren. The
		// child gets a minimal environment so TestMain dispatches it to the
		// sleep helper instead of the task helper.
		childPidFile := os.Getenv("MMDASH_HELPER_CHILD_PID_FILE")
		childEnv := []string{"MMDASH_SLEEP_HELPER=1", "PATH=" + os.Getenv("PATH")}
		if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
			childEnv = append(childEnv, "SystemRoot="+systemRoot, "SystemDrive="+os.Getenv("SystemDrive"))
		}
		command := exec.Command(os.Args[0])
		command.Env = childEnv
		if err := command.Start(); err == nil && childPidFile != "" {
			_ = os.WriteFile(childPidFile, []byte(fmt.Sprint(command.Process.Pid)), 0o600)
		}
		fmt.Println("tree-spawned")
		time.Sleep(120 * time.Second)
	}
}

func fakePython(t *testing.T, dir string) string {
	t.Helper()
	name := "python"
	if runtime.GOOS == "windows" {
		name = "python.exe"
	}
	target := filepath.Join(dir, name)
	if err := copyFile(mustTestBinary(t), target); err != nil {
		t.Fatal(err)
	}
	return target
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer output.Close()
	_, err = io.Copy(output, input)
	return err
}

// newTestRuntime wires a hermetic Runtime: the supervisor is this test binary
// (dispatched through TestMain) and the interpreter is the same binary in
// fake-python mode. Enforcement probing is stubbed because sandboxed CI hosts
// cannot guarantee cgroup or Job Object writability.
func newTestRuntime(t *testing.T) (*Runtime, string) {
	t.Helper()
	root := t.TempDir()
	goos := runtime.GOOS
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	python := fakePython(t, root)
	rt := NewRuntime(filepath.Join(root, "runner"), python, nil)
	rt.RunnerBinary = testBinary
	rt.PollInterval = 10 * time.Millisecond
	previousProbe := probeEnforcement
	probeEnforcement = func(contractLimits) error { return nil }
	t.Cleanup(func() { probeEnforcement = previousProbe })
	if goos != "windows" {
		previousRoot := cgroupRootOverride
		cgroupRootOverride = filepath.Join(root, "cgroup")
		t.Cleanup(func() { cgroupRootOverride = previousRoot })
		if err := os.MkdirAll(cgroupRootOverride, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return rt, testBinary
}

func testSpec(t *testing.T, workspace, entrypointFile string, environment map[string]string, timeout int) contracts.RunSpec {
	t.Helper()
	expires := time.Now().Add(time.Hour).UTC()
	return contracts.RunSpec{
		SchemaVersion: "2", ExperimentID: "0196c800-0000-7000-8000-000000000001",
		ProjectID: "0196c800-0000-7000-8000-000000000002",
		ExecutionEpoch: "0196c800-0000-7000-8000-000000000003",
		SourceCommit: "0000000000000000000000000000000000000001",
		SourceTransfer: contracts.SourceTransfer{
			URL: "http://127.0.0.1:1/source.zip", ExpiresAt: expires, SourceCommit: "0000000000000000000000000000000000000001",
		},
		Entrypoint: "binary:" + entrypointFile, Parameters: map[string]interface{}{"alpha": 1},
		Environment: environment, Inputs: map[string]interface{}{},
		Runtime: "local-process", RuntimeVersion: "1",
		Limits: contracts.ResourceLimits{
			CPUMillis: 1000, MemoryBytes: 1 << 28, TimeoutSecond: timeout,
			DiskBytes: 1 << 20, PIDs: 64, Network: "enabled",
		},
		ResultContract: contracts.ResultContract{
			Directory: "experiments/0196c800-0000-7000-8000-000000000001_20260828_1200/",
			BundleFilename: "execution-bundle.zip", ManifestSchema: "https://mmdash.moe/contracts/manifest.schema.json",
			MaxBundleBytes: 1 << 20,
		},
	}
}

func newRunRequest(t *testing.T, workspace string, spec contracts.RunSpec) sandbox.RunRequest {
	t.Helper()
	name := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	if len(name) > 90 {
		name = name[:90]
	}
	return sandbox.RunRequest{
		ID: "task-" + name,
		Spec: spec, Workspace: workspace,
		OutputDir: filepath.Join(workspace, "output"),
		Stdout:    &syncBuffer{}, Stderr: &syncBuffer{},
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (buffer *syncBuffer) Write(content []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buf.Write(content)
}

func (buffer *syncBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buf.String()
}

func prepareHelperWorkspace(t *testing.T, helperEnvironment map[string]string, mode string) (string, contracts.RunSpec) {
	t.Helper()
	workspace := t.TempDir()
	helperName := "task-helper"
	if runtime.GOOS == "windows" {
		helperName = "task-helper.exe"
	}
	if err := copyFile(mustTestBinary(t), filepath.Join(workspace, helperName)); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{
		"MMDASH_TASK_HELPER": "1", "MMDASH_HELPER_MODE": mode,
	}
	for name, value := range helperEnvironment {
		environment[name] = value
	}
	return workspace, testSpec(t, workspace, helperName, environment, 60)
}

var testBinaryOnce struct {
	once sync.Once
	path string
	err  error
}

func mustTestBinary(t *testing.T) string {
	t.Helper()
	testBinaryOnce.once.Do(func() {
		testBinaryOnce.path, testBinaryOnce.err = os.Executable()
	})
	if testBinaryOnce.err != nil {
		t.Fatal(testBinaryOnce.err)
	}
	return testBinaryOnce.path
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition was not met in time")
}

func TestRunStreamsOutputAndTerminalState(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	workspace, spec := prepareHelperWorkspace(t, nil, "echo")
	request := newRunRequest(t, workspace, spec)
	result, err := runtime.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.TimedOut || result.Canceled {
		t.Fatalf("unexpected result: %+v", result)
	}
	output := request.Stdout.(*syncBuffer).String()
	for _, expected := range []string{
		"hello-from-task",
		"workspace=" + workspace,
		"output=" + request.OutputDir,
		"experiment=" + spec.ExperimentID,
		"parameters=",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("task output %q misses %q", output, expected)
		}
	}
	record, exists, err := loadTaskRecord(runtime.recordPath(request.ID))
	if err != nil || !exists || record.State != taskStateExited {
		t.Fatalf("terminal record missing: %+v %v %v", record, exists, err)
	}
}

func TestRunReportsNonZeroExit(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	workspace, spec := prepareHelperWorkspace(t, nil, "fail")
	request := newRunRequest(t, workspace, spec)
	result, err := runtime.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit code %d, want 7", result.ExitCode)
	}
	if !strings.Contains(request.Stderr.(*syncBuffer).String(), "deliberate failure") {
		t.Fatal("stderr was not streamed")
	}
}

func TestRunLaunchFailureSurfacesStableError(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	workspace, spec := prepareHelperWorkspace(t, nil, "echo")
	spec.Entrypoint = "binary:missing-task-binary.exe"
	request := newRunRequest(t, workspace, spec)
	_, err := runtime.Run(context.Background(), request)
	if err == nil {
		t.Fatal("a launch failure must not surface as a zero-exit success")
	}
	var stable interface{ ErrorCode() string }
	if !errors.As(err, &stable) || stable.ErrorCode() != ErrCodeRunnerFailed {
		t.Fatalf("expected a %s failure, got %v", ErrCodeRunnerFailed, err)
	}
	if !strings.Contains(err.Error(), "missing-task-binary") {
		t.Fatalf("the launch failure reason is missing from the error: %v", err)
	}
	record, exists, recordErr := loadTaskRecord(runtime.recordPath(request.ID))
	if recordErr != nil || !exists {
		t.Fatalf("terminal record missing: %v %v", exists, recordErr)
	}
	if record.State != taskStateFailed || record.LastError == "" {
		t.Fatalf("the durable record must keep state=failed and the failure reason: %+v", record)
	}
}

func TestRunTimeoutTerminatesProcessTree(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	childPidFile := filepath.Join(t.TempDir(), "child.pid")
	workspace, spec := prepareHelperWorkspace(t, map[string]string{
		"MMDASH_HELPER_CHILD_PID_FILE": childPidFile,
	}, "tree")
	spec.Limits.TimeoutSecond = 2
	request := newRunRequest(t, workspace, spec)
	result, err := runtime.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut {
		t.Fatalf("expected timeout, got %+v", result)
	}
	data, err := os.ReadFile(childPidFile)
	if err != nil {
		t.Fatal("the task never recorded its child process")
	}
	var childPID int
	if err := json.Unmarshal(data, &childPID); err != nil {
		childPID = 0
		fmt.Sscanf(string(data), "%d", &childPID)
	}
	if childPID <= 0 {
		t.Fatalf("invalid child PID %q", string(data))
	}
	waitForCondition(t, 10*time.Second, func() bool { return !processAlive(childPID) })
}

func TestRunCancelTerminatesProcessTree(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	childPidFile := filepath.Join(t.TempDir(), "child.pid")
	workspace, spec := prepareHelperWorkspace(t, map[string]string{
		"MMDASH_HELPER_CHILD_PID_FILE": childPidFile,
	}, "tree")
	request := newRunRequest(t, workspace, spec)
	resultCh := make(chan sandbox.RunResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := runtime.Run(context.Background(), request)
		resultCh <- result
		errCh <- err
	}()
	waitForCondition(t, 10*time.Second, func() bool {
		_, err := os.Stat(childPidFile)
		return err == nil
	})
	waitForCondition(t, 10*time.Second, func() bool {
		data, _ := os.ReadFile(childPidFile)
		var pid int
		fmt.Sscanf(string(data), "%d", &pid)
		return pid > 0 && processAlive(pid)
	})
	if err := runtime.Cancel(context.Background(), request.ID); err != nil {
		t.Fatal(err)
	}
	var result sandbox.RunResult
	select {
	case result = <-resultCh:
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("cancellation did not finish the run")
	}
	if !result.Canceled {
		t.Fatalf("expected cancellation, got %+v", result)
	}
	data, _ := os.ReadFile(childPidFile)
	var childPID int
	fmt.Sscanf(string(data), "%d", &childPID)
	waitForCondition(t, 10*time.Second, func() bool { return !processAlive(childPID) })
}

func TestRunReattachAfterGatewayRestart(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	workspace, spec := prepareHelperWorkspace(t, nil, "echo-slow")
	request := newRunRequest(t, workspace, spec)
	firstDone := make(chan sandbox.RunResult, 1)
	firstErr := make(chan error, 1)
	go func() {
		result, err := runtime.Run(context.Background(), request)
		firstDone <- result
		firstErr <- err
	}()
	// Wait until the supervisor is running, then reconnect like a restarted
	// Gateway process would.
	waitForCondition(t, 10*time.Second, func() bool {
		record, exists, err := loadTaskRecord(runtime.recordPath(request.ID))
		return err == nil && exists && record.State == taskStateRunning
	})
	secondResult, secondErr := runtime.Run(context.Background(), request)
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	if secondResult.ExitCode != 0 {
		t.Fatalf("reattached run returned %+v", secondResult)
	}
	select {
	case firstResult := <-firstDone:
		if err := <-firstErr; err != nil {
			t.Fatal(err)
		}
		if firstResult.ExitCode != 0 {
			t.Fatalf("original run returned %+v", firstResult)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("original run did not finish")
	}
}

func TestRunHostRestartedStableError(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	workspace, spec := prepareHelperWorkspace(t, nil, "echo")
	request := newRunRequest(t, workspace, spec)
	if err := os.MkdirAll(runtime.stateDir(request.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveTaskRecord(runtime.recordPath(request.ID), taskRecord{
		SchemaVersion: taskStateSchemaVersion, TaskID: request.ID,
		BootID: "host-from-a-previous-session", State: taskStateRunning,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := runtime.Run(context.Background(), request)
	var stable interface{ ErrorCode() string }
	if !errors.As(err, &stable) || stable.ErrorCode() != ErrCodeHostRestarted {
		t.Fatalf("expected HOST_RESTARTED, got %v", err)
	}
}

func TestRunRunnerLostStableError(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	workspace, spec := prepareHelperWorkspace(t, nil, "echo")
	request := newRunRequest(t, workspace, spec)
	if err := os.MkdirAll(runtime.stateDir(request.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveTaskRecord(runtime.recordPath(request.ID), taskRecord{
		SchemaVersion: taskStateSchemaVersion, TaskID: request.ID,
		BootID: bootID(), TaskPID: 999_999_999, State: taskStateRunning,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := runtime.Run(context.Background(), request)
	var stable interface{ ErrorCode() string }
	if !errors.As(err, &stable) || stable.ErrorCode() != ErrCodeRunnerLost {
		t.Fatalf("expected RUNNER_LOST, got %v", err)
	}
}

func TestPrepareEnvironmentRejectsNetworkPolicy(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	workspace, spec := prepareHelperWorkspace(t, nil, "echo")
	spec.Limits.Network = "disabled"
	err := runtime.PrepareEnvironment(context.Background(), sandbox.EnvironmentRequest{
		ID: "task-network", Spec: spec, Workspace: workspace,
	})
	var stable interface{ ErrorCode() string }
	if !errors.As(err, &stable) || stable.ErrorCode() != ErrCodeLimitsNotEnforceable {
		t.Fatalf("expected LIMITS_NOT_ENFORCEABLE, got %v", err)
	}
}

func TestPrepareEnvironmentRejectsUnenforceableLimits(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	previousProbe := probeEnforcement
	probeEnforcement = func(contractLimits) error { return errors.New("no enforcement support") }
	t.Cleanup(func() { probeEnforcement = previousProbe })
	workspace, spec := prepareHelperWorkspace(t, nil, "echo")
	err := runtime.PrepareEnvironment(context.Background(), sandbox.EnvironmentRequest{
		ID: "task-limits", Spec: spec, Workspace: workspace,
	})
	var stable interface{ ErrorCode() string }
	if !errors.As(err, &stable) || stable.ErrorCode() != ErrCodeLimitsNotEnforceable {
		t.Fatalf("expected LIMITS_NOT_ENFORCEABLE, got %v", err)
	}
}

func TestProbeRunsSupervisedLifecycle(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	if err := runtime.Probe(context.Background()); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
}
