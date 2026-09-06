package artifact

import (
	"context"
	"time"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PurgeObject deletes an exact object while its Blob row is transactionally locked.
type PurgeObject func(context.Context, string) error

// TransactionalAuditRecorder appends audit state in a business transaction.
type TransactionalAuditRecorder interface {
	RecordInTransaction(context.Context, transaction.Tx, audit.Event) error
}

// Store is the authoritative Artifact persistence boundary.
type Store interface {
	CreateFolder(context.Context, Folder) (Folder, error)
	EnsureFolderPath(context.Context, string, []string) (Folder, error)
	GetFolderTree(context.Context, string) (FolderTree, error)
	RenameFolder(context.Context, string, string, string, time.Time) (Folder, error)
	MoveFolder(context.Context, string, string, *string, *int, time.Time) (Folder, error)
	DeleteFolder(context.Context, string, string, bool, time.Time) error
	MoveArtifact(context.Context, string, string, *string, time.Time) (Detail, error)
	CreateFirst(context.Context, Artifact, Version, UploadSession) error
	CreateGit(context.Context, Artifact, Version, string) error
	CreateVersion(context.Context, string, string, Version, UploadSession) (UploadSession, error)
	AttachToAgentRun(context.Context, string, string, string, string, time.Time) (ChatAttachment, error)
	FindBlob(context.Context, string, string, int64) (Blob, error)
	GetArtifact(context.Context, string, string) (Artifact, error)
	GetDetail(context.Context, string, string, bool) (Detail, error)
	GetUpload(context.Context, string, string) (UploadSession, error)
	GetUploadByIdempotency(context.Context, string, string) (UploadSession, error)
	GetVersion(context.Context, string, string, string) (Version, error)
	List(context.Context, string, ListFilter) (Page, error)
	ListVersions(context.Context, string, string) (VersionList, error)
	ListAgentRunAttachments(context.Context, string, []string) ([]ChatAttachment, error)
	Update(context.Context, string, string, UpdateInput, time.Time) (Detail, error)
	MarkUploading(context.Context, string, time.Time) error
	BeginConfirm(context.Context, string, time.Time, time.Time) (bool, error)
	SetUploadStatus(context.Context, string, string, string, time.Time) error
	UpsertParts(context.Context, string, []UploadPart) error
	FinalizeUpload(context.Context, UploadSession, Blob, time.Time) (Detail, error)
	Trash(context.Context, string, string, string, time.Time) error
	Restore(context.Context, string, string, string, time.Time) (Detail, error)
	RestoreVersion(context.Context, string, string, string, string, string, string, time.Time) (Detail, error)
	Purge(context.Context, string, string, PurgeObject) error
	ExpireUploads(context.Context, time.Time, int) ([]UploadSession, error)
	MarkProviderAborted(context.Context, string, time.Time) error
	ListPreviews(context.Context, string, string, string) (PreviewList, error)
	GetPreview(context.Context, string, string, string, string) (Preview, error)
	GetPreviewByJob(context.Context, string) (Preview, error)
	CreatePreviewTransfer(
		context.Context,
		PreviewTransfer,
	) (PreviewTransfer, bool, error)
	GetPreviewTransfer(context.Context, string, string) (PreviewTransfer, error)
	MarkPreviewTransferUploaded(context.Context, string, string, time.Time) error
	ExpirePreviewTransfers(context.Context, time.Time, int) ([]PreviewTransfer, error)
	MarkPreviewTransferAborted(context.Context, string, time.Time) error
	CompletePreviewInTransaction(
		context.Context,
		transaction.Tx,
		jobs.Job,
		previewResult,
		time.Time,
	) error
	UpdatePreviewJobInTransaction(
		context.Context,
		transaction.Tx,
		jobs.Job,
		string,
		string,
		time.Time,
	) error
	ReconcilePreviewJobs(context.Context, time.Time) error
}
