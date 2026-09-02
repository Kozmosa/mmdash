package article

import (
	"context"
	"io"
	"time"

	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

type Store interface {
	GetDraft(context.Context, string) (Draft, error)
	PersistDraft(context.Context, string, string, PersistDraftInput, string, []Block, map[string]interface{}, string) (Draft, error)
	ReviewBlock(context.Context, string, string, string) (Block, error)
	CreateChapterTag(context.Context, ChapterTag) (ChapterTag, bool, error)
	GetChapterTag(context.Context, string, string) (ChapterTag, error)
	ListChapterTags(context.Context, string) ([]ChapterTag, error)
	UpdateChapterTag(context.Context, string, string, string, string) (ChapterTag, error)
	DeleteChapterTag(context.Context, string, string, string) error
	ReviewChapterTag(context.Context, string, string, string) (ChapterTag, error)
	CreatePatch(context.Context, Patch) (Patch, error)
	ListPatches(context.Context, string, string) ([]Patch, error)
	ReviewPatch(context.Context, string, string, string, string, *int64) (Patch, error)
	AcceptPatch(context.Context, string, string, string, PersistDraftInput, string, []Block, map[string]interface{}, string) (Patch, error)
	CreateReference(context.Context, Reference) (Reference, bool, error)
	ListReferences(context.Context, string) ([]Reference, error)
	DeleteReference(context.Context, string, string, string) error
	CreateCommit(context.Context, Commit) (Commit, bool, error)
	CreateCommitOperation(context.Context, CommitOperation) (CommitOperation, bool, error)
	ClaimCommitOperations(context.Context, string, time.Time, time.Duration, int) ([]CommitOperation, error)
	BindCommitOperation(context.Context, CommitOperation, Commit, time.Time) (Commit, error)
	CompleteCommitOperation(context.Context, CommitOperation, time.Time) error
	FailCommitOperation(context.Context, CommitOperation, string, bool, time.Time, time.Time) error
	GetCommitOperation(context.Context, string, string) (CommitOperation, error)
	RenewCommitOperationLease(context.Context, string, string, time.Time) error
	GetCommit(context.Context, string, string) (Commit, error)
	ListCommits(context.Context, string) ([]Commit, error)
	CreateTemplate(context.Context, Template) (Template, bool, error)
	GetTemplate(context.Context, string, string) (Template, error)
	ListTemplates(context.Context, string) ([]Template, error)
	CreateBuild(context.Context, Build, jobs.CreateInput, jobs.TransactionalWriter) (Build, bool, error)
	GetBuild(context.Context, string, string) (Build, error)
	GetBuildByJob(context.Context, transaction.Tx, string) (Build, error)
	ListBuilds(context.Context, string, string) ([]Build, error)
	MarkBuildRunning(context.Context, transaction.Tx, string) error
	UpdateBuildProgress(context.Context, string, int, string) (Build, error)
	CompleteBuild(context.Context, transaction.Tx, string, map[string]interface{}) (Build, error)
	FailBuild(context.Context, transaction.Tx, string, string, string) (Build, error)
	AddBuildOutput(context.Context, string, BuildOutput) error
	CreateRelease(context.Context, Release) (Release, bool, error)
	GetRelease(context.Context, string, string) (Release, error)
	ListReleases(context.Context, string) ([]Release, error)
	CreatePublication(context.Context, Publication) (Publication, bool, error)
	CreatePublicationBuild(context.Context, Publication, Build, jobs.CreateInput, jobs.TransactionalWriter) (Publication, bool, error)
	GetPublicationByBuild(context.Context, transaction.Tx, string) (Publication, error)
	GetPublication(context.Context, string, string) (Publication, error)
	RetryPublicationBuild(context.Context, Publication, string, Build, jobs.CreateInput, jobs.TransactionalWriter) (Publication, error)
	CompletePublication(context.Context, transaction.Tx, string, string) error
	FailPublication(context.Context, transaction.Tx, string, string) error
	UpsertZoteroBinding(context.Context, ZoteroBinding, string) (ZoteroBinding, error)
	GetZoteroBinding(context.Context, string) (ZoteroBinding, error)
	DeleteZoteroBinding(context.Context, string) error
}

type ArtifactAccess interface {
	ArticleTemplateGrant(context.Context, string, string, string) (map[string]interface{}, error)
	ArticleResourceGrant(context.Context, string, string, string) (map[string]interface{}, error)
	ArchiveArticleTemplate(context.Context, string, string, string, string, string, int64, io.Reader) (string, string, error)
	ArchiveArticleBuildOutput(context.Context, string, string, string, []string, string, string, string, string, int64, io.Reader) (string, string, error)
}
