package repo

import (
	"context"
	"strings"
	"testing"
)

func TestWebhookSchemaCheckerRequiresDatabase(t *testing.T) {
	err := (WebhookSchemaChecker{}).Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unexpected schema checker error: %v", err)
	}
}

func TestValidateGitVersion(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		output  string
		wantErr bool
	}{
		{name: "minimum", output: "git version 2.20.0"},
		{name: "current", output: "git version 2.52.0.windows.1"},
		{name: "patch suffix", output: "git version 2.39.3 (Apple Git-146)"},
		{name: "too old", output: "git version 2.19.9", wantErr: true},
		{name: "malformed", output: "unknown", wantErr: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			err := validateGitVersion(fixture.output)
			if (err != nil) != fixture.wantErr {
				t.Fatalf("validate %q: %v", fixture.output, err)
			}
		})
	}
}
