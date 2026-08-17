package mbox

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mmdash/mmdash/box/config"
	"github.com/mmdash/mmdash/box/gateway"
)

func TestSetupPersistsConfigurationAndInstallationIdentity(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	handled, err := Execute(context.Background(), []string{
		"setup", "--non-interactive", "--root", root,
		"--control-url", "http://localhost:3001", "--name", "test-box",
	}, &output, &output)
	if err != nil || !handled {
		t.Fatalf("setup: handled=%v err=%v output=%s", handled, err, output.String())
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ControlURL != "http://localhost:3001" || cfg.Name != "test-box" {
		t.Fatalf("configuration %#v", cfg)
	}
	identity, err := gateway.LoadIdentity(root + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(identity.InstallationID, "box-installation-") {
		t.Fatalf("installation identity %q", identity.InstallationID)
	}
}
