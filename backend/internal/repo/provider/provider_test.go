package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

const providerSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fakeRunner struct {
	handler func(gitcli.Command) (gitcli.Result, error)
}

func (runner fakeRunner) Run(
	_ context.Context,
	command gitcli.Command,
) (gitcli.Result, error) {
	return runner.handler(command)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestRegistryValidatesMappingsAndGitHubPATWithoutPuttingTokenInArgs(t *testing.T) {
	const token = "github_pat_secret"
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer "+token {
			t.Fatal("GitHub token missing from authorization header")
		}
		return &http.Response{
			Body: io.NopCloser(strings.NewReader(
				`{"default_branch":"main","permissions":{"push":true}}`,
			)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			StatusCode: http.StatusOK,
		}, nil
	})}
	git := fakeRunner{handler: func(command gitcli.Command) (gitcli.Result, error) {
		for _, argument := range command.Args {
			if strings.Contains(argument, token) {
				t.Fatal("token leaked into Git argv")
			}
		}
		if command.Credentials == nil || command.Credentials.Token != token {
			t.Fatal("token was not passed through AskPass credentials")
		}
		return gitcli.Result{Stdout: []byte(
			providerSHA + "\trefs/heads/main\n" +
				providerSHA + "\trefs/heads/article\n" +
				providerSHA + "\trefs/heads/result\n",
		)}, nil
	}}
	registry := NewRegistry()
	if err := registry.Register("github", GitHub{
		APIBase: "https://api.github.test", Client: httpClient,
		Git: git, RuntimeRoot: t.TempDir(),
	}); err != nil {
		t.Fatalf("register GitHub: %v", err)
	}
	connection, err := registry.Test(context.Background(), Config{
		AccessToken: token, ArticleBranch: "article", CodeBranch: "main",
		Provider: "github", RemoteURL: "https://github.com/Kozmosa/mmdash",
		ResultBranch: "result",
	})
	if err != nil || connection.DefaultBranch != "main" ||
		len(connection.Branches) != 3 {
		t.Fatalf("test GitHub: %+v, %v", connection, err)
	}

	_, err = registry.Test(context.Background(), Config{
		AccessToken: token, ArticleBranch: "main", CodeBranch: "main",
		Provider: "github", RemoteURL: "https://github.com/Kozmosa/mmdash",
		ResultBranch: "result",
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("duplicate mapping should fail: %v", err)
	}
}

func TestGitHubRuntimeResolveDoesNotCallAPIOrEnumerateRemoteHeads(t *testing.T) {
	const token = "github_pat_runtime"
	registry := NewRegistry()
	if err := registry.Register("github", GitHub{
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("runtime resolution called the GitHub API")
			return nil, errors.New("unexpected API call")
		})},
		Git: fakeRunner{handler: func(gitcli.Command) (gitcli.Result, error) {
			t.Fatal("runtime resolution enumerated remote refs")
			return gitcli.Result{}, errors.New("unexpected Git call")
		}},
	}); err != nil {
		t.Fatal(err)
	}
	connection, err := registry.Resolve(context.Background(), Config{
		AccessToken: token, ArticleBranch: "article", CodeBranch: "main",
		Provider: "github", RemoteURL: "https://github.com/Kozmosa/mmdash",
		ResultBranch: "result",
	})
	if err != nil {
		t.Fatal(err)
	}
	if connection.FetchURL != "https://github.com/Kozmosa/mmdash.git" ||
		connection.Credentials == nil || connection.Credentials.Token != token ||
		len(connection.Branches) != 0 {
		t.Fatalf("unexpected runtime connection: %#v", connection)
	}
}

func TestRegistryRejectsMissingMappedBranch(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("server_existing", adapterFunc(func(
		context.Context,
		Config,
	) (Connection, error) {
		return Connection{Branches: map[string]string{"main": providerSHA}}, nil
	})); err != nil {
		t.Fatalf("register adapter: %v", err)
	}
	_, err := registry.Test(context.Background(), Config{
		ArticleBranch: "article", CodeBranch: "main",
		Provider: "server_existing", ResultBranch: "result",
	})
	if !errors.Is(err, ErrBranchMissing) {
		t.Fatalf("missing branch should fail: %v", err)
	}
}

func TestManagedAndDisabledServerExistingProviders(t *testing.T) {
	managed, err := (Managed{}).Test(context.Background(), Config{
		Provider: "managed", CodeBranch: "main",
		ArticleBranch: "article", ResultBranch: "result",
	})
	if err != nil || managed.Provider != "managed" ||
		managed.CanonicalRemoteURL != managedCanonicalRemote ||
		len(managed.Branches) != 3 {
		t.Fatalf("managed provider: %#v %v", managed, err)
	}
	_, err = (ServerExisting{}).Test(context.Background(), Config{
		Provider: "server_existing", RemoteURL: "/srv/repository",
		CodeBranch: "main", ArticleBranch: "article", ResultBranch: "result",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("empty allowlist should disable server provider: %v", err)
	}
}

type adapterFunc func(context.Context, Config) (Connection, error)

func (adapter adapterFunc) Test(ctx context.Context, config Config) (Connection, error) {
	return adapter(ctx, config)
}
