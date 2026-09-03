// Package article owns collaborative Markdown authoring, immutable Git commits,
// deterministic document builds, and human-reviewed releases.
package article

import (
	"errors"
	"time"
)

var (
	ErrConflict    = errors.New("article state conflict")
	ErrForbidden   = errors.New("article access forbidden")
	ErrInvalid     = errors.New("invalid article request")
	ErrNotFound    = errors.New("article object not found")
	ErrNotReady    = errors.New("article object is not ready")
	ErrSuperseded  = errors.New("article preview was superseded")
	ErrUnavailable = errors.New("article integration unavailable")
)

const (
	BuildFormal           = "formal"
	BuildPreview          = "preview"
	BuildTemplateTest     = "template_test"
	BuildQueued           = "queued"
	BuildRunning          = "running"
	BuildSucceeded        = "succeeded"
	BuildFailed           = "failed"
	BuildSuperseded       = "superseded"
	ChapterTagUnedited    = "unedited"
	ChapterTagUnreviewed  = "unreviewed"
	ChapterTagReviewed    = "reviewed"
	ChapterTagNeedsReview = "needs_review"
)

type Block struct {
	Attrs      map[string]interface{} `json:"attrs"`
	BlockID    string                 `json:"block_id"`
	NodeType   string                 `json:"node_type"`
	Ordinal    int                    `json:"ordinal"`
	Provenance map[string]interface{} `json:"provenance"`
	Tag        string                 `json:"tag"`
	Text       string                 `json:"text"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

// ChapterTag is independent from the tag carried by an Article block. It is
// anchored to the stable heading block identity and keeps its own review
// provenance so heading edits cannot silently inherit a review decision.
type ChapterTag struct {
	ChapterTagID       string     `json:"chapter_tag_id"`
	HeadingBlockID     string     `json:"heading_block_id"`
	HeadingBlockType   string     `json:"heading_block_type"`
	HeadingFingerprint string     `json:"heading_fingerprint"`
	ProjectID          string     `json:"project_id"`
	ReviewedAt         *time.Time `json:"reviewed_at,omitempty"`
	ReviewedBy         *string    `json:"reviewed_by,omitempty"`
	StaleReason        string     `json:"stale_reason,omitempty"`
	Status             string     `json:"status"`
	UpdatedAt          time.Time  `json:"updated_at"`
	UpdatedBy          string     `json:"updated_by"`
}

type Draft struct {
	Blocks        []Block                `json:"blocks"`
	DraftRevision int64                  `json:"draft_revision"`
	Markdown      string                 `json:"markdown"`
	ProjectID     string                 `json:"project_id"`
	StateVector   string                 `json:"state_vector"`
	SyncStatus    string                 `json:"sync_status"`
	TiptapJSON    map[string]interface{} `json:"tiptap_json"`
	UpdatedAt     time.Time              `json:"updated_at"`
	YjsUpdate     string                 `json:"yjs_update"`
	ReferencesBIB string                 `json:"-"`
	Manifest      map[string]interface{} `json:"-"`
	ActorKind     string                 `json:"-"`
	Provenance    map[string]interface{} `json:"-"`
}

type PersistDraftInput struct {
	ActorKind        string
	ExpectedRevision int64
	Provenance       map[string]interface{}
	StateVector      string
	TiptapJSON       map[string]interface{}
	YjsUpdate        string
}

type Patch struct {
	AcceptedRevision *int64                 `json:"accepted_revision,omitempty"`
	BaseRevision     int64                  `json:"base_revision"`
	CreatedAt        time.Time              `json:"created_at"`
	CreatedBy        string                 `json:"created_by"`
	Patch            map[string]interface{} `json:"patch"`
	PatchID          string                 `json:"patch_id"`
	ProjectID        string                 `json:"project_id"`
	Provenance       map[string]interface{} `json:"provenance"`
	Rationale        string                 `json:"rationale"`
	ReviewedBy       string                 `json:"reviewed_by,omitempty"`
	Status           string                 `json:"status"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

type Reference struct {
	CitationKey     string                 `json:"citation_key,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	CreatedBy       string                 `json:"created_by"`
	Metadata        map[string]interface{} `json:"metadata"`
	ProjectID       string                 `json:"project_id"`
	ReferenceID     string                 `json:"reference_id"`
	ReferenceType   string                 `json:"reference_type"`
	SourceObjectID  string                 `json:"source_object_id"`
	SourceVersionID string                 `json:"source_version_id"`
	Title           string                 `json:"title"`
}

type Commit struct {
	CommitID          string                 `json:"commit_id"`
	CommitSHA         string                 `json:"commit_sha"`
	CreatedAt         time.Time              `json:"created_at"`
	CreatedBy         string                 `json:"created_by"`
	DraftRevision     int64                  `json:"draft_revision"`
	ManuscriptSHA256  string                 `json:"manuscript_sha256"`
	Message           string                 `json:"message"`
	ProjectID         string                 `json:"project_id"`
	StateVector       string                 `json:"state_vector"`
	TiptapJSON        map[string]interface{} `json:"-"`
	YjsUpdate         string                 `json:"-"`
	PreviousCommitSHA string                 `json:"-"`
	ReferencesSHA256  string                 `json:"-"`
	ManifestSHA256    string                 `json:"-"`
	FrozenReferences  []Reference            `json:"-"`
}

type CommitOperation struct {
	Attempts       int        `json:"attempts"`
	CommitID       string     `json:"commit_id,omitempty"`
	CommitSHA      string     `json:"commit_sha,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	DraftRevision  int64      `json:"draft_revision"`
	ErrorCode      string     `json:"error_code,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	IdempotencyKey string     `json:"-"`
	MaxAttempts    int        `json:"max_attempts"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	OperationKind  string     `json:"operation_kind"`
	OperationID    string     `json:"operation_id"`
	ProjectID      string     `json:"project_id"`
	PublicationID  string     `json:"publication_id,omitempty"`
	Stage          string     `json:"stage"`
	Status         string     `json:"status"`
	UpdatedAt      time.Time  `json:"updated_at"`

	BibliographyTool  string                 `json:"-"`
	CreatedBy         string                 `json:"-"`
	Engine            string                 `json:"-"`
	ExpectedHeadSHA   string                 `json:"-"`
	FrozenReferences  []Reference            `json:"-"`
	LeaseExpiresAt    *time.Time             `json:"-"`
	LockedBy          string                 `json:"-"`
	ManifestBytes     []byte                 `json:"-"`
	ManifestSHA256    string                 `json:"-"`
	Manuscript        string                 `json:"-"`
	ManuscriptSHA256  string                 `json:"-"`
	Message           string                 `json:"-"`
	Notes             string                 `json:"-"`
	PreviousCommitSHA string                 `json:"-"`
	PublicationKey    string                 `json:"-"`
	ReferencesBIB     string                 `json:"-"`
	ReferencesSHA256  string                 `json:"-"`
	RequestSHA256     string                 `json:"-"`
	StateVector       string                 `json:"-"`
	Tag               string                 `json:"-"`
	TemplateID        string                 `json:"-"`
	TiptapJSON        map[string]interface{} `json:"-"`
	Title             string                 `json:"-"`
	YjsUpdate         string                 `json:"-"`
}

type TemplateManifest struct {
	BibliographyTarget string `json:"bibliography_target"`
	BibliographyTool   string `json:"bibliography_tool"`
	ContentTarget      string `json:"content_target"`
	Engine             string `json:"engine"`
	Entrypoint         string `json:"entrypoint"`
	Name               string `json:"name"`
	Output             string `json:"output"`
	SchemaVersion      string `json:"schema_version"`
	Version            string `json:"version"`
}

type Template struct {
	ArtifactID  string           `json:"artifact_id"`
	CreatedAt   time.Time        `json:"created_at"`
	CreatedBy   string           `json:"created_by"`
	ErrorCode   string           `json:"error_code,omitempty"`
	Manifest    TemplateManifest `json:"manifest"`
	ProjectID   string           `json:"project_id"`
	Status      string           `json:"status"`
	TemplateID  string           `json:"template_id"`
	UpdatedAt   time.Time        `json:"updated_at"`
	VersionID   string           `json:"version_id"`
	TestBuildID string           `json:"-"`
}

type BuildOutput struct {
	ArtifactID string `json:"artifact_id"`
	Filename   string `json:"filename"`
	MIMEType   string `json:"mime_type"`
	Role       string `json:"role"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"size_bytes"`
	VersionID  string `json:"version_id"`
}

type Build struct {
	BibliographyTool   string                 `json:"bibliography_tool"`
	BuildID            string                 `json:"build_id"`
	BuildKind          string                 `json:"build_kind"`
	CommitID           string                 `json:"commit_id,omitempty"`
	CommitSHA          string                 `json:"commit_sha,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
	CreatedBy          string                 `json:"created_by"`
	DraftRevision      *int64                 `json:"draft_revision,omitempty"`
	Engine             string                 `json:"engine"`
	ErrorCode          string                 `json:"error_code,omitempty"`
	ErrorMessage       string                 `json:"error_message,omitempty"`
	FinishedAt         *time.Time             `json:"finished_at,omitempty"`
	JobID              string                 `json:"job_id,omitempty"`
	Outputs            []BuildOutput          `json:"outputs"`
	ProgressPercent    int                    `json:"progress_percent"`
	ProgressStage      string                 `json:"progress_stage"`
	ProjectID          string                 `json:"project_id"`
	Status             string                 `json:"status"`
	TemplateArtifactID string                 `json:"template_artifact_id"`
	TemplateID         string                 `json:"template_id"`
	TemplateVersionID  string                 `json:"template_version_id"`
	Toolchain          map[string]interface{} `json:"toolchain"`
	UpdatedAt          time.Time              `json:"updated_at"`
	IdempotencyKey     string                 `json:"-"`
}

type Release struct {
	BuildID           string                 `json:"build_id"`
	CommitID          string                 `json:"commit_id"`
	CommitSHA         string                 `json:"commit_sha"`
	CreatedAt         time.Time              `json:"created_at"`
	CreatedBy         string                 `json:"created_by"`
	Engine            string                 `json:"engine"`
	Notes             string                 `json:"notes"`
	Outputs           []BuildOutput          `json:"outputs"`
	ProjectID         string                 `json:"project_id"`
	ReleaseID         string                 `json:"release_id"`
	Tag               string                 `json:"tag"`
	TemplateVersionID string                 `json:"template_version_id"`
	Title             string                 `json:"title"`
	Toolchain         map[string]interface{} `json:"toolchain"`
}

type Publication struct {
	BuildID        string    `json:"build_id"`
	CommitID       string    `json:"commit_id"`
	CreatedAt      time.Time `json:"created_at"`
	CreatedBy      string    `json:"created_by"`
	ErrorCode      string    `json:"error_code,omitempty"`
	Notes          string    `json:"notes"`
	ProjectID      string    `json:"project_id"`
	PublicationID  string    `json:"publication_id"`
	ReleaseID      string    `json:"release_id,omitempty"`
	Status         string    `json:"status"`
	Tag            string    `json:"tag"`
	Title          string    `json:"title"`
	UpdatedAt      time.Time `json:"updated_at"`
	IdempotencyKey string    `json:"-"`
}

type ZoteroBinding struct {
	APIKeyConfigured bool   `json:"api_key_configured"`
	CollectionKey    string `json:"collection_key,omitempty"`
	LibraryID        string `json:"library_id"`
	LibraryType      string `json:"library_type"`
	ProjectID        string `json:"project_id"`
	ReadOnly         bool   `json:"read_only"`
}

type ZoteroCollection struct {
	CollectionKey       string  `json:"collection_key"`
	Name                string  `json:"name"`
	NumCollections      int     `json:"num_collections"`
	NumItems            int     `json:"num_items"`
	ParentCollectionKey *string `json:"parent_collection_key"`
}

type ZoteroItem struct {
	Authors     []string               `json:"authors"`
	CitationKey string                 `json:"citation_key"`
	DOI         string                 `json:"doi,omitempty"`
	ItemKey     string                 `json:"item_key"`
	ItemType    string                 `json:"item_type"`
	Raw         map[string]interface{} `json:"raw"`
	Title       string                 `json:"title"`
	Version     int64                  `json:"version"`
	Year        string                 `json:"year,omitempty"`
}

type Aggregate struct {
	Builds            []Build            `json:"builds"`
	ChapterTags       []ChapterTag       `json:"chapter_tags"`
	Commits           []Commit           `json:"commits"`
	CommitOperations  []CommitOperation  `json:"commit_operations"`
	Draft             Draft              `json:"draft"`
	References        []Reference        `json:"references"`
	Releases          []Release          `json:"releases"`
	SectionCompletion float64            `json:"section_completion"`
	Templates         []Template         `json:"templates"`
	UnreviewedBlocks  int                `json:"unreviewed_blocks"`
	Warnings          []AggregateWarning `json:"warnings"`
}

// AggregateWarning keeps the usable draft available when a secondary Article
// projection is temporarily unavailable. It exposes stable product metadata,
// never an internal database or adapter error.
type AggregateWarning struct {
	Code      string `json:"code"`
	Component string `json:"component"`
	Message   string `json:"message"`
}

type CommitDetail struct {
	Builds   []Build   `json:"builds"`
	Commit   Commit    `json:"commit"`
	Releases []Release `json:"releases"`
}

type Page[T any] struct {
	Items []T `json:"items"`
}

type BuildJobInput struct {
	ArticleManifest  map[string]interface{}   `json:"article_manifest"`
	BibliographyTool string                   `json:"bibliography_tool"`
	BuildID          string                   `json:"build_id"`
	BuildKind        string                   `json:"build_kind"`
	Engine           string                   `json:"engine"`
	Limits           map[string]interface{}   `json:"limits"`
	Manuscript       string                   `json:"manuscript"`
	ProjectID        string                   `json:"project_id"`
	ReferencesBIB    string                   `json:"references_bib"`
	Resources        []map[string]interface{} `json:"resources,omitempty"`
	Template         map[string]interface{}   `json:"template"`
	Toolchain        map[string]interface{}   `json:"toolchain"`
}
