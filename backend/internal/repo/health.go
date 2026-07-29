package repo

import (
	"context"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

// GitChecker verifies that the configured Git binary can start.
type GitChecker struct {
	Client    *gitcli.Client
	Directory string
}

func (checker GitChecker) Check(ctx context.Context) error {
	_, err := checker.Client.Run(ctx, gitcli.Command{
		Args: []string{"--version"}, Directory: checker.Directory,
		Operation: "repo.readiness.git",
	})
	return err
}

func (GitChecker) Name() string { return "git" }

// StorageChecker verifies writable, atomic managed storage.
type StorageChecker struct {
	Storage *gitcli.Storage
}

func (checker StorageChecker) Check(context.Context) error {
	return checker.Storage.Check()
}

func (StorageChecker) Name() string { return "repo_storage" }
