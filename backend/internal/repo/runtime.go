package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
)

// Runtime owns all managed Git subprocess and worktree operations.
type Runtime struct {
	Clock        interface{ Now() time.Time }
	CloneTimeout time.Duration
	Git          *gitcli.Client
	Storage      *gitcli.Storage
	WriteTimeout time.Duration
}

// Synchronize fetches authority from the remote and updates clean managed worktrees.
func (runtime Runtime) Synchronize(
	ctx context.Context,
	repository Repository,
	connection provider.Connection,
	requested []WorkspaceKind,
	source string,
) (SyncResult, error) {
	layout, err := runtime.Storage.Layout(repository.StorageKey)
	if err != nil {
		return SyncResult{}, err
	}
	initial := false
	workspaces, err := selectWorkspaces(repository.Workspaces, requested)
	if err != nil {
		return SyncResult{}, err
	}
	if _, err := os.Stat(layout.Bare); errors.Is(err, os.ErrNotExist) {
		initial = true
		workspaces = repository.Workspaces
		if err := runtime.initialize(ctx, repository, connection); err != nil {
			return SyncResult{}, err
		}
		layout, err = runtime.Storage.Layout(repository.StorageKey)
		if err != nil {
			return SyncResult{}, err
		}
	} else if err != nil {
		return SyncResult{}, err
	} else if err := runtime.fetchWorkspaces(
		ctx, layout, connection, workspaces, runtime.CloneTimeout,
	); err != nil {
		return SyncResult{}, err
	}

	result := SyncResult{
		Commits: []Commit{}, Initial: initial, Source: source,
		Workspaces: []SyncedWorkspace{},
	}
	seenCommits := map[string]bool{}
	for _, workspace := range workspaces {
		synced, commitSHAs, err := runtime.synchronizeWorkspace(
			ctx, layout, workspace, initial,
		)
		if err != nil {
			return SyncResult{}, err
		}
		result.Workspaces = append(result.Workspaces, synced)
		for _, commitSHA := range commitSHAs {
			if seenCommits[commitSHA] {
				continue
			}
			seenCommits[commitSHA] = true
			commit, err := runtime.readCommit(
				ctx, layout, repository.ID, commitSHA, initial,
			)
			if err != nil {
				return SyncResult{}, err
			}
			result.Commits = append(result.Commits, commit)
		}
	}
	sort.Slice(result.Workspaces, func(left, right int) bool {
		return workspaceOrder(result.Workspaces[left].Workspace) <
			workspaceOrder(result.Workspaces[right].Workspace)
	})
	return result, nil
}

func selectWorkspaces(
	available []Workspace,
	requested []WorkspaceKind,
) ([]Workspace, error) {
	if len(requested) == 0 {
		return append([]Workspace(nil), available...), nil
	}
	wanted := map[WorkspaceKind]bool{}
	for _, kind := range requested {
		if !validWorkspaceKind(kind) || wanted[kind] {
			return nil, ErrInvalid
		}
		wanted[kind] = true
	}
	selected := make([]Workspace, 0, len(wanted))
	for _, workspace := range available {
		if wanted[workspace.Workspace] {
			selected = append(selected, workspace)
		}
	}
	if len(selected) != len(wanted) {
		return nil, ErrBranchMapping
	}
	return selected, nil
}

func (runtime Runtime) initialize(
	ctx context.Context,
	repository Repository,
	connection provider.Connection,
) (err error) {
	if connection.Provider == "managed" {
		return runtime.initializeManaged(ctx, repository, connection)
	}
	staging, err := runtime.Storage.Prepare(repository.StorageKey)
	if err != nil {
		return err
	}
	promoted := false
	defer func() {
		if !promoted {
			_ = runtime.Storage.Discard(staging, repository.StorageKey)
		}
	}()
	timeout := runtime.CloneTimeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	if _, err := runtime.Git.Run(ctx, gitcli.Command{
		Args:      []string{"init", "--bare", "bare.git"},
		Directory: staging, Operation: "repo.init", Timeout: timeout,
	}); err != nil {
		return err
	}
	bare := filepath.Join(staging, "bare.git")
	if _, err := runtime.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + bare,
			"remote", "add", "origin", connection.FetchURL,
		},
		Directory: staging, Operation: "repo.remote.add",
		Sensitive: []string{connection.FetchURL}, Timeout: timeout,
	}); err != nil {
		return err
	}
	if err := runtime.fetchWorkspacesAt(
		ctx, staging, bare, connection, repository.Workspaces, timeout,
	); err != nil {
		return err
	}
	if err := runtime.Storage.Promote(staging, repository.StorageKey); err != nil {
		return err
	}
	promoted = true
	return nil
}

func (runtime Runtime) initializeManaged(
	ctx context.Context,
	repository Repository,
	connection provider.Connection,
) (err error) {
	staging, err := runtime.Storage.Prepare(repository.StorageKey)
	if err != nil {
		return err
	}
	promoted := false
	defer func() {
		if !promoted {
			_ = runtime.Storage.Discard(staging, repository.StorageKey)
		}
	}()
	timeout := runtime.CloneTimeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	seed := filepath.Join(staging, "seed")
	if err := os.Mkdir(seed, 0o700); err != nil {
		return err
	}
	run := func(directory, operation string, args ...string) error {
		_, commandErr := runtime.Git.Run(ctx, gitcli.Command{
			Args: args, Directory: directory, Operation: operation, Timeout: timeout,
		})
		return commandErr
	}
	if err := run(seed, "repo.managed.seed.init", "init"); err != nil {
		return err
	}
	if err := run(
		seed,
		"repo.managed.seed.commit",
		"commit", "--allow-empty", "--no-gpg-sign", "-m",
		"chore(repo): initialize mmdash managed repository",
	); err != nil {
		return err
	}
	if err := run(
		seed, "repo.managed.seed.default-branch",
		"branch", "-M", connection.DefaultBranch,
	); err != nil {
		return err
	}
	branches := make([]string, 0, len(repository.Workspaces))
	seen := map[string]bool{connection.DefaultBranch: true}
	for _, workspace := range repository.Workspaces {
		if seen[workspace.RemoteBranch] {
			continue
		}
		seen[workspace.RemoteBranch] = true
		branches = append(branches, workspace.RemoteBranch)
	}
	sort.Strings(branches)
	for _, branch := range branches {
		if err := run(
			seed, "repo.managed.seed.branch", "branch", branch,
		); err != nil {
			return err
		}
	}
	if err := run(staging, "repo.managed.bare.init", "init", "--bare", "bare.git"); err != nil {
		return err
	}
	if err := run(
		seed, "repo.managed.seed.remote", "remote", "add", "origin", "../bare.git",
	); err != nil {
		return err
	}
	if err := run(seed, "repo.managed.seed.push", "push", "origin", "--all"); err != nil {
		return err
	}
	bare := filepath.Join(staging, "bare.git")
	if err := run(
		staging, "repo.managed.bare.head", "--git-dir="+bare,
		"symbolic-ref", "HEAD", "refs/heads/"+connection.DefaultBranch,
	); err != nil {
		return err
	}
	if err := run(
		staging, "repo.managed.remote.add", "--git-dir="+bare,
		"remote", "add", "origin", "bare.git",
	); err != nil {
		return err
	}
	if err := os.RemoveAll(seed); err != nil {
		return err
	}
	if err := runtime.fetchWorkspacesAt(
		ctx, staging, bare, connection, repository.Workspaces, timeout,
	); err != nil {
		return err
	}
	if err := runtime.Storage.Promote(staging, repository.StorageKey); err != nil {
		return err
	}
	promoted = true
	return nil
}

// fetchWorkspace updates exactly one mapped remote branch. Workspace writes
// use this path so an Article or Result commit never waits for unrelated
// branches. The explicit force refspec is safe for the remote-tracking ref:
// it records force-pushes locally without ever rewriting the remote branch.
func (runtime Runtime) fetchWorkspace(
	ctx context.Context,
	layout gitcli.Layout,
	connection provider.Connection,
	workspace Workspace,
	timeout time.Duration,
) error {
	return runtime.fetchWorkspaceAt(
		ctx, layout.Repository, layout.Bare, connection, workspace, timeout,
	)
}

func (runtime Runtime) fetchWorkspaceAt(
	ctx context.Context,
	directory string,
	bare string,
	connection provider.Connection,
	workspace Workspace,
	timeout time.Duration,
) error {
	if gitcli.ValidateBranch(workspace.RemoteBranch) != nil {
		return ErrInvalid
	}
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	branch := workspace.RemoteBranch
	_, err := runtime.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + bare,
			"fetch", "--prune", "origin",
			"+refs/heads/" + branch + ":refs/remotes/origin/" + branch,
		},
		Credentials: connection.Credentials,
		Directory:   directory,
		Operation:   "repo.fetch." + string(workspace.Workspace),
		Sensitive:   []string{connection.FetchURL},
		Timeout:     timeout,
	})
	return err
}

func (runtime Runtime) fetchWorkspaces(
	ctx context.Context,
	layout gitcli.Layout,
	connection provider.Connection,
	workspaces []Workspace,
	timeout time.Duration,
) error {
	return runtime.fetchWorkspacesAt(
		ctx, layout.Repository, layout.Bare, connection, workspaces, timeout,
	)
}

func (runtime Runtime) fetchWorkspacesAt(
	ctx context.Context,
	directory string,
	bare string,
	connection provider.Connection,
	workspaces []Workspace,
	timeout time.Duration,
) error {
	seen := map[string]bool{}
	for _, workspace := range workspaces {
		if seen[workspace.RemoteBranch] {
			continue
		}
		seen[workspace.RemoteBranch] = true
		if err := runtime.fetchWorkspaceAt(
			ctx, directory, bare, connection, workspace, timeout,
		); err != nil {
			return err
		}
	}
	return nil
}

func (runtime Runtime) synchronizeWorkspace(
	ctx context.Context,
	layout gitcli.Layout,
	workspace Workspace,
	initial bool,
) (SyncedWorkspace, []string, error) {
	remoteRef := "refs/remotes/origin/" + workspace.RemoteBranch
	head, err := runtime.resolve(ctx, layout, remoteRef+"^{commit}")
	if err != nil {
		return SyncedWorkspace{}, nil, safe(
			"REPO_BRANCH_NOT_FOUND", "A mapped repository branch was not found",
		)
	}
	tree, err := runtime.resolve(ctx, layout, head+"^{tree}")
	if err != nil {
		return SyncedWorkspace{}, nil, err
	}
	worktree := layout.Worktrees[string(workspace.Workspace)]
	if _, err := os.Stat(worktree); errors.Is(err, os.ErrNotExist) {
		if err := runtime.createWorktree(
			ctx, layout, workspace, remoteRef, worktree,
		); err != nil {
			return SyncedWorkspace{}, nil, err
		}
	} else if err != nil {
		return SyncedWorkspace{}, nil, err
	} else {
		status, err := runtime.Git.Run(ctx, gitcli.Command{
			Args:      []string{"status", "--porcelain=v2", "-z"},
			Directory: worktree, Operation: "repo.worktree.status",
		})
		if err != nil {
			return SyncedWorkspace{}, nil, err
		}
		entries, err := gitcli.ParseStatusPorcelainV2(status.Stdout)
		if err != nil {
			return SyncedWorkspace{}, nil, err
		}
		if len(entries) > 0 {
			return SyncedWorkspace{}, nil, ErrWorktreeDirty
		}
		if _, err := runtime.Git.Run(ctx, gitcli.Command{
			Args:      []string{"reset", "--hard", remoteRef},
			Directory: worktree, Operation: "repo.worktree.reset",
		}); err != nil {
			return SyncedWorkspace{}, nil, err
		}
	}
	historyRewritten := false
	commitSHAs := []string{head}
	if !initial && workspace.HeadCommitSHA != nil && *workspace.HeadCommitSHA != head {
		isAncestor, err := runtime.isAncestor(ctx, layout, *workspace.HeadCommitSHA, head)
		if err != nil {
			return SyncedWorkspace{}, nil, err
		}
		historyRewritten = !isAncestor
		if isAncestor {
			commitSHAs, err = runtime.listCommits(
				ctx, layout, *workspace.HeadCommitSHA, head,
			)
			if err != nil {
				return SyncedWorkspace{}, nil, err
			}
		}
	}
	return SyncedWorkspace{
		HeadCommitSHA: head, HistoryRewritten: historyRewritten,
		Status: WorkspaceReady, TreeSHA: tree, Workspace: workspace.Workspace,
	}, commitSHAs, nil
}

func (runtime Runtime) createWorktree(
	ctx context.Context,
	layout gitcli.Layout,
	workspace Workspace,
	remoteRef string,
	worktree string,
) error {
	if err := os.MkdirAll(filepath.Dir(worktree), 0o700); err != nil {
		return err
	}
	if _, err := runtime.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"branch", "--force", workspace.LocalBranch, remoteRef,
		},
		Directory: layout.Repository, Operation: "repo.worktree.branch",
	}); err != nil {
		return err
	}
	if _, err := runtime.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"worktree", "add", worktree, workspace.LocalBranch,
		},
		Directory: layout.Repository, Operation: "repo.worktree.add",
		Sensitive: []string{worktree},
	}); err != nil {
		return err
	}
	_, _ = runtime.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"branch", "--set-upstream-to=" + remoteRef, workspace.LocalBranch,
		},
		Directory: layout.Repository, Operation: "repo.worktree.upstream",
	})
	return nil
}

func (runtime Runtime) resolve(
	ctx context.Context,
	layout gitcli.Layout,
	revision string,
) (string, error) {
	result, err := runtime.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"rev-parse", "--verify", "--end-of-options", revision,
		},
		Directory: layout.Repository, Operation: "repo.revision.resolve",
	})
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(result.Stdout))
	if err := gitcli.ValidateFullSHA(sha); err != nil {
		return "", err
	}
	return sha, nil
}

func (runtime Runtime) isAncestor(
	ctx context.Context,
	layout gitcli.Layout,
	ancestor string,
	descendant string,
) (bool, error) {
	_, err := runtime.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"merge-base", "--is-ancestor", ancestor, descendant,
		},
		Directory: layout.Repository, Operation: "repo.history.ancestor",
	})
	if err == nil {
		return true, nil
	}
	var commandError *gitcli.CommandError
	if errors.As(err, &commandError) && commandError.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

func (runtime Runtime) listCommits(
	ctx context.Context,
	layout gitcli.Layout,
	previous string,
	current string,
) ([]string, error) {
	result, err := runtime.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"rev-list", "--reverse", previous + ".." + current,
		},
		Directory: layout.Repository, Operation: "repo.commits.new",
	})
	if err != nil {
		return nil, err
	}
	shas := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(result.Stdout)), "\n") {
		sha := strings.TrimSpace(line)
		if sha == "" {
			continue
		}
		if gitcli.ValidateFullSHA(sha) != nil {
			return nil, fmt.Errorf("parse rev-list output")
		}
		shas = append(shas, sha)
	}
	if len(shas) == 0 {
		shas = append(shas, current)
	}
	return shas, nil
}

func (runtime Runtime) readCommit(
	ctx context.Context,
	layout gitcli.Layout,
	repositoryID string,
	commitSHA string,
	initial bool,
) (Commit, error) {
	result, err := runtime.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"show", "-s",
			"--format=%H%x00%T%x00%P%x00%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%B",
			commitSHA,
		},
		Directory: layout.Repository, Operation: "repo.commit.read",
	})
	if err != nil {
		return Commit{}, err
	}
	fields := strings.SplitN(string(result.Stdout), "\x00", 10)
	if len(fields) != 10 ||
		gitcli.ValidateFullSHA(strings.TrimSpace(fields[0])) != nil ||
		gitcli.ValidateFullSHA(strings.TrimSpace(fields[1])) != nil {
		return Commit{}, fmt.Errorf("parse commit metadata")
	}
	parents := strings.Fields(fields[2])
	for _, parent := range parents {
		if gitcli.ValidateFullSHA(parent) != nil {
			return Commit{}, fmt.Errorf("parse commit parent")
		}
	}
	authorTime, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[5]))
	if err != nil {
		return Commit{}, fmt.Errorf("parse author time")
	}
	committerTime, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[8]))
	if err != nil {
		return Commit{}, fmt.Errorf("parse committer time")
	}
	source := "sync"
	if initial {
		source = "connect"
	}
	message := strings.TrimRight(fields[9], "\r\n")
	if len(message) > 100000 {
		return Commit{}, safe(
			"REPO_COMMIT_MESSAGE_TOO_LARGE",
			"Repository commit message exceeds the supported size",
		)
	}
	return Commit{
		Author: GitIdentity{
			Email: fields[4], Name: fields[3], Time: authorTime,
		},
		Changes: []ChangedPath{}, CommitSHA: strings.TrimSpace(fields[0]),
		Committer: GitIdentity{
			Email: fields[7], Name: fields[6], Time: committerTime,
		},
		FirstSeenAt: runtime.now(), Message: message,
		ParentSHAs: parents, RepositoryID: repositoryID, Source: source,
		TreeSHA: strings.TrimSpace(fields[1]),
	}, nil
}

func (runtime Runtime) now() time.Time {
	if runtime.Clock == nil {
		return time.Now().UTC()
	}
	return runtime.Clock.Now().UTC()
}

func workspaceOrder(workspace WorkspaceKind) int {
	switch workspace {
	case WorkspaceCode:
		return 0
	case WorkspaceArticle:
		return 1
	default:
		return 2
	}
}
