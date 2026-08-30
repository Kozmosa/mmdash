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
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
)

func TestWorkspaceWriterPushesRecoversAndReplaysPreparedCommit(t *testing.T) {
	reader, repository, originalHead := readerFixture(t)
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
	remote := filepath.Join(filepath.Dir(source), "writer-remote.git")
	runTestGit(t, gitPath, filepath.Dir(source), "clone", "--bare", source, remote)
	runTestGit(
		t, gitPath, layout.Repository, "--git-dir="+layout.Bare,
		"remote", "set-url", "origin", remote,
	)
	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	runtime := Runtime{
		Clock: clock.Fixed{Time: now}, CloneTimeout: 30 * time.Second,
		Git: reader.Git, Storage: reader.Storage,
	}
	writer := WorkspaceWriter{
		Clock: clock.Fixed{Time: now}, Git: reader.Git,
		Runtime: runtime, Storage: reader.Storage,
	}
	code, err := findWorkspace(repository, WorkspaceCode)
	if err != nil {
		t.Fatal(err)
	}
	claim := CommitClaim{
		Owner: "write-1", Repository: repository, Workspace: code,
	}
	request := WorkspaceCommitRequest{
		ActorEmail: "writer@example.test", ActorID: "user-1",
		ActorName: "Repo Writer",
		Changes: []FileChange{{
			Content:   []byte("created by mmdash\n"),
			Operation: "put", Path: "generated/result.txt",
		}},
		ExpectedHeadSHA: originalHead, IdempotencyKey: "request-1",
		Message:   "docs(repo): add generated result",
		ProjectID: repository.ProjectID, Workspace: WorkspaceCode,
	}
	connection := provider.Connection{Provider: "server_existing"}
	prepared, err := writer.Prepare(
		context.Background(), claim, connection, request,
	)
	if err != nil {
		t.Fatalf("prepare commit: %v", err)
	}
	if prepared.CommitSHA == originalHead || prepared.Source != "mmdash" {
		t.Fatalf("unexpected prepared commit: %#v", prepared)
	}
	if observed := runTestGit(
		t, gitPath, filepath.Dir(remote),
		"--git-dir="+remote, "rev-parse", "main",
	); observed != originalHead {
		t.Fatalf("prepare changed remote head: %s", observed)
	}
	pushed, err := writer.PushPrepared(
		context.Background(), claim, connection, prepared.CommitSHA,
	)
	if err != nil {
		t.Fatalf("push prepared commit: %v", err)
	}
	if pushed.CommitSHA != prepared.CommitSHA {
		t.Fatalf("unexpected pushed commit: %#v", pushed)
	}
	if observed := runTestGit(
		t, gitPath, filepath.Dir(remote),
		"--git-dir="+remote, "rev-parse", "main",
	); observed != prepared.CommitSHA {
		t.Fatalf("remote did not advance: %s", observed)
	}
	replayed, err := writer.PushPrepared(
		context.Background(), claim, connection, prepared.CommitSHA,
	)
	if err != nil || replayed.CommitSHA != prepared.CommitSHA {
		t.Fatalf("idempotent prepared replay failed: %#v %v", replayed, err)
	}

	external := filepath.Join(filepath.Dir(source), "external-writer")
	runTestGit(t, gitPath, filepath.Dir(source), "clone", remote, external)
	runTestGit(t, gitPath, external, "config", "user.name", "External Writer")
	runTestGit(t, gitPath, external, "config", "user.email", "external@example.test")
	externalHead := commitExternalChange(
		t, gitPath, external, "external.txt", "external\n", "external change",
	)
	code.HeadCommitSHA = &externalHead
	claim.Workspace = code
	raceRequest := request
	raceRequest.ExpectedHeadSHA = externalHead
	raceRequest.IdempotencyKey = "request-2"
	raceRequest.Changes = []FileChange{{
		Content:   []byte("must be rolled back\n"),
		Operation: "put", Path: "generated/race.txt",
	}}
	racePrepared, err := writer.Prepare(
		context.Background(), claim, connection, raceRequest,
	)
	if err != nil {
		t.Fatalf("prepare racing commit: %v", err)
	}
	racingHead := commitExternalChange(
		t, gitPath, external, "racing.txt", "racing\n", "racing change",
	)
	_, err = writer.PushPrepared(
		context.Background(), claim, connection, racePrepared.CommitSHA,
	)
	if !errors.Is(err, ErrHeadChanged) {
		t.Fatalf("expected non-fast-forward conflict, got %v", err)
	}
	worktree := layout.Worktrees["code"]
	if head := runTestGit(t, gitPath, worktree, "rev-parse", "HEAD"); head != racingHead {
		t.Fatalf("managed worktree was not recovered: %s", head)
	}
	if _, err := os.Stat(filepath.Join(worktree, "generated", "race.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed write survived recovery: %v", err)
	}
}

func TestRuntimeCreatesAndReleasesDetachedCheckout(t *testing.T) {
	reader, repository, head := readerFixture(t)
	runtime := Runtime{
		Clock: reader.Clock, CloneTimeout: 30 * time.Second,
		Git: reader.Git, Storage: reader.Storage,
	}
	relative, err := runtime.CreateCheckout(
		context.Background(), repository,
		"00000000-0000-4000-8000-000000000099", head,
	)
	if err != nil {
		t.Fatalf("create checkout: %v", err)
	}
	target, err := reader.Storage.ManagedPath(repository.StorageKey, relative)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Fatalf("detached checkout is incomplete: %v", err)
	}
	if err := runtime.ReleaseCheckout(
		context.Background(), repository, relative,
	); err != nil {
		t.Fatalf("release checkout: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checkout path still exists: %v", err)
	}
	if err := runtime.ReleaseCheckout(
		context.Background(), repository, relative,
	); err != nil {
		t.Fatalf("release must be idempotent: %v", err)
	}
}

func TestRuntimeReconcileWorktrees(t *testing.T) {
	reader, repository, head := readerFixture(t)
	runtime := Runtime{
		Clock: reader.Clock, CloneTimeout: 30 * time.Second,
		Git: reader.Git, Storage: reader.Storage,
	}
	activeRelative, err := runtime.CreateCheckout(
		context.Background(), repository,
		"00000000-0000-4000-8000-000000000101", head,
	)
	if err != nil {
		t.Fatalf("create active checkout: %v", err)
	}
	orphanRelative, err := runtime.CreateCheckout(
		context.Background(), repository,
		"00000000-0000-4000-8000-000000000102", head,
	)
	if err != nil {
		t.Fatalf("create orphan checkout: %v", err)
	}
	activePath, err := reader.Storage.ManagedPath(
		repository.StorageKey, activeRelative,
	)
	if err != nil {
		t.Fatal(err)
	}
	orphanPath, err := reader.Storage.ManagedPath(
		repository.StorageKey, orphanRelative,
	)
	if err != nil {
		t.Fatal(err)
	}
	active := Checkout{
		CheckoutID:      "00000000-0000-4000-8000-000000000101",
		CheckoutRelpath: activeRelative,
	}
	result, err := runtime.ReconcileWorktrees(
		context.Background(), repository, []Checkout{active},
	)
	if err != nil {
		t.Fatalf("reconcile worktrees: %v", err)
	}
	if result.NeedsSync || len(result.MissingCheckoutIDs) != 0 {
		t.Fatalf("unexpected reconciliation result: %#v", result)
	}
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("active checkout was removed: %v", err)
	}
	if _, err := os.Stat(orphanPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan checkout survived: %v", err)
	}

	for index := range repository.Workspaces {
		if repository.Workspaces[index].Workspace == WorkspaceCode {
			mismatched := strings.Repeat("0", 40)
			repository.Workspaces[index].HeadCommitSHA = &mismatched
		}
	}
	result, err = runtime.ReconcileWorktrees(
		context.Background(), repository, []Checkout{active},
	)
	if err != nil {
		t.Fatalf("detect mismatched worktree: %v", err)
	}
	if !result.NeedsSync {
		t.Fatal("mismatched long-lived worktree did not request synchronization")
	}

	if err := runtime.ReleaseCheckout(
		context.Background(), repository, activeRelative,
	); err != nil {
		t.Fatalf("remove active checkout: %v", err)
	}
	result, err = runtime.ReconcileWorktrees(
		context.Background(), repository, []Checkout{active},
	)
	if err != nil {
		t.Fatalf("detect missing checkout: %v", err)
	}
	if len(result.MissingCheckoutIDs) != 1 ||
		result.MissingCheckoutIDs[0] != active.CheckoutID {
		t.Fatalf("missing checkout was not detected: %#v", result)
	}
}

func commitExternalChange(
	t *testing.T,
	gitPath string,
	source string,
	relative string,
	contents string,
	message string,
) string {
	t.Helper()
	writeFixtureFile(t, source, relative, []byte(contents))
	runTestGit(t, gitPath, source, "add", "--", relative)
	runTestGit(t, gitPath, source, "commit", "-m", message)
	runTestGit(t, gitPath, source, "push", "origin", "main")
	return runTestGit(t, gitPath, source, "rev-parse", "HEAD")
}
