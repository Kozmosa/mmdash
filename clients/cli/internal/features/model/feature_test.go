package model

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mmdash/mmdash/clients/cli/internal/app"
)

func TestFeatureRegistersHumanModelCommands(t *testing.T) {
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
	for _, command := range []string{"model list", "model show", "model sync"} {
		if !strings.Contains(stdout.String(), command) {
			t.Fatalf("help does not contain %q:\n%s", command, stdout.String())
		}
	}
}

func TestModelCommandsRejectInvalidArgumentCounts(t *testing.T) {
	runtime := &app.Runtime{}
	if err := (listCommand{}).Run(context.Background(), runtime, []string{"extra"}); err == nil {
		t.Fatal("model list accepted an argument")
	}
	if err := (showCommand{}).Run(context.Background(), runtime, nil); err == nil {
		t.Fatal("model show accepted a missing question ID")
	}
	if err := (syncCommand{}).Run(context.Background(), runtime, []string{"one", "two"}); err == nil {
		t.Fatal("model sync accepted two question IDs")
	}
}
