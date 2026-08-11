package sandbox

import (
	"context"
	"testing"

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
	manifest := contracts.Manifest{SchemaVersion: "1", ExperimentID: "exp", Status: "succeeded", Files: []contracts.ManifestFile{{Path: "summary.md", SHA256: contracts.SHA256([]byte("ok")), Size: 2, Kind: "summary"}}}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := (Capability{}).Run(context.Background(), RunRequest{Spec: contracts.RunSpec{}}); err == nil {
		t.Fatal("unconfigured runtime executed")
	}
}
