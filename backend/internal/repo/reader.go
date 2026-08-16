package repo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

const (
	defaultCommitLimit = 30
	defaultTreeLimit   = 100
	maxCursorOffset    = 1000000
	maxCommitLimit     = 100
	maxTreeLimit       = 200
)

// Reader serves immutable Git object reads from Core-managed bare repositories.
type Reader struct {
	Clock        interface{ Now() time.Time }
	Git          *gitcli.Client
	MaxTextBytes int64
	Storage      *gitcli.Storage
}

func (reader Reader) ListBranches(
	ctx context.Context,
	repository Repository,
) (BranchList, error) {
	layout, err := reader.readyLayout(repository)
	if err != nil {
		return BranchList{}, err
	}
	result, err := reader.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"for-each-ref",
			"--format=%(refname:strip=3)%00%(objectname)%00",
			"refs/remotes/origin",
		},
		Directory: layout.Repository, Operation: "repo.branches.list",
	})
	if err != nil {
		return BranchList{}, err
	}
	branchOutput := bytes.ReplaceAll(result.Stdout, []byte{0, '\r', '\n'}, []byte{0})
	branchOutput = bytes.ReplaceAll(branchOutput, []byte{0, '\n'}, []byte{0})
	branchOutput = bytes.TrimRight(branchOutput, "\r\n")
	parsed, err := gitcli.ParseBranches(branchOutput)
	if err != nil {
		return BranchList{}, err
	}
	mappings := map[string]WorkspaceKind{}
	for _, workspace := range repository.Workspaces {
		mappings[workspace.RemoteBranch] = workspace.Workspace
	}
	items := make([]Branch, 0, len(parsed))
	for _, branch := range parsed {
		if branch.Name == "HEAD" {
			continue
		}
		var workspace *WorkspaceKind
		if kind, exists := mappings[branch.Name]; exists {
			kind := kind
			workspace = &kind
		}
		items = append(items, Branch{
			CommitSHA: branch.CommitSHA,
			Default:   branch.Name == repository.DefaultBranch,
			Name:      branch.Name,
			Workspace: workspace,
		})
	}
	return BranchList{Items: items}, nil
}

func (reader Reader) ListCommits(
	ctx context.Context,
	repository Repository,
	workspaceKind WorkspaceKind,
	cursor string,
	limit int,
) (CommitPage, error) {
	layout, err := reader.readyLayout(repository)
	if err != nil {
		return CommitPage{}, err
	}
	workspace, err := findWorkspace(repository, workspaceKind)
	if err != nil {
		return CommitPage{}, err
	}
	if workspace.HeadCommitSHA == nil {
		return CommitPage{}, ErrNotReady
	}
	limit = normalizeLimit(limit, defaultCommitLimit, maxCommitLimit)
	offset := 0
	revision := *workspace.HeadCommitSHA
	if cursor != "" {
		decoded, err := decodeCursor(cursor)
		if err != nil ||
			decoded.Kind != "commits" ||
			decoded.Workspace != workspaceKind ||
			decoded.Branch != workspace.RemoteBranch ||
			decoded.Path != "" ||
			decoded.Offset < 0 ||
			decoded.Offset > maxCursorOffset ||
			gitcli.ValidateFullSHA(decoded.Revision) != nil {
			return CommitPage{}, ErrInvalid
		}
		revision = decoded.Revision
		offset = decoded.Offset
	}
	if _, err := reader.resolveCommit(ctx, layout, revision); err != nil {
		return CommitPage{}, err
	}
	result, err := reader.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"log", "--format=%H",
			"--max-count=" + strconv.Itoa(limit+1),
			"--skip=" + strconv.Itoa(offset),
			revision, "--",
		},
		Directory: layout.Repository, Operation: "repo.commits.list",
	})
	if err != nil {
		return CommitPage{}, err
	}
	shas, err := parseSHALines(result.Stdout)
	if err != nil {
		return CommitPage{}, err
	}
	hasMore := len(shas) > limit
	if hasMore {
		shas = shas[:limit]
	}
	runtime := Runtime{Clock: reader.Clock, Git: reader.Git, Storage: reader.Storage}
	items := make([]Commit, 0, len(shas))
	for _, sha := range shas {
		commit, err := runtime.readCommit(
			ctx, layout, repository.ID, sha, false,
		)
		if err != nil {
			return CommitPage{}, err
		}
		commit.Source = "reference"
		commit.FirstSeenAt = commit.Committer.Time.UTC()
		commit.Changes = []ChangedPath{}
		items = append(items, commit)
	}
	var nextCursor *string
	if hasMore {
		encoded, err := encodeCursor(pageCursor{
			Branch: workspace.RemoteBranch, Kind: "commits",
			Offset: offset + limit, Revision: revision,
			Workspace: workspaceKind,
		})
		if err != nil {
			return CommitPage{}, err
		}
		nextCursor = &encoded
	}
	return CommitPage{
		Branch: workspace.RemoteBranch, HasMore: hasMore, Items: items,
		NextCursor: nextCursor, ResolvedRevision: revision,
		Workspace: workspaceKind,
	}, nil
}

func (reader Reader) GetCommit(
	ctx context.Context,
	repository Repository,
	commitSHA string,
) (Commit, error) {
	layout, err := reader.readyLayout(repository)
	if err != nil {
		return Commit{}, err
	}
	commitSHA, err = reader.resolveCommit(ctx, layout, commitSHA)
	if err != nil {
		return Commit{}, err
	}
	runtime := Runtime{Clock: reader.Clock, Git: reader.Git, Storage: reader.Storage}
	commit, err := runtime.readCommit(
		ctx, layout, repository.ID, commitSHA, false,
	)
	if err != nil {
		return Commit{}, err
	}
	commit.Source = "reference"
	commit.FirstSeenAt = commit.Committer.Time.UTC()
	result, err := reader.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"diff-tree", "--root", "--no-commit-id",
			"--name-status", "-z", "-r", "-M", "-C",
			commitSHA, "--",
		},
		Directory: layout.Repository, Operation: "repo.commit.changes",
	})
	if err != nil {
		return Commit{}, err
	}
	changes, err := gitcli.ParseDiffNameStatus(result.Stdout)
	if err != nil {
		return Commit{}, err
	}
	commit.Changes = make([]ChangedPath, 0, len(changes))
	for _, change := range changes {
		commit.Changes = append(commit.Changes, ChangedPath{
			Path: change.Path, PreviousPath: change.PreviousPath,
			Status: normalizeChangeStatus(change.Status),
		})
	}
	return commit, nil
}

func (reader Reader) ListTree(
	ctx context.Context,
	repository Repository,
	workspaceKind WorkspaceKind,
	revision string,
	repositoryPath string,
	cursor string,
	limit int,
) (TreePage, error) {
	layout, err := reader.readyLayout(repository)
	if err != nil {
		return TreePage{}, err
	}
	workspace, err := findWorkspace(repository, workspaceKind)
	if err != nil {
		return TreePage{}, err
	}
	if gitcli.ValidateRepoPath(repositoryPath, true) != nil {
		return TreePage{}, ErrInvalid
	}
	revision, err = reader.resolveCommit(ctx, layout, revision)
	if err != nil {
		return TreePage{}, err
	}
	limit = normalizeLimit(limit, defaultTreeLimit, maxTreeLimit)
	offset := 0
	if cursor != "" {
		decoded, err := decodeCursor(cursor)
		if err != nil ||
			decoded.Kind != "tree" ||
			decoded.Workspace != workspaceKind ||
			decoded.Branch != workspace.RemoteBranch ||
			decoded.Revision != revision ||
			decoded.Path != repositoryPath ||
			decoded.Offset < 0 ||
			decoded.Offset > maxCursorOffset {
			return TreePage{}, ErrInvalid
		}
		offset = decoded.Offset
	}
	treeish := revision
	if repositoryPath != "" {
		treeish += ":" + repositoryPath
	}
	result, err := reader.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"ls-tree", "-z", "-l", treeish,
		},
		Directory: layout.Repository, Operation: "repo.tree.list",
	})
	if err != nil {
		return TreePage{}, ErrObjectNotFound
	}
	parsed, err := gitcli.ParseTree(result.Stdout)
	if err != nil {
		return TreePage{}, err
	}
	if offset > len(parsed) {
		return TreePage{}, ErrInvalid
	}
	end := offset + limit
	hasMore := end < len(parsed)
	if end > len(parsed) {
		end = len(parsed)
	}
	items := make([]TreeEntry, 0, end-offset)
	for _, entry := range parsed[offset:end] {
		fullPath := entry.Path
		if repositoryPath != "" {
			fullPath = path.Join(repositoryPath, entry.Path)
		}
		items = append(items, TreeEntry{
			Kind: treeEntryKind(entry), Mode: entry.Mode,
			Name: entry.Path, ObjectID: entry.ObjectID,
			Path: fullPath, Size: entry.Size,
		})
	}
	var nextCursor *string
	if hasMore {
		encoded, err := encodeCursor(pageCursor{
			Branch: workspace.RemoteBranch, Kind: "tree",
			Offset: end, Path: repositoryPath, Revision: revision,
			Workspace: workspaceKind,
		})
		if err != nil {
			return TreePage{}, err
		}
		nextCursor = &encoded
	}
	return TreePage{
		Branch: workspace.RemoteBranch, HasMore: hasMore, Items: items,
		NextCursor: nextCursor, Path: repositoryPath,
		ResolvedRevision: revision, Workspace: workspaceKind,
	}, nil
}

func (reader Reader) ReadFile(
	ctx context.Context,
	repository Repository,
	workspaceKind WorkspaceKind,
	revision string,
	repositoryPath string,
) (FileContent, error) {
	layout, err := reader.readyLayout(repository)
	if err != nil {
		return FileContent{}, err
	}
	workspace, err := findWorkspace(repository, workspaceKind)
	if err != nil {
		return FileContent{}, err
	}
	if gitcli.ValidateRepoPath(repositoryPath, false) != nil {
		return FileContent{}, ErrInvalid
	}
	revision, err = reader.resolveCommit(ctx, layout, revision)
	if err != nil {
		return FileContent{}, err
	}
	result, err := reader.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"ls-tree", "-z", "-l", revision,
			"--", ":(top,literal)" + repositoryPath,
		},
		Directory: layout.Repository, Operation: "repo.content.lookup",
	})
	if err != nil {
		return FileContent{}, ErrObjectNotFound
	}
	entries, err := gitcli.ParseTree(result.Stdout)
	if err != nil || len(entries) != 1 || entries[0].Path != repositoryPath {
		return FileContent{}, ErrObjectNotFound
	}
	entry := entries[0]
	kind := treeEntryKind(entry)
	if kind == "directory" {
		return FileContent{}, ErrObjectNotFound
	}
	size := int64(0)
	if entry.Size != nil {
		size = *entry.Size
	}
	response := FileContent{
		Branch: workspace.RemoteBranch, Kind: kind, Mode: entry.Mode,
		ObjectID: entry.ObjectID, Path: repositoryPath,
		ResolvedRevision: revision, Size: size, Workspace: workspaceKind,
	}
	switch kind {
	case "submodule":
		response.PreviewStatus = "submodule"
		return response, nil
	case "file", "symlink":
	default:
		return FileContent{}, ErrObjectNotFound
	}
	maxBytes := reader.MaxTextBytes
	if maxBytes <= 0 {
		maxBytes = 1024 * 1024
	}
	if size > maxBytes {
		response.PreviewStatus = "too_large"
		return response, nil
	}
	contents, err := reader.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"cat-file", "blob", entry.ObjectID,
		},
		Directory: layout.Repository, Operation: "repo.content.read",
	})
	if err != nil {
		return FileContent{}, err
	}
	if kind == "symlink" {
		response.PreviewStatus = "symlink"
		if utf8.Valid(contents.Stdout) && !bytes.ContainsRune(contents.Stdout, 0) {
			value := string(contents.Stdout)
			encoding := "utf-8"
			response.Content = &value
			response.Encoding = &encoding
		}
		return response, nil
	}
	if isLFSPointer(contents.Stdout) {
		response.PreviewStatus = "lfs_not_materialized"
		return response, nil
	}
	if !utf8.Valid(contents.Stdout) || bytes.ContainsRune(contents.Stdout, 0) {
		response.PreviewStatus = "binary"
		return response, nil
	}
	value := string(contents.Stdout)
	encoding := "utf-8"
	response.Content = &value
	response.Encoding = &encoding
	response.PreviewStatus = "text"
	return response, nil
}

// HashBlob streams one immutable regular-file blob through SHA-256 without
// buffering its contents or exposing a managed worktree path.
func (reader Reader) HashBlob(
	ctx context.Context,
	repository Repository,
	objectID string,
	expectedSize int64,
) (string, error) {
	if gitcli.ValidateFullSHA(objectID) != nil || expectedSize < 0 || expectedSize > 10<<30 {
		return "", ErrInvalid
	}
	layout, err := reader.readyLayout(repository)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	result, err := reader.Git.RunStream(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare, "cat-file", "blob", objectID,
		},
		Directory: layout.Repository, Operation: "repo.content.hash",
	}, hasher, expectedSize)
	if err != nil || result.Bytes != expectedSize {
		if err != nil {
			return "", err
		}
		return "", ErrObjectNotFound
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (reader Reader) readyLayout(repository Repository) (gitcli.Layout, error) {
	if repository.Status != StatusReady && repository.Status != StatusSyncing {
		return gitcli.Layout{}, ErrNotReady
	}
	return reader.Storage.Layout(repository.StorageKey)
}

func (reader Reader) resolveCommit(
	ctx context.Context,
	layout gitcli.Layout,
	revision string,
) (string, error) {
	if gitcli.ValidateFullSHA(revision) != nil {
		return "", ErrInvalid
	}
	result, err := reader.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare,
			"rev-parse", "--verify", "--end-of-options", revision + "^{commit}",
		},
		Directory: layout.Repository, Operation: "repo.revision.resolve",
	})
	if err != nil {
		return "", ErrObjectNotFound
	}
	resolved := strings.TrimSpace(string(result.Stdout))
	if gitcli.ValidateFullSHA(resolved) != nil {
		return "", ErrObjectNotFound
	}
	return resolved, nil
}

func findWorkspace(
	repository Repository,
	kind WorkspaceKind,
) (Workspace, error) {
	if kind != WorkspaceCode &&
		kind != WorkspaceArticle &&
		kind != WorkspaceResult {
		return Workspace{}, ErrInvalid
	}
	for _, workspace := range repository.Workspaces {
		if workspace.Workspace == kind {
			if workspace.Status != WorkspaceReady {
				return Workspace{}, ErrNotReady
			}
			return workspace, nil
		}
	}
	return Workspace{}, ErrBranchMapping
}

func normalizeLimit(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func parseSHALines(contents []byte) ([]string, error) {
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, nil
	}
	shas := make([]string, 0, len(lines))
	for _, line := range lines {
		sha := strings.TrimSpace(line)
		if gitcli.ValidateFullSHA(sha) != nil {
			return nil, fmt.Errorf("parse commit list")
		}
		shas = append(shas, sha)
	}
	return shas, nil
}

func normalizeChangeStatus(status string) string {
	if status == "" {
		return "unknown"
	}
	switch status[0] {
	case 'A':
		return "added"
	case 'M':
		return "modified"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	case 'T':
		return "type_changed"
	case 'U':
		return "unmerged"
	default:
		return "unknown"
	}
}

func treeEntryKind(entry gitcli.TreeEntry) string {
	switch {
	case entry.Mode == "160000" || entry.Type == "commit":
		return "submodule"
	case entry.Mode == "120000":
		return "symlink"
	case entry.Type == "tree":
		return "directory"
	default:
		return "file"
	}
}

func isLFSPointer(contents []byte) bool {
	if len(contents) > 2048 {
		return false
	}
	text := string(contents)
	return strings.HasPrefix(
		text, "version https://git-lfs.github.com/spec/v1\n",
	) && strings.Contains(text, "\noid sha256:")
}

type pageCursor struct {
	Branch    string        `json:"branch"`
	Kind      string        `json:"kind"`
	Offset    int           `json:"offset"`
	Path      string        `json:"path,omitempty"`
	Revision  string        `json:"revision"`
	Workspace WorkspaceKind `json:"workspace"`
}

func encodeCursor(cursor pageCursor) (string, error) {
	contents, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(contents), nil
}

func decodeCursor(value string) (pageCursor, error) {
	if len(value) > 2048 {
		return pageCursor{}, ErrInvalid
	}
	contents, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return pageCursor{}, ErrInvalid
	}
	var cursor pageCursor
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return pageCursor{}, ErrInvalid
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return pageCursor{}, ErrInvalid
	}
	return cursor, nil
}
