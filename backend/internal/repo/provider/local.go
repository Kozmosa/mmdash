package provider

import (
	"context"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

// Local tests an administrator-allowlisted server-visible Git repository.
type Local struct {
	AllowedRoots []string
	Git          CommandRunner
}

func (provider Local) Test(ctx context.Context, config Config) (Connection, error) {
	source, err := gitcli.ResolveLocalSource(config.RemoteURL, provider.AllowedRoots)
	if err != nil {
		return Connection{}, ErrInvalidConfig
	}
	defaultResult, err := provider.Git.Run(ctx, gitcli.Command{
		Args:      []string{"symbolic-ref", "--quiet", "--short", "HEAD"},
		Directory: source,
		Operation: "provider.local.default-branch",
		Sensitive: []string{source},
	})
	if err != nil {
		return Connection{}, ErrRemoteNotFound
	}
	defaultBranch := strings.TrimSpace(string(defaultResult.Stdout))
	if gitcli.ValidateBranch(defaultBranch) != nil {
		return Connection{}, ErrRemoteNotFound
	}
	branchResult, err := provider.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"for-each-ref",
			"--format=%(objectname)%09%(refname:strip=2)",
			"refs/heads",
		},
		Directory: source,
		Operation: "provider.local.branches",
		Sensitive: []string{source},
	})
	if err != nil {
		return Connection{}, ErrRemoteNotFound
	}
	branches, err := parseLocalBranches(branchResult.Stdout)
	if err != nil {
		return Connection{}, ErrRemoteNotFound
	}
	return Connection{
		Branches: branches, CanonicalRemoteURL: source,
		DefaultBranch: defaultBranch, DisplayName: "Local Git",
		FetchURL: source, Provider: "local",
	}, nil
}

func parseLocalBranches(contents []byte) (map[string]string, error) {
	branches := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		sha, name, found := splitOnce(strings.TrimSpace(line), "\t")
		if !found ||
			gitcli.ValidateFullSHA(sha) != nil ||
			gitcli.ValidateBranch(name) != nil {
			return nil, ErrRemoteNotFound
		}
		branches[name] = sha
	}
	if len(branches) == 0 {
		return nil, ErrRemoteNotFound
	}
	return branches, nil
}
