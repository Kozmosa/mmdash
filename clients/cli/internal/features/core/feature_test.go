package core

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/mmdash/mmdash/clients/cli/internal/app"
	"github.com/mmdash/mmdash/clients/cli/internal/config"
	"github.com/mmdash/mmdash/clients/cli/internal/output"
)

func TestSetDomainCommandPersistsUnifiedLoopbackEndpoints(t *testing.T) {
	for _, name := range []string{"MMDASH_URL", "MMDASH_CORE_URL", "MMDASH_MCP_URL"} {
		t.Setenv(name, "")
	}
	directory := t.TempDir()
	paths := config.Paths{
		ConfigDir:  directory,
		ConfigFile: filepath.Join(directory, "config.json"),
		StateDir:   directory,
	}
	var stdout bytes.Buffer
	runtime := &app.Runtime{
		Config:  config.Default(func(string) string { return "" }),
		Paths:   paths,
		Printer: output.Printer{Stderr: &bytes.Buffer{}, Stdout: &stdout},
	}
	if err := (setDomainCommand{}).Run(context.Background(), runtime, []string{"localhost:3000"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ServerURL != "http://localhost:3000" || loaded.CoreURL != loaded.ServerURL || loaded.MCPURL != loaded.ServerURL+"/mcp" {
		t.Fatalf("unexpected persisted endpoints: %#v", loaded)
	}
	if stdout.Len() == 0 {
		t.Fatal("expected the command to report the saved endpoints")
	}
}
