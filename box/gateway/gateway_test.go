package gateway

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
)

type recordingCore struct {
	statuses       []string
	statusCalls    int
	statusFailures int
	result         bool
	uploaded       bool
	manifest       contracts.Manifest
	exitCode       *int
	errorCode      string
}

func (client *recordingCore) Register(context.Context, string, RegistrationInput) (Registration, error) {
	return Registration{BoxID: "box-1", Token: "box-token"}, nil
}
func (client *recordingCore) Heartbeat(context.Context, string, string, RegistrationInput, contracts.Load) error {
	return nil
}
func (client *recordingCore) Claim(context.Context, string, string, time.Duration) (*contracts.Task, error) {
	return nil, nil
}
func (client *recordingCore) Renew(context.Context, string, string, string, time.Duration) (bool, error) {
	return false, nil
}
func (client *recordingCore) Log(context.Context, string, string, string, contracts.Log) error {
	return nil
}
func (client *recordingCore) Status(_ context.Context, _, _, _, status string, exitCode *int, errorCode, _ string, _ map[string]interface{}, _ string) error {
	client.statusCalls++
	if client.statusFailures > 0 {
		client.statusFailures--
		return transientCoreError{}
	}
	client.statuses = append(client.statuses, status)
	client.exitCode = exitCode
	client.errorCode = errorCode
	return nil
}

type transientCoreError struct{}

func (transientCoreError) Error() string   { return "temporary Core failure" }
func (transientCoreError) Temporary() bool { return true }
func (client *recordingCore) Result(_ context.Context, _, _, _ string, manifest contracts.Manifest, _ contracts.ArtifactPointer) error {
	client.result = true
	client.manifest = manifest
	return nil
}
func (client *recordingCore) UploadArtifact(_ context.Context, _, _, _ string, input io.Reader, size int64, sha string) (contracts.ArtifactPointer, error) {
	contents, err := io.ReadAll(input)
	if err != nil {
		return contracts.ArtifactPointer{}, err
	}
	if int64(len(contents)) != size || sha == "" {
		return contracts.ArtifactPointer{}, io.ErrUnexpectedEOF
	}
	client.uploaded = true
	return contracts.ArtifactPointer{ArtifactID: "artifact-1", VersionID: "version-1", Filename: "artifact.zip", SHA256: sha, Size: size}, nil
}

type recordingRuntime struct{}

func (recordingRuntime) Run(_ context.Context, request sandbox.RunRequest) (sandbox.RunResult, error) {
	contents := []byte("summary")
	if err := os.WriteFile(filepath.Join(request.OutputDir, "summary.md"), contents, 0o600); err != nil {
		return sandbox.RunResult{}, err
	}
	return sandbox.RunResult{}, nil
}
func (recordingRuntime) Cancel(context.Context, string) error  { return nil }
func (recordingRuntime) Destroy(context.Context, string) error { return nil }

func TestGatewayExecutesAndCleansSuccessfulTask(t *testing.T) {
	outputRoot := t.TempDir()
	client := &recordingCore{}
	gateway := Gateway{
		Client: client,
		Config: Config{BoxID: "box-1", BoxToken: "box-token", OutputRoot: outputRoot, Lease: time.Minute},
		Workspace: workspaceFunc(func(context.Context, contracts.RunSpec) (string, func(), error) {
			return t.TempDir(), func() {}, nil
		}),
		Runtime: func(contracts.RunSpec) (sandbox.Runtime, error) { return recordingRuntime{}, nil },
	}
	task := contracts.Task{TaskID: "task-1", ExperimentID: "experiment-1", ProjectID: "project-1", RunSpec: runSpecMap("experiment-1")}
	if err := gateway.execute(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if !client.uploaded || !client.result {
		t.Fatalf("artifact/result were not submitted: %#v", client)
	}
	if client.manifest.ExperimentID != task.ExperimentID || client.manifest.Status != "succeeded" || len(client.manifest.Files) != 3 {
		t.Fatalf("Gateway did not generate the authoritative manifest: %#v", client.manifest)
	}
	if strings.Join(client.statuses, ",") != "preparing,running" {
		t.Fatalf("unexpected statuses: %#v", client.statuses)
	}
	if _, err := os.Stat(filepath.Join(outputRoot, task.TaskID)); !os.IsNotExist(err) {
		t.Fatalf("task output was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputRoot, task.TaskID+".artifact.zip")); !os.IsNotExist(err) {
		t.Fatalf("artifact staging file was not removed: %v", err)
	}
}

func TestGatewayRetriesIdempotentStatusCallbacks(t *testing.T) {
	client := &recordingCore{statusFailures: 1}
	gateway := Gateway{
		Client: client,
		Config: Config{
			BoxID: "box-1", BoxToken: "box-token", OutputRoot: t.TempDir(), Lease: time.Minute,
			CallbackAttempts: 3, CallbackRetryDelay: time.Millisecond,
		},
		Workspace: workspaceFunc(func(context.Context, contracts.RunSpec) (string, func(), error) {
			return t.TempDir(), func() {}, nil
		}),
		Runtime: func(contracts.RunSpec) (sandbox.Runtime, error) { return recordingRuntime{}, nil },
	}
	task := contracts.Task{TaskID: "task-1", ExperimentID: "experiment-1", ProjectID: "project-1", RunSpec: runSpecMap("experiment-1")}
	if err := gateway.execute(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if client.statusCalls != 3 || strings.Join(client.statuses, ",") != "preparing,running" {
		t.Fatalf("transient status callback was not retried safely: calls=%d statuses=%#v", client.statusCalls, client.statuses)
	}
}

func TestGatewayDoesNotRetryPermanentCallbackFailures(t *testing.T) {
	calls := 0
	gateway := Gateway{Config: Config{CallbackAttempts: 4, CallbackRetryDelay: time.Millisecond}}
	err := gateway.retryCoreCallback(context.Background(), func() error {
		calls++
		return io.ErrUnexpectedEOF
	})
	if err == nil || calls != 1 {
		t.Fatalf("permanent callback failure was retried: calls=%d err=%v", calls, err)
	}
}

type nonZeroRuntime struct{ recordingRuntime }

func (nonZeroRuntime) Run(_ context.Context, request sandbox.RunRequest) (sandbox.RunResult, error) {
	_, err := request.Stdout.Write([]byte("failed output\n"))
	return sandbox.RunResult{ExitCode: 7}, err
}

func TestGatewayRejectsNonZeroExitWithoutSuccessArtifact(t *testing.T) {
	client := &recordingCore{}
	gateway := Gateway{
		Client: client,
		Config: Config{BoxID: "box-1", BoxToken: "box-token", OutputRoot: t.TempDir(), Lease: time.Minute},
		Workspace: workspaceFunc(func(context.Context, contracts.RunSpec) (string, func(), error) {
			return t.TempDir(), func() {}, nil
		}),
		Runtime: func(contracts.RunSpec) (sandbox.Runtime, error) { return nonZeroRuntime{}, nil },
	}
	task := contracts.Task{TaskID: "task-1", ExperimentID: "experiment-1", ProjectID: "project-1", RunSpec: runSpecMap("experiment-1")}
	if err := gateway.execute(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if client.uploaded || client.result || client.exitCode == nil || *client.exitCode != 7 || client.errorCode != "NON_ZERO_EXIT" {
		t.Fatalf("non-zero exit was not reported as a terminal failure: %#v", client)
	}
	if strings.Join(client.statuses, ",") != "preparing,running,failed" {
		t.Fatalf("unexpected statuses: %#v", client.statuses)
	}
}

type workspaceFunc func(context.Context, contracts.RunSpec) (string, func(), error)

func (function workspaceFunc) Prepare(ctx context.Context, spec contracts.RunSpec) (string, func(), error) {
	return function(ctx, spec)
}

func TestStaticWorkspaceRequiresTheRepoCommitMarker(t *testing.T) {
	root := t.TempDir()
	spec := validRunSpec("experiment-1")
	provider := StaticWorkspace{Root: root, Commit: spec.SourceCommit}
	if _, _, err := provider.Prepare(context.Background(), spec); err == nil {
		t.Fatal("workspace without a commit marker was accepted")
	}
	if err := os.WriteFile(filepath.Join(root, ".mmdash-commit"), []byte(spec.SourceCommit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, cleanup, err := provider.Prepare(context.Background(), spec); err != nil || cleanup == nil {
		t.Fatalf("pinned workspace rejected: %v", err)
	}
	other := spec
	other.SourceCommit = strings.Repeat("b", 40)
	if _, _, err := provider.Prepare(context.Background(), other); err == nil {
		t.Fatal("different frozen commit was accepted")
	}
}

func validRunSpec(experimentID string) contracts.RunSpec {
	return contracts.RunSpec{
		SchemaVersion: "1", ExperimentID: experimentID, ProjectID: "project-1",
		SourceCommit: strings.Repeat("a", 40), Entrypoint: "python:run.py",
		Parameters: map[string]interface{}{}, Environment: map[string]string{}, Inputs: map[string]interface{}{}, Runtime: "local-docker",
		Limits: contracts.ResourceLimits{CPUMillis: 500, MemoryBytes: 1 << 20, TimeoutSecond: 30, DiskBytes: 1 << 20, PIDs: 32, Network: "disabled"},
	}
}

func runSpecMap(experimentID string) map[string]interface{} {
	spec := validRunSpec(experimentID)
	return map[string]interface{}{
		"schema_version": spec.SchemaVersion, "experiment_id": spec.ExperimentID, "project_id": spec.ProjectID,
		"source_commit": spec.SourceCommit, "entrypoint": spec.Entrypoint,
		"parameters": spec.Parameters, "environment": spec.Environment, "inputs": spec.Inputs, "runtime": spec.Runtime,
		"limits": map[string]interface{}{
			"cpu_millis": spec.Limits.CPUMillis, "memory_bytes": spec.Limits.MemoryBytes, "timeout_seconds": spec.Limits.TimeoutSecond,
			"disk_bytes": spec.Limits.DiskBytes, "pids": spec.Limits.PIDs, "network": spec.Limits.Network,
		},
	}
}
