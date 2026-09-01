package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/repo/egress"
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

func TestClientStreamsBoundedObjectBytes(t *testing.T) {
	client, err := NewClient(os.Args[0], "askpass", 2*time.Second, 1, 16)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	var output bytes.Buffer
	result, err := client.RunStream(context.Background(), Command{
		Args:      []string{"-test.run=TestGitClientHelper", "--", "large"},
		Directory: t.TempDir(), Operation: "test.stream",
	}, &output, 1024)
	if err != nil || result.Bytes != 1024 || output.Len() != 1024 {
		t.Fatalf("stream result: %#v size=%d err=%v", result, output.Len(), err)
	}
	output.Reset()
	if _, err := client.RunStream(context.Background(), Command{
		Args:      []string{"-test.run=TestGitClientHelper", "--", "large"},
		Directory: t.TempDir(), Operation: "test.stream.limit",
	}, &output, 100); !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("stream limit was not enforced: %v", err)
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

func TestClientInjectsOnlyReviewedRepoProxyVariablesAndRedactsCredentials(t *testing.T) {
	proxy, err := egress.Parse(
		"http://proxy-user:proxy-password@127.0.0.1:22334",
		"localhost,127.0.0.1,::1",
	)
	if err != nil {
		t.Fatalf("parse proxy: %v", err)
	}
	client, err := NewClient(os.Args[0], "askpass", time.Second, 1, 4096)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.environment = func(key string) (string, bool) {
		values := map[string]string{
			"HTTP_PROXY":  "http://untrusted-process-proxy:8080",
			"HTTPS_PROXY": "http://untrusted-process-proxy:8080",
			"ALL_PROXY":   "socks5://untrusted-process-proxy:1080",
			"NO_PROXY":    "github.com",
		}
		value, ok := values[key]
		return value, ok
	}
	environment := environmentMap(t, client.commandEnvironment(Command{
		Credentials: &Credentials{Proxy: proxy, Token: "github-secret"},
	}))
	if environment["HTTP_PROXY"] != "http://proxy-user:proxy-password@127.0.0.1:22334" ||
		environment["HTTPS_PROXY"] != environment["HTTP_PROXY"] ||
		environment["http_proxy"] != environment["HTTP_PROXY"] ||
		environment["https_proxy"] != environment["HTTPS_PROXY"] ||
		environment["NO_PROXY"] != "localhost,127.0.0.1,::1" ||
		environment["no_proxy"] != environment["NO_PROXY"] {
		t.Fatalf("unexpected controlled proxy environment: %#v", environment)
	}
	if _, exists := environment["ALL_PROXY"]; exists ||
		strings.Contains(strings.Join(client.commandEnvironment(Command{}), "\n"), "untrusted-process-proxy") {
		t.Fatalf("process proxy environment leaked into Git: %#v", environment)
	}

	result, err := client.Run(context.Background(), Command{
		Args: []string{"-test.run=TestGitClientHelper", "--", "proxy"},
		Credentials: &Credentials{
			Proxy: proxy, Token: "github-secret",
		},
		Directory: t.TempDir(), Operation: "test.proxy",
	})
	if err != nil {
		t.Fatalf("run proxy helper: %v", err)
	}
	if strings.Contains(string(result.Stdout), "proxy-password") ||
		strings.Contains(string(result.Stdout), "github-secret") ||
		!strings.Contains(string(result.Stdout), "[REDACTED]") {
		t.Fatalf("proxy credentials were not redacted: %q", result.Stdout)
	}
	result, err = client.Run(context.Background(), Command{
		Args: []string{"-test.run=TestGitClientHelper", "--", "proxy-failure"},
		Credentials: &Credentials{
			Proxy: proxy, Token: "github-secret",
		},
		Directory: t.TempDir(), Operation: "test.proxy-failure",
	})
	if err == nil || strings.Contains(err.Error(), "proxy-password") ||
		strings.Contains(err.Error(), "github-secret") ||
		strings.Contains(string(result.Stderr), "proxy-password") ||
		strings.Contains(string(result.Stderr), "github-secret") {
		t.Fatalf("proxy failure leaked credentials: err=%v stderr=%q", err, result.Stderr)
	}
}

func TestClientClassifiesSafeGitFailures(t *testing.T) {
	client, err := NewClient(os.Args[0], "askpass", time.Second, 1, 4096)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	for _, test := range []struct {
		mode string
		want error
	}{
		{mode: "auth", want: ErrAuthentication},
		{mode: "branch", want: ErrBranchNotFound},
		{mode: "network", want: ErrNetworkUnavailable},
		{mode: "not-found", want: ErrRemoteNotFound},
		{mode: "provider", want: ErrProviderUnavailable},
	} {
		t.Run(test.mode, func(t *testing.T) {
			_, err := client.Run(context.Background(), Command{
				Args:      []string{"-test.run=TestGitClientHelper", "--", test.mode},
				Directory: t.TempDir(), Operation: "test." + test.mode,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("unexpected classification: %v", err)
			}
		})
	}
}

func TestRealGitLsRemoteUsesTheRepoProxy(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable is unavailable")
	}
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	remote := filepath.Join(root, "repo.git")
	runGitTestCommand(t, gitPath, root, "init", "seed")
	runGitTestCommand(t, gitPath, seed, "config", "user.name", "Repo Test")
	runGitTestCommand(t, gitPath, seed, "config", "user.email", "repo-test@example.test")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("proxy test\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runGitTestCommand(t, gitPath, seed, "add", "README.md")
	runGitTestCommand(t, gitPath, seed, "commit", "-m", "test: proxy")
	runGitTestCommand(t, gitPath, seed, "branch", "-M", "main")
	runGitTestCommand(t, gitPath, root, "init", "--bare", "repo.git")
	runGitTestCommand(t, gitPath, seed, "remote", "add", "origin", remote)
	runGitTestCommand(t, gitPath, seed, "push", "origin", "main")
	runGitTestCommand(t, gitPath, root, "--git-dir="+remote, "update-server-info")

	var requests atomic.Int64
	fileServer := http.FileServer(http.Dir(root))
	proxyServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		fileServer.ServeHTTP(response, request)
	}))
	defer proxyServer.Close()
	proxy, err := egress.Parse(proxyServer.URL, "localhost,127.0.0.1,::1")
	if err != nil {
		t.Fatalf("parse proxy: %v", err)
	}
	client, err := NewClient(gitPath, "true", 10*time.Second, 1, 64*1024)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	result, err := client.Run(context.Background(), Command{
		Args: []string{"ls-remote", "http://git.example.test/repo.git"},
		Credentials: &Credentials{
			Proxy: proxy, Token: "unused-public-token",
		},
		Directory: root, Operation: "test.real-git-proxy",
	})
	if err != nil {
		t.Fatalf("ls-remote through proxy: %v stderr=%s", err, result.Stderr)
	}
	if requests.Load() == 0 || !strings.Contains(string(result.Stdout), "refs/heads/main") {
		t.Fatalf("Git did not use the fake proxy: requests=%d output=%s", requests.Load(), result.Stdout)
	}
}

func runGitTestCommand(t *testing.T, gitPath, directory string, args ...string) {
	t.Helper()
	command := exec.Command(gitPath, args...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
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
	case "auth":
		_, _ = fmt.Fprint(os.Stderr, "fatal: Authentication failed")
		os.Exit(1)
	case "branch":
		_, _ = fmt.Fprint(os.Stderr, "fatal: couldn't find remote ref missing")
		os.Exit(1)
	case "network":
		_, _ = fmt.Fprint(os.Stderr, "fatal: unable to access: Could not resolve host: github.com")
		os.Exit(1)
	case "not-found":
		_, _ = fmt.Fprint(os.Stderr, "remote: Repository not found")
		os.Exit(1)
	case "provider":
		_, _ = fmt.Fprint(os.Stderr, "fatal: The requested URL returned error: 503")
		os.Exit(1)
	case "proxy":
		_, _ = fmt.Fprintf(
			os.Stdout,
			"%s\n%s\n",
			os.Getenv("HTTP_PROXY"),
			os.Getenv("MMDASH_GIT_TOKEN"),
		)
	case "proxy-failure":
		_, _ = fmt.Fprintf(
			os.Stderr,
			"proxy=%s token=%s",
			os.Getenv("HTTP_PROXY"),
			os.Getenv("MMDASH_GIT_TOKEN"),
		)
		os.Exit(1)
	case "secret":
		_, _ = fmt.Fprint(os.Stdout, "top-secret")
	case "large":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("x", 1024))
	case "sleep":
		time.Sleep(time.Second)
	}
	os.Exit(0)
}
