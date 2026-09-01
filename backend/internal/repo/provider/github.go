package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

// CommandRunner is the narrow Git subprocess boundary used by providers.
type CommandRunner interface {
	Run(context.Context, gitcli.Command) (gitcli.Result, error)
}

// GitHub tests HTTPS/PAT access against GitHub metadata and Git refs.
type GitHub struct {
	APIBase     string
	Client      *http.Client
	Git         CommandRunner
	RuntimeRoot string
	UserAgent   string
}

func (provider GitHub) Test(ctx context.Context, config Config) (Connection, error) {
	connection, err := provider.Resolve(ctx, config)
	if err != nil {
		return Connection{}, err
	}
	metadata, err := provider.metadata(
		ctx, connection.DisplayName, config.AccessToken,
	)
	if err != nil {
		return Connection{}, err
	}
	result, err := provider.Git.Run(ctx, gitcli.Command{
		Args:        []string{"ls-remote", "--heads", connection.FetchURL},
		Credentials: connection.Credentials,
		Directory:   provider.RuntimeRoot,
		Operation:   "provider.github.ls-remote",
		Sensitive:   []string{config.RemoteURL},
	})
	if err != nil {
		if errors.Is(err, gitcli.ErrAuthentication) {
			return Connection{}, ErrAuthentication
		}
		return Connection{}, ErrRemoteNotFound
	}
	branches, err := parseRemoteHeads(result.Stdout)
	if err != nil {
		return Connection{}, ErrRemoteNotFound
	}
	connection.Branches = branches
	connection.DefaultBranch = metadata.DefaultBranch
	return connection, nil
}

func (provider GitHub) Resolve(
	_ context.Context,
	config Config,
) (Connection, error) {
	remote, err := gitcli.NormalizeGitHubURL(config.RemoteURL)
	if err != nil || config.AccessToken == "" {
		return Connection{}, ErrInvalidConfig
	}
	return Connection{
		Branches: map[string]string{}, CanonicalRemoteURL: remote.CanonicalURL,
		Credentials: &gitcli.Credentials{
			Token: config.AccessToken, Username: "x-access-token",
		},
		DisplayName: remote.DisplayName, FetchURL: remote.FetchURL,
		Provider: "github",
	}, nil
}

type githubMetadata struct {
	DefaultBranch string `json:"default_branch"`
	Permissions   struct {
		Push bool `json:"push"`
	} `json:"permissions"`
}

func (provider GitHub) metadata(
	ctx context.Context,
	displayName string,
	token string,
) (githubMetadata, error) {
	base := strings.TrimSuffix(provider.APIBase, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		base+"/repos/"+displayName,
		nil,
	)
	if err != nil {
		return githubMetadata{}, ErrInvalidConfig
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	userAgent := provider.UserAgent
	if userAgent == "" {
		userAgent = "mmdash-core/0.1"
	}
	request.Header.Set("User-Agent", userAgent)
	client := provider.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return githubMetadata{}, ErrRemoteNotFound
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized ||
		response.StatusCode == http.StatusForbidden {
		return githubMetadata{}, ErrAuthentication
	}
	if response.StatusCode == http.StatusNotFound {
		return githubMetadata{}, ErrRemoteNotFound
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return githubMetadata{}, ErrRemoteNotFound
	}
	var metadata githubMetadata
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1024*1024))
	if err := decoder.Decode(&metadata); err != nil ||
		metadata.DefaultBranch == "" ||
		gitcli.ValidateBranch(metadata.DefaultBranch) != nil {
		return githubMetadata{}, ErrRemoteNotFound
	}
	if !metadata.Permissions.Push {
		return githubMetadata{}, ErrWritePermission
	}
	return metadata, nil
}

func parseRemoteHeads(contents []byte) (map[string]string, error) {
	branches := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		sha, ref, found := splitOnce(strings.TrimSpace(line), "\t")
		if !found || !strings.HasPrefix(ref, "refs/heads/") ||
			gitcli.ValidateFullSHA(sha) != nil {
			return nil, fmt.Errorf("invalid ls-remote output")
		}
		name := strings.TrimPrefix(ref, "refs/heads/")
		if gitcli.ValidateBranch(name) != nil {
			return nil, fmt.Errorf("invalid remote branch")
		}
		branches[name] = sha
	}
	if len(branches) == 0 {
		return nil, fmt.Errorf("remote has no branches")
	}
	return branches, nil
}
