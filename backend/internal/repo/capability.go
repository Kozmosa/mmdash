package repo

import "context"

// ArticleWorkspace is the narrow future-facing boundary owned by Repo.
type ArticleWorkspace interface {
	ResolveHead(context.Context, string) (Revision, error)
	ListTree(context.Context, string, string, string) ([]TreeEntry, error)
	ReadFile(context.Context, string, string, string) (FileContent, error)
	Commit(context.Context, WorkspaceCommitRequest) (CommitResult, error)
}

// ArticleWorkspaceService adapts Repo without importing future Article domain code.
type ArticleWorkspaceService struct {
	Reader       *Reader
	Repositories Store
	Service      *Service
}

func (workspace ArticleWorkspaceService) ResolveHead(
	ctx context.Context,
	projectID string,
) (Revision, error) {
	repository, err := workspace.Repositories.GetByProject(ctx, projectID)
	if err != nil {
		return Revision{}, err
	}
	article, err := findWorkspace(repository, WorkspaceArticle)
	if err != nil {
		return Revision{}, err
	}
	if article.HeadCommitSHA == nil {
		return Revision{}, ErrNotReady
	}
	return Revision{
		Branch: article.RemoteBranch, CommitSHA: *article.HeadCommitSHA,
		RepositoryID: repository.ID, Workspace: WorkspaceArticle,
	}, nil
}

func (workspace ArticleWorkspaceService) ListTree(
	ctx context.Context,
	projectID string,
	commitSHA string,
	repositoryPath string,
) ([]TreeEntry, error) {
	repository, err := workspace.Repositories.GetByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	items := []TreeEntry{}
	cursor := ""
	for {
		page, err := workspace.Reader.ListTree(
			ctx, repository, WorkspaceArticle, commitSHA,
			repositoryPath, cursor, maxTreeLimit,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if !page.HasMore || page.NextCursor == nil {
			return items, nil
		}
		cursor = *page.NextCursor
	}
}

func (workspace ArticleWorkspaceService) ReadFile(
	ctx context.Context,
	projectID string,
	commitSHA string,
	repositoryPath string,
) (FileContent, error) {
	repository, err := workspace.Repositories.GetByProject(ctx, projectID)
	if err != nil {
		return FileContent{}, err
	}
	return workspace.Reader.ReadFile(
		ctx, repository, WorkspaceArticle, commitSHA, repositoryPath,
	)
}

func (workspace ArticleWorkspaceService) Commit(
	ctx context.Context,
	request WorkspaceCommitRequest,
) (CommitResult, error) {
	request.Workspace = WorkspaceArticle
	if err := workspace.Service.validateCommitRequest(&request); err != nil {
		return CommitResult{}, err
	}
	return workspace.Service.commitTrusted(ctx, request)
}

var _ ArticleWorkspace = ArticleWorkspaceService{}
