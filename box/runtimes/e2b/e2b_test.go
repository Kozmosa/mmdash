package e2b

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
)

type fakeClient struct {
	template  string
	request   sandbox.RunRequest
	canceled  string
	destroyed string
	probed    string
}

func (client *fakeClient) Run(_ context.Context, template string, request sandbox.RunRequest) (sandbox.RunResult, error) {
	client.template, client.request = template, request
	return sandbox.RunResult{ExitCode: 7}, nil
}
func (client *fakeClient) Cancel(_ context.Context, id string) error {
	client.canceled = id
	return nil
}
func (client *fakeClient) Destroy(_ context.Context, id string) error {
	client.destroyed = id
	return nil
}
func (client *fakeClient) Probe(_ context.Context, template string) error {
	client.probed = template
	return nil
}

func TestRuntimePreservesSandboxSemanticsWithoutLeakingProviderFields(t *testing.T) {
	client := &fakeClient{}
	runtime := Runtime{Template: "python-sandbox-v1", Client: client}
	request := sandbox.RunRequest{
		ID:        "task-1",
		Spec:      contracts.RunSpec{SchemaVersion: "2", ExperimentID: "exp-1", ProjectID: "project-1", ExecutionEpoch: "epoch-1", SourceCommit: strings.Repeat("a", 40), SourceTransfer: contracts.SourceTransfer{URL: "https://example.test/source.zip", ExpiresAt: time.Now().Add(time.Hour), SourceCommit: strings.Repeat("a", 40)}, Entrypoint: "python:run.py", Runtime: "e2b", RuntimeVersion: "1", Limits: contracts.ResourceLimits{CPUMillis: 1, MemoryBytes: 1 << 20, TimeoutSecond: 1, DiskBytes: 1 << 20, PIDs: 1, Network: "disabled"}, ResultContract: contracts.ResultContract{Directory: "experiments/exp-1_20260815_1200/", BundleFilename: "execution-bundle.zip", ManifestSchema: "https://mmdash.moe/contracts/manifest.schema.json", MaxBundleBytes: 1 << 20}},
		Workspace: "/workspace", OutputDir: "/output",
	}
	result, err := runtime.Run(context.Background(), request)
	if err != nil || result.ExitCode != 7 {
		t.Fatalf("run result: %#v %v", result, err)
	}
	if client.template != "python-sandbox-v1" || client.request.ID != request.ID || client.request.Spec.Entrypoint != request.Spec.Entrypoint {
		t.Fatalf("provider did not receive the stable sandbox request: %#v", client)
	}
	if err := runtime.Cancel(context.Background(), request.ID); err != nil || client.canceled != request.ID {
		t.Fatalf("cancel: %v %q", err, client.canceled)
	}
	if err := runtime.Destroy(context.Background(), request.ID); err != nil || client.destroyed != request.ID {
		t.Fatalf("destroy: %v %q", err, client.destroyed)
	}
	if err := runtime.Probe(context.Background()); err != nil || client.probed != "python-sandbox-v1" {
		t.Fatalf("probe: %v %q", err, client.probed)
	}
}

func TestRuntimeRequiresProviderConfiguration(t *testing.T) {
	if _, err := (Runtime{Template: "template"}).Run(context.Background(), sandbox.RunRequest{}); err == nil {
		t.Fatal("unconfigured E2B client was accepted")
	}
}
