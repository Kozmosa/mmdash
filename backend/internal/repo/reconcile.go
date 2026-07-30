package repo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

// ReconcileResult describes drift found between PostgreSQL and Git worktrees.
type ReconcileResult struct {
	MissingCheckoutIDs []string
	NeedsSync          bool
}

// ReconcileWorktrees removes orphaned managed checkouts and detects missing or
// mismatched long-lived worktrees without following arbitrary paths.
func (runtimeState Runtime) ReconcileWorktrees(
	ctx context.Context,
	repository Repository,
	activeCheckouts []Checkout,
) (ReconcileResult, error) {
	layout, err := runtimeState.Storage.Layout(repository.StorageKey)
	if err != nil {
		return ReconcileResult{}, err
	}
	if _, err := os.Stat(layout.Bare); errors.Is(err, os.ErrNotExist) {
		return ReconcileResult{NeedsSync: true}, nil
	} else if err != nil {
		return ReconcileResult{}, err
	}
	result, err := runtimeState.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"worktree", "list", "--porcelain",
		},
		Directory: layout.Repository, Operation: "repo.worktree.reconcile.list",
	})
	if err != nil {
		return ReconcileResult{}, err
	}
	worktrees, err := gitcli.ParseWorktrees(result.Stdout)
	if err != nil {
		return ReconcileResult{}, err
	}
	expected := map[string]*string{}
	for _, workspace := range repository.Workspaces {
		target := layout.Worktrees[string(workspace.Workspace)]
		expected[normalizedPath(target)] = workspace.HeadCommitSHA
	}
	activePaths := map[string]string{}
	for _, checkout := range activeCheckouts {
		target, err := runtimeState.Storage.ManagedPath(
			repository.StorageKey, checkout.CheckoutRelpath,
		)
		if err != nil {
			return ReconcileResult{}, err
		}
		activePaths[normalizedPath(target)] = checkout.CheckoutID
	}
	observedExpected := map[string]bool{}
	observedActive := map[string]bool{}
	reconciliation := ReconcileResult{}
	for _, worktree := range worktrees {
		candidate := normalizedPath(worktree.Path)
		if samePath(candidate, normalizedPath(layout.Bare)) || worktree.Bare {
			continue
		}
		if head, exists := expected[candidate]; exists {
			observedExpected[candidate] = true
			if head != nil && worktree.Head != *head {
				reconciliation.NeedsSync = true
			}
			continue
		}
		if checkoutID, exists := activePaths[candidate]; exists {
			observedActive[checkoutID] = true
			continue
		}
		if pathContained(layout.Checkouts, candidate) {
			if _, err := runtimeState.Git.Run(ctx, gitcli.Command{
				Args: []string{
					"--git-dir=" + layout.Bare,
					"worktree", "remove", "--force", worktree.Path,
				},
				Directory: layout.Repository,
				Operation: "repo.worktree.reconcile.orphan",
				Sensitive: []string{worktree.Path},
			}); err != nil {
				return ReconcileResult{}, err
			}
			continue
		}
		return ReconcileResult{}, gitcli.ErrStorageEscape
	}
	for target := range expected {
		if !observedExpected[target] {
			reconciliation.NeedsSync = true
		}
	}
	for _, checkout := range activeCheckouts {
		if !observedActive[checkout.CheckoutID] {
			reconciliation.MissingCheckoutIDs = append(
				reconciliation.MissingCheckoutIDs, checkout.CheckoutID,
			)
		}
	}
	_, err = runtimeState.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare, "worktree", "prune",
		},
		Directory: layout.Repository, Operation: "repo.worktree.reconcile.prune",
	})
	return reconciliation, err
}

// Reconciler performs the required Core startup database/worktree audit.
type Reconciler struct {
	Checkouts    CheckoutStore
	Clock        interface{ Now() time.Time }
	Repositories Store
	Runtime      Runtime
}

func (reconciler Reconciler) Run(ctx context.Context) error {
	repositories, err := reconciler.Repositories.ListRepositories(ctx)
	if err != nil {
		return err
	}
	for _, repository := range repositories {
		checkouts, err := reconciler.Checkouts.ListActiveCheckouts(
			ctx, repository.ID,
		)
		if err != nil {
			return err
		}
		result, err := reconciler.Runtime.ReconcileWorktrees(
			ctx, repository, checkouts,
		)
		if err != nil {
			return err
		}
		for _, checkoutID := range result.MissingCheckoutIDs {
			if err := reconciler.Checkouts.MarkCheckoutError(
				ctx, checkoutID,
			); err != nil {
				return err
			}
		}
		if result.NeedsSync {
			if _, err := reconciler.Repositories.RequestSyncSource(
				ctx, repository.ProjectID,
				reconciler.Clock.Now().UTC(), "poll",
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizedPath(value string) string {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return filepath.Clean(value)
	}
	return filepath.Clean(absolute)
}

func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func pathContained(root, candidate string) bool {
	root = normalizedPath(root)
	candidate = normalizedPath(candidate)
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative == "." ||
		(relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
			!filepath.IsAbs(relative))
}
