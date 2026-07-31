package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/repo"
)

// GitContentReader is the only Repo capability Artifact consumes. The
// implementation must resolve a full commit SHA and safe result-relative path.
type GitContentReader interface {
	Open(context.Context, string, GitReference) (io.ReadCloser, int64, error)
}

// RepoGitContentReader adapts Repo's fixed-SHA reader without exposing Git or
// managed repository paths to Artifact.
type RepoGitContentReader struct {
	Service *repo.Service
}

func (reader RepoGitContentReader) Open(
	ctx context.Context,
	projectID string,
	reference GitReference,
) (io.ReadCloser, int64, error) {
	if reader.Service == nil ||
		reader.Service.Store == nil ||
		reader.Service.Reads == nil ||
		reference.Workspace != string(repo.WorkspaceResult) {
		return nil, 0, ErrNotAvailable
	}
	repository, err := reader.Service.Store.GetByID(ctx, reference.RepositoryID)
	if err != nil || repository.ProjectID != projectID {
		return nil, 0, ErrNotFound
	}
	content, err := reader.Service.Reads.ReadFile(
		ctx, repository, repo.WorkspaceResult,
		reference.CommitSHA, reference.Path,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("read Repo Artifact content: %w", err)
	}
	if content.Kind != "file" ||
		content.ResolvedRevision != strings.ToLower(reference.CommitSHA) ||
		content.Content == nil ||
		content.Encoding == nil ||
		*content.Encoding != "utf-8" {
		return nil, 0, ErrNotAvailable
	}
	value := []byte(*content.Content)
	if int64(len(value)) != content.Size {
		return nil, 0, errors.New("Repo Artifact content size changed")
	}
	return io.NopCloser(strings.NewReader(*content.Content)), content.Size, nil
}
