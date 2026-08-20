package repo

import (
	"bytes"
	"context"
	"fmt"
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

func TestReaderPinsPaginationAndClassifiesImmutableObjects(t *testing.T) {
	reader, repository, head := readerFixture(t)
	ctx := context.Background()

	branches, err := reader.ListBranches(ctx, repository)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	if len(branches.Items) != 3 {
		t.Fatalf("unexpected branches: %#v", branches.Items)
	}
	for _, branch := range branches.Items {
		if branch.Workspace == nil {
			t.Fatalf("mapped branch has no workspace: %#v", branch)
		}
		if branch.Name == "main" && !branch.Default {
			t.Fatalf("main was not marked default: %#v", branch)
		}
	}

	first, err := reader.ListCommits(
		ctx, repository, WorkspaceCode, "", 1,
	)
	if err != nil {
		t.Fatalf("list first commit page: %v", err)
	}
	if first.ResolvedRevision != head ||
		len(first.Items) != 1 ||
		!first.HasMore ||
		first.NextCursor == nil {
		t.Fatalf("unexpected first commit page: %#v", first)
	}
	movedRepository := repository
	movedRepository.Workspaces = append([]Workspace(nil), repository.Workspaces...)
	movedHead := strings.Repeat("a", 40)
	movedRepository.Workspaces[0].HeadCommitSHA = &movedHead
	second, err := reader.ListCommits(
		ctx, movedRepository, WorkspaceCode, *first.NextCursor, 1,
	)
	if err != nil {
		t.Fatalf("list second commit page: %v", err)
	}
	if second.ResolvedRevision != head ||
		len(second.Items) != 1 ||
		second.Items[0].CommitSHA == first.Items[0].CommitSHA {
		t.Fatalf("commit cursor did not advance a pinned history: %#v", second)
	}
	if _, err := reader.ListCommits(
		ctx, repository, WorkspaceArticle, *first.NextCursor, 1,
	); err != ErrInvalid {
		t.Fatalf("cross-workspace cursor must fail, got %v", err)
	}

	commit, err := reader.GetCommit(ctx, repository, head)
	if err != nil {
		t.Fatalf("get commit: %v", err)
	}
	foundRename := false
	for _, change := range commit.Changes {
		if change.Status == "renamed" &&
			change.PreviousPath == "old.txt" &&
			change.Path == "new.txt" {
			foundRename = true
		}
	}
	if !foundRename {
		t.Fatalf("rename was not normalized: %#v", commit.Changes)
	}

	tree, err := reader.ListTree(
		ctx, repository, WorkspaceCode, head, "", "", 2,
	)
	if err != nil {
		t.Fatalf("list root tree: %v", err)
	}
	if len(tree.Items) != 2 || !tree.HasMore || tree.NextCursor == nil {
		t.Fatalf("unexpected root tree page: %#v", tree)
	}
	nextTree, err := reader.ListTree(
		ctx, repository, WorkspaceCode, head, "", *tree.NextCursor, 2,
	)
	if err != nil || len(nextTree.Items) == 0 {
		t.Fatalf("list next tree page: %#v %v", nextTree, err)
	}
	subtree, err := reader.ListTree(
		ctx, repository, WorkspaceCode, head, "dir", "", 100,
	)
	if err != nil {
		t.Fatalf("list subtree: %v", err)
	}
	if len(subtree.Items) != 1 ||
		subtree.Items[0].Path != "dir/中文 #.txt" {
		t.Fatalf("special path was not preserved: %#v", subtree.Items)
	}
	largeTree, err := reader.ListTree(
		ctx, repository, WorkspaceCode, head, "many", "", 200,
	)
	if err != nil || len(largeTree.Items) != 200 ||
		!largeTree.HasMore || largeTree.NextCursor == nil {
		t.Fatalf("large directory page was not bounded: %#v %v", largeTree, err)
	}
	largeTail, err := reader.ListTree(
		ctx, repository, WorkspaceCode, head, "many",
		*largeTree.NextCursor, 200,
	)
	if err != nil || len(largeTail.Items) != 5 || largeTail.HasMore {
		t.Fatalf("large directory tail was not stable: %#v %v", largeTail, err)
	}

	assertPreview(t, reader, repository, head, "dir/中文 #.txt", "text", "你好\n")
	assertPreview(t, reader, repository, head, "binary.bin", "binary", "")
	raw, err := reader.ReadRawFile(
		ctx, repository, WorkspaceCode, head, "binary.bin",
	)
	if err != nil || !bytes.Equal(raw.Content, []byte{'a', 0, 'b'}) {
		t.Fatalf("read raw binary: err=%v content=%v", err, raw.Content)
	}
	if raw.Path != "binary.bin" || raw.ResolvedRevision != head || raw.Size != 3 {
		t.Fatalf("raw metadata mismatch: %+v", raw)
	}
	nestedRaw, err := reader.ReadRawFile(
		ctx, repository, WorkspaceResult, head,
		"experiments/demo/figures/dependency-plot.png",
	)
	if err != nil || !bytes.Equal(nestedRaw.Content, []byte{'p', 0, 'n', 'g'}) {
		t.Fatalf("read nested raw image: err=%v content=%v", err, nestedRaw.Content)
	}
	if _, err := reader.ReadRawFile(
		ctx, repository, WorkspaceCode, head, "pointer.lfs",
	); err != ErrObjectNotFound {
		t.Fatalf("LFS pointer must not be served as raw content, got %v", err)
	}
	assertPreview(t, reader, repository, head, "large.txt", "too_large", "")
	assertPreview(t, reader, repository, head, "pointer.lfs", "lfs_not_materialized", "")
	assertPreview(t, reader, repository, head, "link", "symlink", "README.md")
	assertPreview(t, reader, repository, head, "vendor/submodule", "submodule", "")

	if _, err := reader.ReadFile(
		ctx, repository, WorkspaceCode, head, "../secret",
	); err != ErrInvalid {
		t.Fatalf("path traversal must fail, got %v", err)
	}
	if _, err := reader.ListTree(
		ctx, repository, WorkspaceCode, "deadbeef", "", "", 10,
	); err != ErrInvalid {
		t.Fatalf("short revision must fail, got %v", err)
	}
}

func assertPreview(
	t *testing.T,
	reader Reader,
	repository Repository,
	revision string,
	repositoryPath string,
	status string,
	expected string,
) {
	t.Helper()
	content, err := reader.ReadFile(
		context.Background(), repository, WorkspaceCode,
		revision, repositoryPath,
	)
	if err != nil {
		t.Fatalf("read %s: %v", repositoryPath, err)
	}
	if content.PreviewStatus != status {
		t.Fatalf(
			"%s: got status %s, want %s",
			repositoryPath, content.PreviewStatus, status,
		)
	}
	if expected == "" {
		if content.Content != nil {
			t.Fatalf("%s unexpectedly returned content", repositoryPath)
		}
		return
	}
	if content.Content == nil || *content.Content != expected {
		t.Fatalf("%s: unexpected content %#v", repositoryPath, content.Content)
	}
}

func readerFixture(t *testing.T) (Reader, Repository, string) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git is not installed")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	runTestGit(t, gitPath, root, "init", source)
	runTestGit(t, gitPath, source, "config", "user.name", "Repo Reader")
	runTestGit(t, gitPath, source, "config", "user.email", "reader@example.test")
	writeFixtureFile(t, source, "README.md", []byte("initial\n"))
	runTestGit(t, gitPath, source, "add", "README.md")
	runTestGit(t, gitPath, source, "commit", "-m", "initial")
	runTestGit(t, gitPath, source, "branch", "-M", "main")
	initial := runTestGit(t, gitPath, source, "rev-parse", "HEAD")

	writeFixtureFile(t, source, "dir/中文 #.txt", []byte("你好\n"))
	writeFixtureFile(t, source, "binary.bin", []byte{'a', 0, 'b'})
	writeFixtureFile(t, source, "experiments/demo/figures/dependency-plot.png", []byte{'p', 0, 'n', 'g'})
	writeFixtureFile(t, source, "large.txt", []byte(strings.Repeat("large", 100)))
	writeFixtureFile(t, source, "pointer.lfs", []byte(
		"version https://git-lfs.github.com/spec/v1\n"+
			"oid sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"+
			"size 42\n",
	))
	writeFixtureFile(t, source, "old.txt", []byte("rename me\n"))
	for index := 0; index < 205; index++ {
		writeFixtureFile(
			t, source, filepath.ToSlash(
				filepath.Join("many", fmt.Sprintf("file-%03d.txt", index)),
			),
			[]byte("entry\n"),
		)
	}
	linkSource := filepath.Join(source, ".link-source")
	if err := os.WriteFile(linkSource, []byte("README.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkBlob := runTestGit(t, gitPath, source, "hash-object", "-w", ".link-source")
	if err := os.Remove(linkSource); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, gitPath, source, "add", ".")
	runTestGit(
		t, gitPath, source, "update-index", "--add", "--cacheinfo",
		"120000,"+linkBlob+",link",
	)
	runTestGit(
		t, gitPath, source, "update-index", "--add", "--cacheinfo",
		"160000,"+initial+",vendor/submodule",
	)
	runTestGit(t, gitPath, source, "commit", "-m", "add immutable objects")

	runTestGit(t, gitPath, source, "mv", "old.txt", "new.txt")
	writeFixtureFile(t, source, "README.md", []byte("updated\n"))
	runTestGit(t, gitPath, source, "add", "README.md")
	runTestGit(t, gitPath, source, "commit", "-m", "rename old file")
	head := runTestGit(t, gitPath, source, "rev-parse", "HEAD")
	runTestGit(t, gitPath, source, "branch", "article")
	runTestGit(t, gitPath, source, "branch", "result")

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
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	repository := Repository{
		DefaultBranch: "main",
		ID:            "00000000-0000-4000-8000-000000000011",
		Provider:      ProviderLocal,
		Status:        StatusPending,
		StorageKey:    "00000000-0000-4000-8000-000000000012",
		Workspaces: mappingList(WorkspaceMappings{
			CodeBranch: "main", ArticleBranch: "article", ResultBranch: "result",
		}, now),
	}
	runtime := Runtime{
		Clock: clock.Fixed{Time: now}, CloneTimeout: 30 * time.Second,
		Git: client, Storage: storage,
	}
	synced, err := runtime.Synchronize(
		context.Background(), repository,
		provider.Connection{
			CanonicalRemoteURL: source, DefaultBranch: "main",
			DisplayName: "local", FetchURL: source, Provider: "local",
		},
		"manual",
	)
	if err != nil {
		t.Fatalf("synchronize reader fixture: %v", err)
	}
	for index := range repository.Workspaces {
		for _, workspace := range synced.Workspaces {
			if workspace.Workspace == repository.Workspaces[index].Workspace {
				head := workspace.HeadCommitSHA
				tree := workspace.TreeSHA
				repository.Workspaces[index].HeadCommitSHA = &head
				repository.Workspaces[index].TreeSHA = &tree
				repository.Workspaces[index].Status = WorkspaceReady
			}
		}
	}
	repository.Status = StatusReady
	return Reader{
		Clock: clock.Fixed{Time: now}, Git: client,
		MaxTextBytes: 256, Storage: storage,
	}, repository, head
}

func writeFixtureFile(t *testing.T, root, relative string, contents []byte) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
