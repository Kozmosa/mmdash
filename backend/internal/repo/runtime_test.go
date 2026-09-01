package repo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
)

func TestRuntimeSynchronizesThreeWorktreesAndRejectsDirtyState(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git is not installed")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	runTestGit(t, gitPath, root, "init", source)
	runTestGit(t, gitPath, source, "config", "user.name", "Repo Test")
	runTestGit(t, gitPath, source, "config", "user.email", "repo@example.test")
	if err := os.WriteFile(
		filepath.Join(source, "README.md"), []byte("initial\n"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, gitPath, source, "add", "README.md")
	runTestGit(t, gitPath, source, "commit", "-m", "initial")
	runTestGit(t, gitPath, source, "branch", "-M", "main")
	runTestGit(t, gitPath, source, "branch", "article")
	runTestGit(t, gitPath, source, "branch", "result")

	storage, err := gitcli.NewStorage(filepath.Join(root, "managed"))
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	client, err := gitcli.NewClient(
		gitPath, "unused-askpass", 10*time.Second, 2, 1024*1024,
	)
	if err != nil {
		t.Fatalf("create Git client: %v", err)
	}
	instant := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	runtime := Runtime{
		Clock: clock.Fixed{Time: instant}, CloneTimeout: 30 * time.Second,
		Git: client, Storage: storage,
	}
	repository := Repository{
		ID:         "00000000-0000-4000-8000-000000000001",
		StorageKey: "00000000-0000-4000-8000-000000000002",
		Workspaces: mappingList(WorkspaceMappings{
			CodeBranch: "main", ArticleBranch: "article", ResultBranch: "result",
		}, instant),
	}
	connection := provider.Connection{
		CanonicalRemoteURL: source, DefaultBranch: "main",
		DisplayName: "server", FetchURL: source, Provider: "server_existing",
	}

	first, err := runtime.Synchronize(
		context.Background(), repository, connection, nil, "manual",
	)
	if err != nil {
		t.Fatalf("initial synchronize: %v", err)
	}
	if !first.Initial || len(first.Workspaces) != 3 || len(first.Commits) != 1 {
		t.Fatalf("unexpected initial result: %#v", first)
	}
	layout, err := storage.Layout(repository.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"code", "article", "result"} {
		if _, err := os.Stat(filepath.Join(layout.Worktrees[kind], ".git")); err != nil {
			t.Fatalf("%s worktree was not created: %v", kind, err)
		}
	}
	for index := range repository.Workspaces {
		for _, synced := range first.Workspaces {
			if repository.Workspaces[index].Workspace == synced.Workspace {
				head := synced.HeadCommitSHA
				tree := synced.TreeSHA
				repository.Workspaces[index].HeadCommitSHA = &head
				repository.Workspaces[index].TreeSHA = &tree
			}
		}
	}
	articleDirty := filepath.Join(layout.Worktrees["article"], "article-only.txt")
	if err := os.WriteFile(articleDirty, []byte("draft"), 0o600); err != nil {
		t.Fatal(err)
	}
	scoped, err := runtime.Synchronize(
		context.Background(), repository, connection,
		[]WorkspaceKind{WorkspaceCode}, "webhook",
	)
	if err != nil {
		t.Fatalf("code-only sync inspected the dirty Article worktree: %v", err)
	}
	if len(scoped.Workspaces) != 1 ||
		scoped.Workspaces[0].Workspace != WorkspaceCode {
		t.Fatalf("sync was not limited to code: %#v", scoped.Workspaces)
	}
	if err := os.Remove(articleDirty); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(source, "README.md"), []byte("second\n"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, gitPath, source, "add", "README.md")
	runTestGit(t, gitPath, source, "commit", "-m", "second")
	second, err := runtime.Synchronize(
		context.Background(), repository, connection, nil, "manual",
	)
	if err != nil {
		t.Fatalf("incremental synchronize: %v", err)
	}
	if second.Initial || len(second.Commits) != 2 {
		t.Fatalf("unexpected incremental result: %#v", second)
	}
	var codeHead string
	for _, workspace := range second.Workspaces {
		if workspace.Workspace == WorkspaceCode {
			codeHead = workspace.HeadCommitSHA
		}
	}
	if codeHead == "" || codeHead == *repository.Workspaces[0].HeadCommitSHA {
		t.Fatalf("code head did not advance: %s", codeHead)
	}
	if contents, err := os.ReadFile(filepath.Join(layout.Worktrees["code"], "README.md")); err != nil ||
		string(contents) != "second\n" {
		t.Fatalf("code worktree did not update: %q %v", contents, err)
	}

	if err := os.WriteFile(
		filepath.Join(layout.Worktrees["code"], "untracked.txt"),
		[]byte("must not be destroyed"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Synchronize(
		context.Background(), repository, connection, nil, "manual",
	)
	if !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("expected dirty worktree protection, got %v", err)
	}
}

func TestManagedRuntimeInitializesAndCommitsAllWorkspaces(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git is not installed")
	}
	root := t.TempDir()
	storage, err := gitcli.NewStorage(filepath.Join(root, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := gitcli.NewClient(
		gitPath, "unused-askpass", 10*time.Second, 2, 1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	runtime := Runtime{
		Clock: clock.Fixed{Time: instant}, CloneTimeout: 30 * time.Second,
		Git: client, Storage: storage,
	}
	repository := Repository{
		DefaultBranch: "main",
		ID:            "00000000-0000-4000-8000-000000000101",
		Provider:      ProviderManaged,
		Status:        StatusPending,
		StorageKey:    "00000000-0000-4000-8000-000000000102",
		Workspaces: mappingList(WorkspaceMappings{
			CodeBranch: "main", ArticleBranch: "article", ResultBranch: "result",
		}, instant),
	}
	connection, err := (provider.Managed{}).Test(context.Background(), provider.Config{
		Provider: "managed", CodeBranch: "main",
		ArticleBranch: "article", ResultBranch: "result",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Synchronize(
		context.Background(), repository, connection, nil, "manual",
	)
	if err != nil {
		t.Fatalf("initialize managed repository: %v", err)
	}
	if !result.Initial || len(result.Workspaces) != 3 || len(result.Commits) != 1 {
		t.Fatalf("unexpected managed initialization: %#v", result)
	}
	for index := range repository.Workspaces {
		for _, synced := range result.Workspaces {
			if synced.Workspace == repository.Workspaces[index].Workspace {
				head := synced.HeadCommitSHA
				tree := synced.TreeSHA
				repository.Workspaces[index].HeadCommitSHA = &head
				repository.Workspaces[index].TreeSHA = &tree
				repository.Workspaces[index].Status = WorkspaceReady
			}
		}
	}
	repository.Status = StatusReady
	layout, err := storage.Layout(repository.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	if remote := runTestGit(
		t, gitPath, layout.Repository,
		"--git-dir="+layout.Bare, "remote", "get-url", "origin",
	); remote != "bare.git" {
		t.Fatalf("managed origin should remain internal: %q", remote)
	}
	writer := WorkspaceWriter{
		Clock: clock.Fixed{Time: instant}, Git: client,
		Runtime: runtime, Storage: storage,
	}
	for index, workspace := range repository.Workspaces {
		request := WorkspaceCommitRequest{
			ActorEmail: "user@example.test", ActorID: "user-1", ActorName: "User",
			Changes: []FileChange{{
				Content: []byte(string(workspace.Workspace) + "\n"), Operation: "put",
				Path: string(workspace.Workspace) + ".txt",
			}},
			ExpectedHeadSHA: *workspace.HeadCommitSHA,
			IdempotencyKey:  "managed-workspace-" + string(workspace.Workspace),
			Message:         "test managed " + string(workspace.Workspace),
			ProjectID:       "project-1", Workspace: workspace.Workspace,
		}
		claim := CommitClaim{
			Repository: repository, Workspace: workspace,
		}
		prepared, err := writer.Prepare(
			context.Background(), claim, connection, request,
		)
		if err != nil {
			t.Fatalf("prepare %s commit: %v", workspace.Workspace, err)
		}
		pushed, err := writer.PushPrepared(
			context.Background(), claim, connection, prepared.CommitSHA,
		)
		if err != nil {
			t.Fatalf("push %s commit: %v", workspace.Workspace, err)
		}
		observed := runTestGit(
			t, gitPath, layout.Repository, "--git-dir="+layout.Bare,
			"rev-parse", "refs/heads/"+workspace.RemoteBranch,
		)
		if pushed.CommitSHA != observed || observed == request.ExpectedHeadSHA {
			t.Fatalf("%s branch did not advance: %#v", workspace.Workspace, pushed)
		}
		repository.Workspaces[index].HeadCommitSHA = &observed
	}
}

func TestRuntimeDetectsForcePushWithoutDiscardingOldObjects(t *testing.T) {
	reader, repository, head := readerFixture(t)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git is not installed")
	}
	layout, err := reader.Storage.Layout(repository.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	source := runTestGit(
		t, gitPath, layout.Repository,
		"--git-dir="+layout.Bare, "remote", "get-url", "origin",
	)
	rewrittenHead := runTestGit(
		t, gitPath, source, "rev-parse", head+"~2",
	)
	runTestGit(t, gitPath, source, "reset", "--hard", rewrittenHead)
	runtime := Runtime{
		Clock: reader.Clock, CloneTimeout: 30 * time.Second,
		Git: reader.Git, Storage: reader.Storage,
	}
	result, err := runtime.Synchronize(
		context.Background(), repository,
		provider.Connection{
			CanonicalRemoteURL: source, DefaultBranch: "main",
			DisplayName: "server", FetchURL: source, Provider: "server_existing",
		},
		[]WorkspaceKind{WorkspaceCode}, "webhook",
	)
	if err != nil {
		t.Fatalf("synchronize rewritten history: %v", err)
	}
	var code SyncedWorkspace
	for _, workspace := range result.Workspaces {
		if workspace.Workspace == WorkspaceCode {
			code = workspace
		}
	}
	if !code.HistoryRewritten || code.HeadCommitSHA != rewrittenHead {
		t.Fatalf("force push was not detected: %#v", code)
	}
	if observed := runTestGit(
		t, gitPath, layout.Worktrees["code"], "rev-parse", "HEAD",
	); observed != rewrittenHead {
		t.Fatalf("code worktree did not follow rewritten head: %s", observed)
	}
	if observed := runTestGit(
		t, gitPath, layout.Repository,
		"--git-dir="+layout.Bare, "cat-file", "-t", head,
	); observed != "commit" {
		t.Fatalf("old commit object was discarded: %s", observed)
	}
}

func runTestGit(t *testing.T, gitPath, directory string, args ...string) string {
	t.Helper()
	command := exec.Command(gitPath, args...)
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"git %s failed: %v\n%s",
			strings.Join(args, " "), err, strings.TrimSpace(string(output)),
		)
	}
	return strings.TrimSpace(string(output))
}
