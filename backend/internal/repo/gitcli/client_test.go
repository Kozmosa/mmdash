package gitcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClientBoundsOutputRedactsCredentialsAndReportsTimeout(t *testing.T) {
	client, err := NewClient(os.Args[0], "askpass", 2*time.Second, 1, 16)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	result, err := client.Run(context.Background(), Command{
		Args:        []string{"-test.run=TestGitClientHelper", "--", "secret"},
		Credentials: &Credentials{Token: "top-secret"},
		Directory:   t.TempDir(),
		Operation:   "test.secret",
	})
	if err != nil {
		t.Fatalf("run redaction helper: %v", err)
	}
	if strings.Contains(string(result.Stdout), "top-secret") ||
		!strings.Contains(string(result.Stdout), "[REDACTED]") {
		t.Fatalf("credential was not redacted: %q", result.Stdout)
	}

	_, err = client.Run(context.Background(), Command{
		Args:      []string{"-test.run=TestGitClientHelper", "--", "large"},
		Directory: t.TempDir(),
		Operation: "test.large",
	})
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("large output should be bounded: %v", err)
	}

	_, err = client.Run(context.Background(), Command{
		Args:      []string{"-test.run=TestGitClientHelper", "--", "sleep"},
		Directory: t.TempDir(),
		Operation: "test.timeout",
		Timeout:   20 * time.Millisecond,
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("slow command should time out: %v", err)
	}
}

func TestClientUsesStableMaintenanceIdentityAndActorOverrides(t *testing.T) {
	client, err := NewClient(os.Args[0], "askpass", time.Second, 1, 1024)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.environment = func(string) (string, bool) { return "", false }

	maintenance := environmentMap(t, client.commandEnvironment(Command{}))
	for key, expected := range map[string]string{
		"GIT_AUTHOR_EMAIL":    maintenanceGitEmail,
		"GIT_AUTHOR_NAME":     maintenanceGitName,
		"GIT_COMMITTER_EMAIL": maintenanceGitEmail,
		"GIT_COMMITTER_NAME":  maintenanceGitName,
	} {
		if maintenance[key] != expected {
			t.Fatalf("unexpected maintenance %s: %q", key, maintenance[key])
		}
	}

	actor := environmentMap(t, client.commandEnvironment(Command{
		Environment: map[string]string{
			"GIT_AUTHOR_EMAIL":    "actor@example.test",
			"GIT_AUTHOR_NAME":     "Actor",
			"GIT_COMMITTER_EMAIL": "committer@example.test",
			"GIT_COMMITTER_NAME":  "Committer",
			"GIT_AUTHOR_DATE":     "2026-08-10T00:00:00Z",
		},
	}))
	for key, expected := range map[string]string{
		"GIT_AUTHOR_EMAIL":    "actor@example.test",
		"GIT_AUTHOR_NAME":     "Actor",
		"GIT_COMMITTER_EMAIL": "committer@example.test",
		"GIT_COMMITTER_NAME":  "Committer",
		"GIT_AUTHOR_DATE":     "2026-08-10T00:00:00Z",
	} {
		if actor[key] != expected {
			t.Fatalf("unexpected actor %s: %q", key, actor[key])
		}
	}
}

func environmentMap(t *testing.T, environment []string) map[string]string {
	t.Helper()
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("invalid environment entry: %q", entry)
		}
		if _, exists := values[key]; exists {
			t.Fatalf("duplicate environment key: %s", key)
		}
		values[key] = value
	}
	return values
}

func TestGitClientHelper(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	switch os.Args[separator+1] {
	case "secret":
		_, _ = fmt.Fprint(os.Stdout, "top-secret")
	case "large":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("x", 1024))
	case "sleep":
		time.Sleep(time.Second)
	}
	os.Exit(0)
}
