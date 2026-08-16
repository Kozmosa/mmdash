package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/box/contracts"
)

func TestEntrypointCommandRejectsShellAndTraversal(t *testing.T) {
	if _, err := EntrypointCommand("python:script.py && whoami"); err == nil {
		t.Fatal("shell syntax was accepted")
	}
	if _, err := EntrypointCommand("python:../script.py"); err == nil {
		t.Fatal("traversal was accepted")
	}
	command, err := EntrypointCommand("python:experiments/run.py")
	if err != nil || len(command) != 2 {
		t.Fatalf("fixed command: %#v %v", command, err)
	}
}

func TestManifestPathAndHashValidation(t *testing.T) {
	if err := contracts.ValidateRelativePath("../secret"); err == nil {
		t.Fatal("zip-slip path was accepted")
	}
	now := time.Now().UTC()
	manifest := contracts.Manifest{
		SchemaVersion: "2", ExperimentID: "exp", SourceCommit: strings.Repeat("a", 40),
		ResultDirectory: "experiments/exp_20260815_1200", Status: "succeeded",
		StartedAt: now, FinishedAt: now.Add(time.Second), Runtime: "local-docker", RuntimeVersion: "1",
		Files: []contracts.ManifestFile{{Path: "summary.md", SHA256: contracts.SHA256([]byte("ok")), Size: 2, Kind: "summary"}},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := (Capability{}).Run(context.Background(), RunRequest{Spec: contracts.RunSpec{}}); err == nil {
		t.Fatal("unconfigured runtime executed")
	}
}
