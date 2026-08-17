package localdocker

import (
	"strings"
	"testing"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
)

func TestBuildArgsKeepsAllDockerOptionsBeforeImage(t *testing.T) {
	spec := contracts.RunSpec{
		SchemaVersion: "1", ExperimentID: "exp", ProjectID: "project",
		SourceCommit: strings.Repeat("a", 40), Entrypoint: "python:run.py",
		Parameters: map[string]interface{}{}, Environment: map[string]string{"MODE": "test"},
		Inputs: map[string]interface{}{}, Runtime: "local-docker",
		Limits: contracts.ResourceLimits{CPUMillis: 500, MemoryBytes: 1 << 20, TimeoutSecond: 30, DiskBytes: 1 << 20, PIDs: 32, Network: "disabled"},
	}
	args, err := buildArgs("sandbox:fixed", "1000:1000", sandbox.RunRequest{ID: "task-1", Spec: spec, Workspace: "/workspace", OutputDir: "/output"}, []string{"python3", "/workspace/run.py"})
	if err != nil {
		t.Fatal(err)
	}
	imageIndex := indexOf(args, "sandbox:fixed")
	if imageIndex < 0 {
		t.Fatal("image missing")
	}
	for _, expected := range []string{"create", "--name", "mmdash-task-task-1"} {
		if indexOf(args, expected) < 0 {
			t.Fatalf("durable container argument %q is missing: %#v", expected, args)
		}
	}
	if indexOf(args, "--rm") >= 0 {
		t.Fatalf("durable task container must not use --rm: %#v", args)
	}
	for _, option := range []string{"--env", "MODE=test", "--network", "none", "--storage-opt", "size=1048576", "--user", "1000:1000"} {
		if indexOf(args, option) > imageIndex {
			t.Fatalf("Docker option %q appears after image: %#v", option, args)
		}
	}
	if indexOf(args, "--entrypoint") < 0 || args[indexOf(args, "--entrypoint")+1] != "python3" {
		t.Fatalf("fixed executable was not installed as the Docker entrypoint: %#v", args)
	}
	if got := strings.Join(args[imageIndex+1:], " "); got != "/workspace/run.py" {
		t.Fatalf("unexpected fixed command: %s", got)
	}
}

func TestBuildArgsRejectsEmptyCommand(t *testing.T) {
	if _, err := buildArgs("sandbox:fixed", "", sandbox.RunRequest{}, nil); err == nil {
		t.Fatal("empty Sandbox command was accepted")
	}
}

func TestBuildArgsRejectsUnsafeEnvironmentNames(t *testing.T) {
	spec := contracts.RunSpec{
		SchemaVersion: "1", ExperimentID: "exp", ProjectID: "project",
		SourceCommit: strings.Repeat("a", 40), Entrypoint: "python:run.py",
		Parameters: map[string]interface{}{}, Environment: map[string]string{"BAD=NAME": "value"},
		Inputs: map[string]interface{}{}, Runtime: "local-docker",
		Limits: contracts.ResourceLimits{CPUMillis: 500, MemoryBytes: 1 << 20, TimeoutSecond: 30, DiskBytes: 1 << 20, PIDs: 32, Network: "disabled"},
	}
	if _, err := buildArgs("sandbox:fixed", "", sandbox.RunRequest{ID: "task-1", Spec: spec, Workspace: "/workspace", OutputDir: "/output"}, []string{"python3", "/workspace/run.py"}); err == nil {
		t.Fatal("unsafe environment name was accepted")
	}
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}
