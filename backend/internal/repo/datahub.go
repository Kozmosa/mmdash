package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

// DataHubFile is current-code-head metadata; it never carries file contents.
type DataHubFile struct {
	Kind     string
	Mode     string
	ObjectID string
	Path     string
	Size     *int64
}

// DataHubSink is the Data Hub-owned projection boundary used by Repo events.
type DataHubSink interface {
	DeleteStaleRepoFiles(context.Context, string, string) error
	ProjectRepoCommit(
		context.Context, contract.EventEnvelope,
		Repository, Workspace, Commit, bool,
	) error
	ProjectRepository(
		context.Context, contract.EventEnvelope, Repository,
	) error
	UpsertRepoFiles(
		context.Context, Repository, string, []DataHubFile, time.Time,
	) error
}

// DataHubProjector resolves authoritative Git objects after durable events.
type DataHubProjector struct {
	BatchSize    int
	Reader       *Reader
	Repositories Store
	Sink         DataHubSink
}

// DataHubReaderAdapter resolves every projection through authorized Repo APIs.
type DataHubReaderAdapter struct {
	Service *Service
}

func (adapter DataHubReaderAdapter) Repository(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
) (Repository, error) {
	if adapter.Service == nil {
		return Repository{}, ErrNotReady
	}
	return adapter.Service.Get(ctx, caller, projectID)
}

func (adapter DataHubReaderAdapter) Commit(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	metadata map[string]interface{},
) (Commit, error) {
	commitSHA, ok := metadata["commit_sha"].(string)
	if !ok || gitcli.ValidateFullSHA(commitSHA) != nil {
		return Commit{}, ErrInvalid
	}
	return adapter.Service.GetCommit(
		ctx, caller, projectID, commitSHA,
	)
}

func (adapter DataHubReaderAdapter) File(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	metadata map[string]interface{},
) (FileContent, error) {
	commitSHA, shaOK := metadata["commit_sha"].(string)
	repositoryPath, pathOK := metadata["path"].(string)
	if !shaOK || !pathOK ||
		gitcli.ValidateFullSHA(commitSHA) != nil ||
		gitcli.ValidateRepoPath(repositoryPath, false) != nil {
		return FileContent{}, ErrInvalid
	}
	return adapter.Service.ReadFile(
		ctx, caller, projectID, WorkspaceCode,
		commitSHA, repositoryPath,
	)
}

func (projector DataHubProjector) Project(
	ctx context.Context,
	event contract.EventEnvelope,
) error {
	if event.ProjectID == nil ||
		projector.Reader == nil ||
		projector.Repositories == nil ||
		projector.Sink == nil {
		return ErrInvalid
	}
	repositoryID, ok := event.Payload["repository_id"].(string)
	if !ok || repositoryID == "" {
		return ErrInvalid
	}
	repository, err := projector.Repositories.GetByProject(
		ctx, *event.ProjectID,
	)
	if err != nil {
		return err
	}
	if repository.ID != repositoryID {
		return ErrConflict
	}
	switch event.EventType {
	case "repo.connected":
		if err := projector.Sink.ProjectRepository(
			ctx, event, repository,
		); err != nil {
			return err
		}
		for _, workspace := range repository.Workspaces {
			if workspace.HeadCommitSHA == nil {
				return ErrNotReady
			}
			commit, err := projector.Reader.GetCommit(
				ctx, repository, *workspace.HeadCommitSHA,
			)
			if err != nil {
				return err
			}
			if err := projector.Sink.ProjectRepoCommit(
				ctx, event, repository, workspace, commit, false,
			); err != nil {
				return err
			}
		}
		code, err := findWorkspace(repository, WorkspaceCode)
		if err != nil {
			return err
		}
		if code.HeadCommitSHA == nil {
			return ErrNotReady
		}
		return projector.indexCode(
			ctx, event, repository, *code.HeadCommitSHA,
		)
	case "repo.commit.created", "repo.commit.detected":
		workspace, commitSHA, err := projectionCommit(event)
		if err != nil {
			return err
		}
		mapping, err := findWorkspace(repository, workspace)
		if err != nil {
			return err
		}
		branch, ok := event.Payload["branch"].(string)
		if !ok || branch != mapping.RemoteBranch {
			return ErrInvalid
		}
		commit, err := projector.Reader.GetCommit(
			ctx, repository, commitSHA,
		)
		if err != nil {
			return err
		}
		if err := projector.Sink.ProjectRepoCommit(
			ctx, event, repository, mapping, commit, true,
		); err != nil {
			return err
		}
		if workspace != WorkspaceCode ||
			mapping.HeadCommitSHA == nil ||
			*mapping.HeadCommitSHA != commitSHA {
			return nil
		}
		return projector.indexCode(
			ctx, event, repository, commitSHA,
		)
	default:
		return fmt.Errorf("unsupported Repo projection event %q", event.EventType)
	}
}

func (projector DataHubProjector) indexCode(
	ctx context.Context,
	event contract.EventEnvelope,
	repository Repository,
	commitSHA string,
) error {
	if gitcli.ValidateFullSHA(commitSHA) != nil {
		return ErrInvalid
	}
	batchSize := projector.BatchSize
	if batchSize < 1 || batchSize > maxTreeLimit {
		batchSize = maxTreeLimit
	}
	batch := make([]DataHubFile, 0, batchSize)
	directories := []string{""}
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := projector.Sink.UpsertRepoFiles(
			ctx, repository, commitSHA, batch, event.OccurredAt,
		); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	for len(directories) > 0 {
		directory := directories[0]
		directories = directories[1:]
		cursor := ""
		for {
			page, err := projector.Reader.ListTree(
				ctx, repository, WorkspaceCode, commitSHA,
				directory, cursor, batchSize,
			)
			if err != nil {
				return err
			}
			for _, entry := range page.Items {
				if entry.Kind == "directory" {
					directories = append(directories, entry.Path)
					continue
				}
				batch = append(batch, DataHubFile{
					Kind: entry.Kind, Mode: entry.Mode,
					ObjectID: entry.ObjectID, Path: entry.Path,
					Size: entry.Size,
				})
				if len(batch) == batchSize {
					if err := flush(); err != nil {
						return err
					}
				}
			}
			if !page.HasMore || page.NextCursor == nil {
				break
			}
			cursor = *page.NextCursor
		}
	}
	if err := flush(); err != nil {
		return err
	}
	current, err := projector.Repositories.GetByProject(
		ctx, repository.ProjectID,
	)
	if err != nil {
		return err
	}
	code, err := findWorkspace(current, WorkspaceCode)
	if err != nil {
		return err
	}
	if code.HeadCommitSHA == nil || *code.HeadCommitSHA != commitSHA {
		return nil
	}
	return projector.Sink.DeleteStaleRepoFiles(
		ctx, repository.ID, commitSHA,
	)
}

func projectionCommit(
	event contract.EventEnvelope,
) (WorkspaceKind, string, error) {
	rawWorkspace, workspaceOK := event.Payload["workspace"].(string)
	commitSHA, shaOK := event.Payload["commit_sha"].(string)
	workspace := WorkspaceKind(strings.TrimSpace(rawWorkspace))
	if !workspaceOK || !shaOK ||
		(workspace != WorkspaceCode &&
			workspace != WorkspaceArticle &&
			workspace != WorkspaceResult) ||
		gitcli.ValidateFullSHA(commitSHA) != nil {
		return "", "", ErrInvalid
	}
	return workspace, commitSHA, nil
}

var _ interface {
	Project(context.Context, contract.EventEnvelope) error
} = DataHubProjector{}
