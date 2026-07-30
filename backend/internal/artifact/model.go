package artifact

import "time"

const (
	KindProblem          = "problem"
	KindAttachment       = "attachment"
	KindExperimentResult = "experiment_result"
	KindModelFile        = "model_file"
	KindArticleBuild     = "article_build"
	KindOther            = "other"

	SourceUserUpload = "user_upload"
	SourceExperiment = "experiment"
	SourceModel      = "model"
	SourceArticle    = "article"
	SourceSystem     = "system"

	StatusPendingUpload = "pending_upload"
	StatusVerifying     = "verifying"
	StatusAvailable     = "available"
	StatusFailed        = "failed"
	StatusTrashed       = "trashed"

	UploadInitialized = "initialized"
	UploadUploading   = "uploading"
	UploadCompleting  = "completing"
	UploadVerifying   = "verifying"
	UploadCompleted   = "completed"
	UploadAborted     = "aborted"
	UploadExpired     = "expired"
	UploadFailed      = "failed"
)

// Artifact is the stable, project-scoped file identity.
type Artifact struct {
	ID               string     `json:"artifact_id"`
	ProjectID        string     `json:"project_id"`
	Kind             string     `json:"kind"`
	Source           string     `json:"source"`
	SourceObjectID   *string    `json:"source_object_id"`
	Tags             []string   `json:"tags"`
	Name             string     `json:"name"`
	Description      *string    `json:"description"`
	RecommendedUsage []string   `json:"recommended_usage"`
	CurrentVersionID *string    `json:"current_version_id"`
	Status           string     `json:"status"`
	CreatedBy        string     `json:"created_by"`
	TrashedAt        *time.Time `json:"trashed_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// GitReference pins a Repo-backed Artifact to immutable content.
type GitReference struct {
	RepositoryID string `json:"repository_id"`
	Workspace    string `json:"workspace"`
	CommitSHA    string `json:"commit_sha"`
	Path         string `json:"path"`
}

// Version is immutable after it becomes available.
type Version struct {
	ID           string        `json:"version_id"`
	ArtifactID   string        `json:"artifact_id"`
	VersionNo    int           `json:"version_no"`
	StorageClass string        `json:"storage_class"`
	Filename     string        `json:"filename"`
	SHA256       string        `json:"sha256"`
	MIMEType     string        `json:"mime_type"`
	SizeBytes    int64         `json:"size_bytes"`
	Status       string        `json:"status"`
	AvailableAt  *time.Time    `json:"available_at"`
	GitReference *GitReference `json:"git_reference"`
	CreatedBy    string        `json:"created_by"`
	CreatedAt    time.Time     `json:"created_at"`

	BlobID    string `json:"-"`
	ObjectKey string `json:"-"`
	Backend   string `json:"-"`
	ProjectID string `json:"-"`
}

// Detail combines stable metadata with the selected current Version.
type Detail struct {
	Artifact       Artifact `json:"artifact"`
	CurrentVersion *Version `json:"current_version"`
}

// Page is a bounded cursor page of Artifact details.
type Page struct {
	Items      []Detail `json:"items"`
	HasMore    bool     `json:"has_more"`
	NextCursor *string  `json:"next_cursor"`
}

// VersionList contains every retained immutable Version.
type VersionList struct {
	Items []Version `json:"items"`
}

// UploadPart is provider-confirmed multipart progress.
type UploadPart struct {
	PartNumber  int       `json:"part_number"`
	SizeBytes   int64     `json:"size_bytes"`
	ETag        string    `json:"etag"`
	CompletedAt time.Time `json:"completed_at"`
}

// UploadSession is the internal persistence projection. Provider fields must
// never be serialized through public handlers.
type UploadSession struct {
	ID               string
	ProjectID        string
	ArtifactID       string
	VersionID        string
	ProviderUploadID string
	StagingKey       string
	ExpectedSHA256   string
	ExpectedSize     int64
	MIMEType         string
	PartSizeBytes    int64
	PartCount        int
	Status           string
	IdempotencyKey   string
	CreatedBy        string
	ExpiresAt        time.Time
	CompletedAt      *time.Time
	AbortedAt        *time.Time
	ErrorCode        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Filename         string
	VersionNo        int
	ArtifactStatus   string
	Parts            []UploadPart
}

// PublicUploadSession contains no provider upload ID or object key.
type PublicUploadSession struct {
	UploadID       string       `json:"upload_id"`
	ArtifactID     string       `json:"artifact_id"`
	VersionID      string       `json:"version_id"`
	UploadMode     string       `json:"upload_mode"`
	TransferMode   string       `json:"transfer_mode"`
	Status         string       `json:"status"`
	SizeBytes      int64        `json:"size_bytes"`
	SHA256         string       `json:"sha256"`
	PartSizeBytes  int64        `json:"part_size_bytes"`
	PartCount      int          `json:"part_count"`
	CompletedParts []UploadPart `json:"completed_parts"`
	ExpiresAt      time.Time    `json:"expires_at"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

// TransferGrant is a short-lived direct or Core-streamed request.
type TransferGrant struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// PartGrant binds one exact expected part size to a transfer.
type PartGrant struct {
	PartNumber int           `json:"part_number"`
	SizeBytes  int64         `json:"size_bytes"`
	Transfer   TransferGrant `json:"transfer"`
}

// PartGrantList is deliberately bounded to at most 100 items.
type PartGrantList struct {
	Items []PartGrant `json:"items"`
}

// DownloadGrant describes an authorized immutable Version transfer.
type DownloadGrant struct {
	ArtifactID string        `json:"artifact_id"`
	VersionID  string        `json:"version_id"`
	Filename   string        `json:"filename"`
	MIMEType   string        `json:"mime_type"`
	SizeBytes  int64         `json:"size_bytes"`
	Transfer   TransferGrant `json:"transfer"`
}

// InitializeUploadInput creates the stable Artifact and first Version.
type InitializeUploadInput struct {
	Filename       string
	Name           string
	SizeBytes      int64
	SHA256         string
	MIMEType       string
	Kind           string
	Tags           []string
	Description    *string
	IdempotencyKey string
}

// InitializeVersionInput creates another immutable Version.
type InitializeVersionInput struct {
	Filename       string
	SizeBytes      int64
	SHA256         string
	MIMEType       string
	IdempotencyKey string
}

// RegisterGitInput is the Core-internal registration contract for small,
// immutable result-branch files. It is intentionally not an HTTP request.
type RegisterGitInput struct {
	Name           string
	Filename       string
	MIMEType       string
	Kind           string
	Source         string
	SourceObjectID string
	Tags           []string
	Description    *string
	GitReference   GitReference
}

// UpdateInput contains only editable Artifact display metadata.
type UpdateInput struct {
	Name        *string
	Kind        *string
	Tags        *[]string
	Description **string
}

// ListFilter selects a stable, low-cardinality Artifact subset.
type ListFilter struct {
	Cursor string
	Kind   string
	Limit  int
	Source string
	Status string
	Tag    string
	Trash  bool
}

// ConfirmPart is one client-observed provider ETag.
type ConfirmPart struct {
	PartNumber int
	ETag       string
}

// Blob is one project-local, content-addressed object.
type Blob struct {
	ID             string
	ProjectID      string
	SHA256         string
	SizeBytes      int64
	Backend        string
	ObjectKey      string
	ReferenceCount int64
}
