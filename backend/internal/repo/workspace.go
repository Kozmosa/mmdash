package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
)

// CreateCheckout creates one detached managed worktree for a fixed commit.
func (runtime Runtime) CreateCheckout(
	ctx context.Context,
	repository Repository,
	checkoutID string,
	commitSHA string,
) (string, error) {
	if gitcli.ValidateFullSHA(commitSHA) != nil {
		return "", ErrInvalid
	}
	relative := filepath.Join("checkouts", checkoutID)
	target, err := runtime.Storage.ManagedPath(repository.StorageKey, relative)
	if err != nil {
		return "", err
	}
	layout, err := runtime.Storage.Layout(repository.StorageKey)
	if err != nil {
		return "", err
	}
	if _, err := runtime.resolve(ctx, layout, commitSHA+"^{commit}"); err != nil {
		return "", ErrObjectNotFound
	}
	if err := os.MkdirAll(layout.Checkouts, 0o700); err != nil {
		return "", err
	}
	if _, err := runtime.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"worktree", "add", "--detach", target, commitSHA,
		},
		Directory: layout.Repository, Operation: "repo.checkout.create",
		Sensitive: []string{target},
	}); err != nil {
		return "", err
	}
	return filepath.ToSlash(relative), nil
}

// ReleaseCheckout removes only a verified managed checkout worktree.
func (runtime Runtime) ReleaseCheckout(
	ctx context.Context,
	repository Repository,
	relative string,
) error {
	target, err := runtime.Storage.ManagedPath(repository.StorageKey, relative)
	if err != nil {
		return err
	}
	layout, err := runtime.Storage.Layout(repository.StorageKey)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		_, _ = runtime.Git.Run(ctx, gitcli.Command{
			Args: []string{
				"--git-dir=" + layout.Bare, "worktree", "prune",
			},
			Directory: layout.Repository, Operation: "repo.checkout.prune",
		})
		return nil
	} else if err != nil {
		return err
	}
	if _, err := runtime.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"worktree", "remove", "--force", target,
		},
		Directory: layout.Repository, Operation: "repo.checkout.release",
		Sensitive: []string{target},
	}); err != nil {
		return err
	}
	return nil
}

// WorkspaceWriter applies bounded ordinary-file changes and pushes normally.
type WorkspaceWriter struct {
	Clock   interface{ Now() time.Time }
	Git     *gitcli.Client
	Runtime Runtime
	Storage *gitcli.Storage
}

func (writer WorkspaceWriter) Prepare(
	ctx context.Context,
	claim CommitClaim,
	connection provider.Connection,
	request WorkspaceCommitRequest,
) (commit Commit, err error) {
	layout, err := writer.Storage.Layout(claim.Repository.StorageKey)
	if err != nil {
		return Commit{}, err
	}
	if err := writer.Runtime.fetch(
		ctx, layout, connection, writer.Runtime.CloneTimeout,
	); err != nil {
		return Commit{}, err
	}
	remoteRef := "refs/remotes/origin/" + claim.Workspace.RemoteBranch
	remoteHead, err := writer.Runtime.resolve(ctx, layout, remoteRef+"^{commit}")
	if err != nil {
		return Commit{}, ErrObjectNotFound
	}
	if remoteHead != request.ExpectedHeadSHA {
		return Commit{}, ErrHeadChanged
	}
	worktree := layout.Worktrees[string(claim.Workspace.Workspace)]
	if err := writer.requireClean(ctx, worktree); err != nil {
		return Commit{}, err
	}
	if _, err := writer.Git.Run(ctx, gitcli.Command{
		Args:      []string{"reset", "--hard", remoteRef},
		Directory: worktree, Operation: "repo.commit.reset",
	}); err != nil {
		return Commit{}, err
	}
	mutated := false
	defer func() {
		if err != nil && mutated {
			_ = writer.recover(ctx, layout, worktree, remoteRef)
		}
	}()
	for _, change := range request.Changes {
		if err := applyFileChange(worktree, change); err != nil {
			return Commit{}, err
		}
		mutated = true
	}
	pathspecs := make([]string, 0, len(request.Changes)+2)
	pathspecs = append(pathspecs, "add", "--all", "--")
	if !request.StageAll {
		for _, change := range request.Changes {
			pathspecs = append(pathspecs, ":(top,literal)"+change.Path)
		}
	}
	if _, err := writer.Git.Run(ctx, gitcli.Command{
		Args: pathspecs, Directory: worktree, Operation: "repo.commit.stage",
	}); err != nil {
		return Commit{}, err
	}
	_, diffErr := writer.Git.Run(ctx, gitcli.Command{
		Args:      []string{"diff", "--cached", "--quiet", "--exit-code"},
		Directory: worktree, Operation: "repo.commit.diff",
	})
	if diffErr == nil {
		return Commit{}, ErrNoChanges
	}
	var commandError *gitcli.CommandError
	if !errors.As(diffErr, &commandError) || commandError.ExitCode != 1 {
		return Commit{}, diffErr
	}
	environment := map[string]string{
		"GIT_AUTHOR_EMAIL":    request.ActorEmail,
		"GIT_AUTHOR_NAME":     request.ActorName,
		"GIT_COMMITTER_EMAIL": request.ActorEmail,
		"GIT_COMMITTER_NAME":  request.ActorName,
	}
	if _, err := writer.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"-c", "core.hooksPath=" + os.DevNull,
			"commit", "--no-gpg-sign", "-m", request.Message,
		},
		Directory: worktree, Environment: environment,
		Operation: "repo.commit.create",
	}); err != nil {
		return Commit{}, err
	}
	head, err := writer.resolveWorktreeHead(ctx, worktree)
	if err != nil {
		return Commit{}, err
	}
	commit, err = writer.Runtime.readCommit(
		ctx, layout, claim.Repository.ID, head, false,
	)
	if err != nil {
		return Commit{}, err
	}
	commit.Source = "mmdash"
	return commit, nil
}

func (writer WorkspaceWriter) PushPrepared(
	ctx context.Context,
	claim CommitClaim,
	connection provider.Connection,
	preparedSHA string,
) (Commit, error) {
	layout, err := writer.Storage.Layout(claim.Repository.StorageKey)
	if err != nil {
		return Commit{}, err
	}
	if gitcli.ValidateFullSHA(preparedSHA) != nil {
		return Commit{}, ErrInvalid
	}
	if _, err := writer.Runtime.resolve(
		ctx, layout, preparedSHA+"^{commit}",
	); err != nil {
		return Commit{}, ErrObjectNotFound
	}
	if err := writer.Runtime.fetch(
		ctx, layout, connection, writer.Runtime.CloneTimeout,
	); err != nil {
		return Commit{}, err
	}
	remoteRef := "refs/remotes/origin/" + claim.Workspace.RemoteBranch
	remoteHead, err := writer.Runtime.resolve(ctx, layout, remoteRef+"^{commit}")
	if err != nil {
		return Commit{}, ErrObjectNotFound
	}
	worktree := layout.Worktrees[string(claim.Workspace.Workspace)]
	if remoteHead != preparedSHA {
		expected := ""
		if claim.Workspace.HeadCommitSHA != nil {
			expected = *claim.Workspace.HeadCommitSHA
		}
		if remoteHead != expected {
			_ = writer.recover(ctx, layout, worktree, remoteRef)
			return Commit{}, ErrHeadChanged
		}
		pushResult, pushErr := writer.Git.Run(ctx, gitcli.Command{
			Args: []string{
				"--git-dir=" + layout.Bare,
				"push", "origin",
				preparedSHA + ":refs/heads/" + claim.Workspace.RemoteBranch,
			},
			Credentials: connection.Credentials,
			Directory:   layout.Repository,
			Operation:   "repo.commit.push",
		})
		_ = pushResult
		if pushErr != nil {
			if fetchErr := writer.Runtime.fetch(
				ctx, layout, connection, writer.Runtime.CloneTimeout,
			); fetchErr == nil {
				observed, resolveErr := writer.Runtime.resolve(
					ctx, layout, remoteRef+"^{commit}",
				)
				if resolveErr == nil && observed == preparedSHA {
					pushErr = nil
				} else if resolveErr == nil && observed != expected {
					pushErr = ErrHeadChanged
				}
			}
			if pushErr != nil {
				_ = writer.recover(ctx, layout, worktree, remoteRef)
				if errors.Is(pushErr, ErrHeadChanged) {
					return Commit{}, pushErr
				}
				return Commit{}, safe(
					"REPO_PUSH_FAILED", "Repository push failed",
				)
			}
		}
	}
	if _, err := writer.Git.Run(ctx, gitcli.Command{
		Args:      []string{"reset", "--hard", preparedSHA},
		Directory: worktree, Operation: "repo.commit.finalize",
	}); err != nil {
		return Commit{}, err
	}
	commit, err := writer.Runtime.readCommit(
		ctx, layout, claim.Repository.ID, preparedSHA, false,
	)
	if err != nil {
		return Commit{}, err
	}
	commit.Source = "mmdash"
	return commit, nil
}

func (writer WorkspaceWriter) requireClean(
	ctx context.Context,
	worktree string,
) error {
	result, err := writer.Git.Run(ctx, gitcli.Command{
		Args:      []string{"status", "--porcelain=v2", "-z"},
		Directory: worktree, Operation: "repo.commit.status",
	})
	if err != nil {
		return err
	}
	entries, err := gitcli.ParseStatusPorcelainV2(result.Stdout)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return ErrWorktreeDirty
	}
	return nil
}

func (writer WorkspaceWriter) recover(
	ctx context.Context,
	layout gitcli.Layout,
	worktree string,
	remoteRef string,
) error {
	if _, err := writer.Git.Run(ctx, gitcli.Command{
		Args:      []string{"reset", "--hard", remoteRef},
		Directory: worktree, Operation: "repo.commit.recover.reset",
	}); err != nil {
		return err
	}
	if _, err := writer.Git.Run(ctx, gitcli.Command{
		Args:      []string{"clean", "-fdx", "--"},
		Directory: worktree, Operation: "repo.commit.recover.clean",
	}); err != nil {
		return err
	}
	return nil
}

func (writer WorkspaceWriter) resolveWorktreeHead(
	ctx context.Context,
	worktree string,
) (string, error) {
	result, err := writer.Git.Run(ctx, gitcli.Command{
		Args:      []string{"rev-parse", "--verify", "HEAD^{commit}"},
		Directory: worktree, Operation: "repo.commit.head",
	})
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(result.Stdout))
	if gitcli.ValidateFullSHA(sha) != nil {
		return "", fmt.Errorf("parse committed head")
	}
	return sha, nil
}

func applyFileChange(worktree string, change FileChange) error {
	if gitcli.ValidateRepoPath(change.Path, false) != nil {
		return ErrInvalid
	}
	segments := strings.Split(change.Path, "/")
	parent := worktree
	for _, segment := range segments[:len(segments)-1] {
		parent = filepath.Join(parent, segment)
		info, err := os.Lstat(parent)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(parent, 0o700); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrInvalid
		}
	}
	target := filepath.Join(parent, segments[len(segments)-1])
	info, statErr := os.Lstat(target)
	switch change.Operation {
	case "put":
		if statErr == nil &&
			(info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
			return ErrInvalid
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		temporary, err := os.CreateTemp(parent, ".mmdash-write-")
		if err != nil {
			return err
		}
		temporaryPath := temporary.Name()
		defer os.Remove(temporaryPath)
		if err := temporary.Chmod(0o600); err != nil {
			_ = temporary.Close()
			return err
		}
		if change.SourcePath == "" {
			if _, err := temporary.Write(change.Content); err != nil {
				_ = temporary.Close()
				return err
			}
		} else {
			sourceInfo, sourceErr := os.Lstat(change.SourcePath)
			if sourceErr != nil || !sourceInfo.Mode().IsRegular() ||
				sourceInfo.Mode()&os.ModeSymlink != 0 || sourceInfo.Size() != change.SizeBytes {
				_ = temporary.Close()
				return ErrInvalid
			}
			source, sourceErr := os.Open(change.SourcePath)
			if sourceErr != nil {
				_ = temporary.Close()
				return sourceErr
			}
			digest := sha256.New()
			copied, copyErr := io.Copy(io.MultiWriter(temporary, digest), source)
			closeErr := source.Close()
			if copyErr != nil || closeErr != nil || copied != change.SizeBytes ||
				hex.EncodeToString(digest.Sum(nil)) != change.ContentSHA256 {
				_ = temporary.Close()
				return ErrInvalid
			}
		}
		if err := temporary.Close(); err != nil {
			return err
		}
		if statErr == nil {
			if err := os.Remove(target); err != nil {
				return err
			}
		}
		return os.Rename(temporaryPath, target)
	case "delete":
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return ErrInvalid
			}
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ErrInvalid
		}
		return os.Remove(target)
	default:
		return ErrInvalid
	}
}
