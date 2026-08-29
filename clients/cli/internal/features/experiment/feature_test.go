package experiment

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mmdash/mmdash/clients/cli/internal/app"
)

func TestFeatureRegistersHumanExperimentCommands(t *testing.T) {
	var stdout bytes.Buffer
	application, err := app.New(app.Options{
		Features: []app.Feature{Feature{}},
		Stderr:   &bytes.Buffer{},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Version:  "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := application.Run(context.Background(), []string{"help"}); code != 0 {
		t.Fatalf("help exit code = %d", code)
	}
	for _, command := range []string{"experiment list", "experiment create", "experiment run"} {
		if !strings.Contains(stdout.String(), command) {
			t.Fatalf("help does not contain %q:\n%s", command, stdout.String())
		}
	}
}

func TestCreateRejectsUnknownRuntimePolicy(t *testing.T) {
	runtime := &app.Runtime{}
	err := (createCommand{}).Run(context.Background(), runtime, []string{
		"box", "sweep", "0123456789012345678901234567890123456789", "python:run.py", "bare-metal",
	})
	if err == nil || !strings.Contains(err.Error(), "runtime policy must be auto, e2b, local-docker, or local-process") {
		t.Fatalf("unknown policy error = %v", err)
	}
}

func TestCreateAcceptsEveryFrozenRuntimePolicy(t *testing.T) {
	for _, policy := range []string{"local-process", "local-docker", "e2b", "auto"} {
		runtime := &app.Runtime{}
		err := (createCommand{}).Run(context.Background(), runtime, []string{
			"box", "sweep", "0123456789012345678901234567890123456789", "python:run.py", policy,
		})
		// Without a configured project session the command stops after
		// validation; a usage rejection here would mean the frozen policy
		// was not accepted.
		if err != nil && strings.Contains(err.Error(), "runtime policy must be") {
			t.Fatalf("policy %q was rejected: %v", policy, err)
		}
	}
}

func TestCreateSelfRejectsRuntimeSelection(t *testing.T) {
	runtime := &app.Runtime{}
	err := (createCommand{}).Run(context.Background(), runtime, []string{
		"self", "notebook", "0123456789012345678901234567890123456789", "python:run.py", "local-process",
	})
	if err == nil || !strings.Contains(err.Error(), "self-run experiments do not select a runtime") {
		t.Fatalf("self-run policy error = %v", err)
	}
}
