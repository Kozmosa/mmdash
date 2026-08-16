package gateway

import (
	"context"
	"encoding/json"
	"errors"
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
	offline       bool
	statuses      []string
	logs          []contracts.LogEntry
	logsTruncated bool
	result        bool
	uploaded      bool
	resumeCalls   int
	registerCalls int
	devicePolls   int
	accepted      int64
}

func (client *recordingCore) temporary() error {
	if client.offline {
		return transientCoreError{}
	}
	return nil
}

func (client *recordingCore) StartDeviceAuthorization(context.Context) (contracts.DeviceAuthorization, error) {
	return contracts.DeviceAuthorization{DeviceCode: "device", UserCode: "ABCD", VerificationURI: "https://example.test/device", ExpiresAt: time.Now().Add(time.Minute), Interval: 1}, nil
}
func (client *recordingCore) ExchangeDeviceAuthorization(context.Context, string) (contracts.BoxRegistrationGrant, error) {
	client.devicePolls++
	return contracts.BoxRegistrationGrant{RegistrationGrant: "grant", GrantExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (client *recordingCore) Register(context.Context, string, RegistrationInput) (Registration, error) {
	client.registerCalls++
	return Registration{BoxID: "box-1", Token: "box-token"}, nil
}
func (client *recordingCore) Heartbeat(context.Context, string, string, RegistrationInput, contracts.Load) error {
	return client.temporary()
}
func (client *recordingCore) Claim(context.Context, string, string, time.Duration) (*contracts.Task, error) {
	return nil, client.temporary()
}
func (client *recordingCore) Resume(context.Context, string, string, string, contracts.ResumeRequest) (contracts.Resume, error) {
	client.resumeCalls++
	if err := client.temporary(); err != nil {
		return contracts.Resume{}, err
	}
	return contracts.Resume{Action: "continue", AcceptedPhase: "running", AcceptedThroughSequence: client.accepted}, nil
}
func (client *recordingCore) Logs(_ context.Context, _, _, _ string, batch contracts.LogBatch) (contracts.LogAcknowledgement, error) {
	if err := client.temporary(); err != nil {
		return contracts.LogAcknowledgement{}, err
	}
	client.logs = append(client.logs, batch.Entries...)
	client.logsTruncated = client.logsTruncated || batch.LogsTruncated
	if len(batch.Entries) > 0 {
		client.accepted = batch.Entries[len(batch.Entries)-1].Sequence
	}
	return contracts.LogAcknowledgement{AcceptedThroughSequence: client.accepted}, nil
}
func (client *recordingCore) Status(_ context.Context, _, _, _, _, _, status string, _ time.Time, _ *int, _ *contracts.Failure, _ map[string]interface{}, _ string) error {
	if err := client.temporary(); err != nil {
		return err
	}
	client.statuses = append(client.statuses, status)
	return nil
}
func (client *recordingCore) Result(context.Context, string, string, string, string, string, contracts.ArtifactPointer) error {
	if err := client.temporary(); err != nil {
		return err
	}
	client.result = true
	return nil
}
func (client *recordingCore) UploadArtifact(_ context.Context, _, _, _, _ string, input io.Reader, size int64, sha string) (contracts.ArtifactPointer, error) {
	if err := client.temporary(); err != nil {
		return contracts.ArtifactPointer{}, err
	}
	contents, err := io.ReadAll(input)
	if err != nil || int64(len(contents)) != size || sha == "" {
		return contracts.ArtifactPointer{}, io.ErrUnexpectedEOF
	}
	client.uploaded = true
	return contracts.ArtifactPointer{ArtifactID: "artifact-1", VersionID: "version-1", Filename: "execution-bundle.zip", SHA256: sha, Size: size}, nil
}

type transientCoreError struct{}

func (transientCoreError) Error() string   { return "temporary Core failure" }
func (transientCoreError) Temporary() bool { return true }

type recordingRuntime struct{ output string }

func (runtime recordingRuntime) Run(_ context.Context, request sandbox.RunRequest) (sandbox.RunResult, error) {
	if _, err := request.Stdout.Write([]byte(runtime.output)); err != nil {
		return sandbox.RunResult{}, err
	}
	if err := os.WriteFile(filepath.Join(request.OutputDir, "summary.md"), []byte("summary"), 0o600); err != nil {
		return sandbox.RunResult{}, err
	}
	return sandbox.RunResult{ResourceUsage: map[string]interface{}{}}, nil
}
func (recordingRuntime) Cancel(context.Context, string) error  { return nil }
func (recordingRuntime) Destroy(context.Context, string) error { return nil }

func TestGatewayExecutesAndSubmitsDurableBundle(t *testing.T) {
	client := &recordingCore{}
	gateway := newTestGateway(t, client, 1<<20)
	task := validTask()
	gateway.state.Tasks[task.TaskID] = &taskState{Task: task, Phase: "preparing", NextSequence: 1}
	if err := gateway.persist(); err != nil {
		t.Fatal(err)
	}
	if err := gateway.execute(context.Background(), task.TaskID); err != nil {
		t.Fatal(err)
	}
	if !client.uploaded || !client.result {
		t.Fatalf("bundle/result were not submitted: %#v", client)
	}
	if strings.Join(client.statuses, ",") != "preparing,running,uploading" {
		t.Fatalf("unexpected statuses: %#v", client.statuses)
	}
	if len(client.logs) != 1 || client.logs[0].Sequence != 1 || client.logs[0].Stream != "stdout" {
		t.Fatalf("ordered logs were not submitted: %#v", client.logs)
	}
	if _, exists := gateway.state.Tasks[task.TaskID]; exists {
		t.Fatal("acknowledged task remained in the durable spool")
	}
}

func TestGatewayKeepsRuntimeResultWhileCoreIsOfflineAndReplaysAfterRestart(t *testing.T) {
	client := &recordingCore{offline: true}
	gateway := newTestGateway(t, client, 1<<20)
	task := validTask()
	gateway.state.Tasks[task.TaskID] = &taskState{Task: task, Phase: "preparing", NextSequence: 1}
	if err := gateway.execute(context.Background(), task.TaskID); err != nil {
		t.Fatal(err)
	}
	local := gateway.state.Tasks[task.TaskID]
	if local == nil || !local.RuntimeFinished || local.BundlePath == "" || local.Artifact != nil {
		t.Fatalf("offline result was not durably retained: %#v", local)
	}
	statePath := gateway.Config.StatePath
	client.offline = false
	restarted := newTestGatewayAt(t, client, 1<<20, statePath, gateway.Config.OutputRoot)
	restarted.syncAll(context.Background())
	if !client.uploaded || !client.result {
		t.Fatal("restarted Gateway did not replay the retained result")
	}
	if len(restarted.state.Tasks) != 0 {
		t.Fatalf("acknowledged replay remained in state: %#v", restarted.state.Tasks)
	}
}

func TestGatewayTruncatesOnlyNewLogsAtLocalBudget(t *testing.T) {
	client := &recordingCore{offline: true}
	gateway := newTestGateway(t, client, 4)
	gateway.Runtime = func(contracts.RunSpec) (sandbox.Runtime, error) { return recordingRuntime{output: "123456789"}, nil }
	task := validTask()
	gateway.state.Tasks[task.TaskID] = &taskState{Task: task, Phase: "preparing", NextSequence: 1}
	if err := gateway.execute(context.Background(), task.TaskID); err != nil {
		t.Fatal(err)
	}
	local := gateway.state.Tasks[task.TaskID]
	if local == nil || !local.RuntimeFinished || !local.LogsTruncated || len(local.Logs) != 0 {
		t.Fatalf("runtime did not finish with truncated logs: %#v", local)
	}
	client.offline = false
	gateway.syncAll(context.Background())
	if !client.logsTruncated || !client.result {
		t.Fatalf("truncation/result were not replayed: %#v", client)
	}
}

func TestGatewayUsesDeviceAuthorizationForFirstRegistration(t *testing.T) {
	client := &recordingCore{}
	gateway := newTestGateway(t, client, 1<<20)
	gateway.Config.BoxID, gateway.Config.BoxToken = "", ""
	gateway.state.BoxID, gateway.state.BoxToken = "", ""
	if err := gateway.ensureIdentity(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.devicePolls != 1 || client.registerCalls != 1 || gateway.Config.BoxID != "box-1" {
		t.Fatalf("device registration flow was not completed: %#v", client)
	}
	restored, err := loadState(gateway.Config.StatePath)
	if err != nil || restored.BoxToken != "box-token" || restored.InstallationID == "" {
		t.Fatalf("Box identity was not persisted: %#v %v", restored, err)
	}
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
}

func newTestGateway(t *testing.T, client *recordingCore, logBudget int64) *Gateway {
	root := t.TempDir()
	return newTestGatewayAt(t, client, logBudget, filepath.Join(root, "state.json"), filepath.Join(root, "outputs"))
}

func newTestGatewayAt(t *testing.T, client *recordingCore, logBudget int64, statePath, outputRoot string) *Gateway {
	t.Helper()
	workspace := t.TempDir()
	commit := strings.Repeat("a", 40)
	if err := os.WriteFile(filepath.Join(workspace, ".mmdash-commit"), []byte(commit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gateway := &Gateway{
		Client: client,
		Config: Config{
			Name: "test-box", Version: "test", BoxID: "box-1", BoxToken: "box-token",
			StatePath: statePath, OutputRoot: outputRoot, LogBudgetBytes: logBudget,
			Capabilities: []contracts.Capability{{Name: "sandbox", Version: "2"}},
			Runtimes:     []contracts.Runtime{{Name: "local-docker", Version: "1"}},
			Limits:       contracts.ResourceLimits{CPUMillis: 1000, MemoryBytes: 1 << 30, TimeoutSecond: 60, DiskBytes: 1 << 30, PIDs: 64, Network: "disabled"},
		},
		Workspace: StaticWorkspace{Root: workspace, Commit: commit},
		Runtime:   func(contracts.RunSpec) (sandbox.Runtime, error) { return recordingRuntime{output: "hello\n"}, nil },
		Stdout:    io.Discard, Stderr: io.Discard,
	}
	if err := gateway.initialize(); err != nil {
		t.Fatal(err)
	}
	return gateway
}

func validTask() contracts.Task {
	spec := validRunSpec("experiment-1")
	data, _ := json.Marshal(spec)
	runSpec := map[string]interface{}{}
	_ = json.Unmarshal(data, &runSpec)
	return contracts.Task{
		TaskID: "task-1", ExperimentID: spec.ExperimentID, ProjectID: spec.ProjectID,
		BoxID: "box-1", ExecutionEpoch: spec.ExecutionEpoch, Status: "preparing", Attempt: 1,
		RunSpec: runSpec, ActualRuntime: spec.Runtime, RuntimeVersion: spec.RuntimeVersion,
	}
}

func validRunSpec(experimentID string) contracts.RunSpec {
	return contracts.RunSpec{
		SchemaVersion: "2", ExperimentID: experimentID, ProjectID: "project-1", ExecutionEpoch: "epoch-1",
		SourceCommit:   strings.Repeat("a", 40),
		SourceTransfer: contracts.SourceTransfer{URL: "https://example.test/source.zip", ExpiresAt: time.Now().Add(time.Hour), SourceCommit: strings.Repeat("a", 40)},
		Entrypoint:     "python:run.py", Parameters: map[string]interface{}{}, Environment: map[string]string{}, Inputs: map[string]interface{}{},
		Runtime: "local-docker", RuntimeVersion: "1",
		Limits:         contracts.ResourceLimits{CPUMillis: 500, MemoryBytes: 1 << 20, TimeoutSecond: 30, DiskBytes: 1 << 20, PIDs: 32, Network: "disabled"},
		ResultContract: contracts.ResultContract{Directory: "experiments/experiment-1_20260815_1200", BundleFilename: "execution-bundle.zip", ManifestSchema: "mmdash.manifest.v2", MaxBundleBytes: 1 << 20},
	}
}

func TestRetryClassification(t *testing.T) {
	if !isRetryableCoreError(transientCoreError{}) || isRetryableCoreError(errors.New("permanent")) {
		t.Fatal("Core error retry classification is incorrect")
	}
}
