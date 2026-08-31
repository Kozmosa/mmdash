package provider

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/repo/egress"
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

func TestGitHubMetadataAndLsRemoteShareTheExplicitRepoProxy(t *testing.T) {
	const token = "github_pat_shared_proxy"
	var requests atomic.Int64
	proxyServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Host != "api.github.test" || request.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("unexpected proxied GitHub request: %s", request.URL.String())
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"default_branch":"main","permissions":{"push":true}}`)
	}))
	defer proxyServer.Close()
	proxy, err := egress.Parse(proxyServer.URL, "localhost,127.0.0.1,::1")
	if err != nil {
		t.Fatalf("parse proxy: %v", err)
	}
	git := fakeRunner{handler: func(command gitcli.Command) (gitcli.Result, error) {
		if command.Credentials == nil ||
			command.Credentials.Proxy.GitEnvironment()["HTTPS_PROXY"] != proxyServer.URL {
			t.Fatal("Git ls-remote did not receive the shared Repo proxy")
		}
		return gitcli.Result{Stdout: []byte(
			providerSHA + "\trefs/heads/main\n" +
				providerSHA + "\trefs/heads/article\n" +
				providerSHA + "\trefs/heads/result\n",
		)}, nil
	}}
	connection, err := (GitHub{
		APIBase: "http://api.github.test", Client: proxy.HTTPClient(),
		Egress: proxy, Git: git, RuntimeRoot: t.TempDir(),
	}).Test(context.Background(), Config{
		AccessToken: token, ArticleBranch: "article", CodeBranch: "main",
		Provider: "github", RemoteURL: "https://github.com/acme/model",
		ResultBranch: "result",
	})
	if err != nil || requests.Load() != 1 || connection.Credentials == nil {
		t.Fatalf("shared proxy connection failed: requests=%d connection=%#v err=%v", requests.Load(), connection, err)
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

func TestGitHubMetadataClassifiesTransportStatusAndPayloadFailures(t *testing.T) {
	transportCause := &net.DNSError{Err: "temporary failure", Name: "api.github.test", IsTemporary: true}
	for _, test := range []struct {
		name      string
		response  *http.Response
		transport error
		want      error
		wantCause bool
	}{
		{name: "dns", transport: transportCause, want: ErrNetworkUnavailable, wantCause: true},
		{name: "timeout", transport: context.DeadlineExceeded, want: ErrTimeout, wantCause: true},
		{name: "unauthorized", response: githubResponse(http.StatusUnauthorized, `{}`), want: ErrAuthentication},
		{name: "forbidden", response: githubResponse(http.StatusForbidden, `{}`), want: ErrAuthentication},
		{name: "not found", response: githubResponse(http.StatusNotFound, `{}`), want: ErrRemoteNotFound},
		{name: "rate limit", response: githubResponse(http.StatusTooManyRequests, `{}`), want: ErrTemporarilyUnavailable},
		{name: "provider 503", response: githubResponse(http.StatusServiceUnavailable, `{}`), want: ErrTemporarilyUnavailable},
		{name: "unexpected status", response: githubResponse(http.StatusBadRequest, `{}`), want: ErrInvalidResponse},
		{name: "invalid json", response: githubResponse(http.StatusOK, `{`), want: ErrInvalidResponse},
		{name: "write permission", response: githubResponse(http.StatusOK, `{"default_branch":"main","permissions":{"push":false}}`), want: ErrWritePermission},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return test.response, test.transport
			})}
			_, err := (GitHub{APIBase: "https://api.github.test", Client: client}).metadata(
				context.Background(), "acme/model", "token",
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("unexpected classification: %v", err)
			}
			if test.wantCause {
				if test.want == ErrNetworkUnavailable {
					var dnsError *net.DNSError
					if !errors.As(err, &dnsError) {
						t.Fatalf("DNS cause was not preserved: %v", err)
					}
				} else if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("timeout cause was not preserved: %v", err)
				}
			}
		})
	}
}

func TestGitHubPreservesTypedGitFailures(t *testing.T) {
	for _, test := range []struct {
		gitError error
		name     string
		want     error
	}{
		{name: "authentication", gitError: gitcli.ErrAuthentication, want: ErrAuthentication},
		{name: "branch", gitError: gitcli.ErrBranchNotFound, want: ErrBranchMissing},
		{name: "network", gitError: gitcli.ErrNetworkUnavailable, want: ErrNetworkUnavailable},
		{name: "not found", gitError: gitcli.ErrRemoteNotFound, want: ErrRemoteNotFound},
		{name: "provider", gitError: gitcli.ErrProviderUnavailable, want: ErrTemporarilyUnavailable},
		{name: "timeout", gitError: gitcli.ErrTimeout, want: ErrTimeout},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := GitHub{
				APIBase: "https://api.github.test",
				Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return githubResponse(http.StatusOK, `{"default_branch":"main","permissions":{"push":true}}`), nil
				})},
				Git: fakeRunner{handler: func(gitcli.Command) (gitcli.Result, error) {
					return gitcli.Result{}, &gitcli.CommandError{
						Code: test.gitError, ExitCode: 1, Operation: "provider.github.ls-remote",
					}
				}},
				RuntimeRoot: t.TempDir(),
			}
			_, err := provider.Test(context.Background(), Config{
				AccessToken: "token", ArticleBranch: "article", CodeBranch: "main",
				Provider: "github", RemoteURL: "https://github.com/acme/model",
				ResultBranch: "result",
			})
			if !errors.Is(err, test.want) || !errors.Is(err, test.gitError) {
				t.Fatalf("classification or cause was lost: %v", err)
			}
		})
	}
}

func TestGitHubProxyConnectionFailureDoesNotExposeProxyCredentials(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve proxy address: %v", err)
	}
	proxyAddress := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close reserved proxy address: %v", err)
	}
	proxy, err := egress.Parse(
		"http://proxy-user:proxy-password@"+proxyAddress,
		"localhost,127.0.0.1,::1",
	)
	if err != nil {
		t.Fatalf("parse proxy: %v", err)
	}
	_, err = (GitHub{
		APIBase: "https://api.github.test",
		Client:  proxy.HTTPClient(),
	}).metadata(context.Background(), "acme/model", "github-token")
	if !errors.Is(err, ErrNetworkUnavailable) {
		t.Fatalf("unexpected proxy failure classification: %v", err)
	}
	for _, secret := range []string{"proxy-user", "proxy-password", "github-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("proxy failure leaked %s: %v", secret, err)
		}
	}
}

func githubResponse(status int, body string) *http.Response {
	return &http.Response{
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		StatusCode: status,
	}
}

type adapterFunc func(context.Context, Config) (Connection, error)

func (adapter adapterFunc) Test(ctx context.Context, config Config) (Connection, error) {
	return adapter(ctx, config)
}
