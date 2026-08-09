// Package model owns the single Notion source, per-question model history,
// immutable Snapshots, and synchronization workflow.
package model

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

const (
	SettingTypeNotion = "model.notion"

	JobTypeDiscover = "model.notion.discover"
	JobTypeSnapshot = "model.notion.snapshot"

	SyncIdle      = "idle"
	SyncQueued    = "queued"
	SyncRunning   = "running"
	SyncSucceeded = "succeeded"
	SyncUnchanged = "unchanged"
	SyncFailed    = "failed"
	SyncCancelled = "cancelled"
	SyncTimedOut  = "timed_out"

	SyncScopeSource   = "source"
	SyncScopeQuestion = "question"

	SyncTriggerManual    = "manual"
	SyncTriggerScheduled = "scheduled"
	SyncTriggerSettings  = "settings"

	defaultSyncInterval = 5 * time.Minute
	minimumSyncInterval = time.Minute
	maximumSyncInterval = 24 * time.Hour
)

var (
	ErrConflict           = errors.New("model conflict")
	ErrInvalid            = errors.New("invalid model request")
	ErrNotConfigured      = errors.New("model source is not configured")
	ErrNotFound           = errors.New("model record not found")
	ErrNotionUnauthorized = errors.New("Notion OAuth access token is unauthorized")
	ErrOAuthUnavailable   = errors.New("Notion OAuth is not configured")
	ErrPageUndiscovered   = errors.New("Notion page is not a discovered descendant")
	ErrSyncUnavailable    = errors.New("model synchronization is unavailable")

	questionCodePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)
	hexPageIDPattern    = regexp.MustCompile(`(?i)([0-9a-f]{32})`)
	uuidPageIDPattern   = regexp.MustCompile(`(?i)([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)
	sha256Pattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Source struct {
	ID                      string     `json:"source_id"`
	ProjectID               string     `json:"project_id"`
	NotionRootPageID        string     `json:"notion_root_page_id"`
	NotionRootPageURL       string     `json:"notion_root_page_url"`
	NotionRootTitle         string     `json:"notion_root_title"`
	AutoSyncEnabled         bool       `json:"auto_sync_enabled"`
	AutoSyncIntervalSeconds int        `json:"auto_sync_interval_seconds"`
	NextSyncAt              *time.Time `json:"next_sync_at,omitempty"`
	CountdownSeconds        *int       `json:"countdown_seconds,omitempty"`
	LastSyncID              string     `json:"last_sync_id,omitempty"`
	LastSyncStatus          string     `json:"last_sync_status,omitempty"`
	LastSyncedAt            *time.Time `json:"last_synced_at,omitempty"`
	LastErrorCode           string     `json:"last_error_code,omitempty"`
	LastErrorMessage        string     `json:"last_error_message,omitempty"`
	SyncStatus              string     `json:"sync_status"`
	DiscoveredPageCount     int        `json:"discovered_page_count"`
	CreatedBy               string     `json:"-"`
	UpdatedBy               string     `json:"-"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type SourcePage struct {
	NotionPageID    string    `json:"notion_page_id"`
	ParentPageID    string    `json:"parent_page_id,omitempty"`
	Title           string    `json:"title"`
	URL             string    `json:"url"`
	Depth           int       `json:"depth"`
	HasChildren     bool      `json:"has_children"`
	BoundQuestionID string    `json:"bound_question_id,omitempty"`
	LastSeenAt      time.Time `json:"last_seen_at"`
}

type Question struct {
	ID               string     `json:"question_id"`
	ProjectID        string     `json:"project_id"`
	SourceID         string     `json:"-"`
	Code             string     `json:"code"`
	Title            string     `json:"title"`
	NotionPageID     string     `json:"notion_page_id"`
	NotionPageURL    string     `json:"notion_page_url"`
	Position         int        `json:"position"`
	LatestSnapshotID string     `json:"latest_snapshot_id,omitempty"`
	SnapshotCount    int        `json:"snapshot_count"`
	SyncStatus       string     `json:"sync_status"`
	LastSyncID       string     `json:"last_sync_id,omitempty"`
	LastSyncedAt     *time.Time `json:"last_synced_at,omitempty"`
	LastErrorCode    string     `json:"last_error_code,omitempty"`
	LastErrorMessage string     `json:"last_error_message,omitempty"`
	CreatedBy        string     `json:"created_by"`
	UpdatedBy        string     `json:"updated_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type RichText struct {
	Text          string `json:"text"`
	Expression    string `json:"expression,omitempty"`
	Bold          bool   `json:"bold,omitempty"`
	Italic        bool   `json:"italic,omitempty"`
	Strikethrough bool   `json:"strikethrough,omitempty"`
	Underline     bool   `json:"underline,omitempty"`
	Code          bool   `json:"code,omitempty"`
	Color         string `json:"color,omitempty"`
	Href          string `json:"href,omitempty"`
}

type Block struct {
	ID                string       `json:"block_id"`
	Type              string       `json:"type"`
	Text              string       `json:"text"`
	Level             int          `json:"level,omitempty"`
	RichText          []RichText   `json:"rich_text"`
	Language          string       `json:"language,omitempty"`
	Expression        string       `json:"expression,omitempty"`
	Checked           *bool        `json:"checked,omitempty"`
	Rows              [][]string   `json:"rows,omitempty"`
	Cells             [][]RichText `json:"cells,omitempty"`
	URL               string       `json:"url,omitempty"`
	ArtifactID        string       `json:"artifact_id,omitempty"`
	ArtifactVersionID string       `json:"artifact_version_id,omitempty"`
	Caption           string       `json:"caption,omitempty"`
	Children          []Block      `json:"children"`
}

type OutlineItem struct {
	BlockID string `json:"block_id"`
	Title   string `json:"title"`
	Level   int    `json:"level"`
}

type SnapshotAsset struct {
	SourceBlockID     string `json:"source_block_id"`
	ArtifactID        string `json:"artifact_id"`
	ArtifactVersionID string `json:"artifact_version_id"`
	Filename          string `json:"filename"`
	MIMEType          string `json:"mime_type"`
}

type SnapshotSummary struct {
	ID                 string    `json:"snapshot_id"`
	QuestionID         string    `json:"question_id"`
	PreviousSnapshotID string    `json:"previous_snapshot_id,omitempty"`
	Title              string    `json:"title"`
	ContentHash        string    `json:"content_hash"`
	Summary            string    `json:"summary"`
	Tags               []string  `json:"tags"`
	VersionNote        string    `json:"version_note,omitempty"`
	CapturedAt         time.Time `json:"captured_at"`
	TriggeredBy        string    `json:"triggered_by"`
	CreatedAt          time.Time `json:"created_at"`
	MetadataUpdatedAt  time.Time `json:"metadata_updated_at"`
}

type Snapshot struct {
	SnapshotSummary
	ProjectID       string          `json:"project_id"`
	NotionPageID    string          `json:"notion_page_id"`
	NotionPageURL   string          `json:"notion_page_url"`
	Outline         []OutlineItem   `json:"outline"`
	Blocks          []Block         `json:"blocks"`
	ContentMarkdown string          `json:"content_markdown"`
	ContentText     string          `json:"content_text"`
	Assets          []SnapshotAsset `json:"assets"`
}

type Sync struct {
	ID                string     `json:"sync_id"`
	ProjectID         string     `json:"project_id"`
	SourceID          string     `json:"-"`
	QuestionID        string     `json:"question_id,omitempty"`
	Scope             string     `json:"scope"`
	Trigger           string     `json:"trigger"`
	Status            string     `json:"status"`
	JobID             string     `json:"job_id"`
	RequestedBy       string     `json:"requested_by"`
	RequestedAt       time.Time  `json:"requested_at"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	CreatedSnapshotID string     `json:"created_snapshot_id,omitempty"`
	ErrorCode         string     `json:"error_code,omitempty"`
	ErrorMessage      string     `json:"error_message,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Overview struct {
	ProjectID       string       `json:"project_id"`
	GeneratedAt     time.Time    `json:"generated_at"`
	Configured      bool         `json:"configured"`
	Source          *Source      `json:"source,omitempty"`
	DiscoveredPages []SourcePage `json:"discovered_pages"`
	Questions       []Question   `json:"questions"`
}

type NotionOAuthConnection struct {
	Available     bool   `json:"available"`
	Connected     bool   `json:"connected"`
	BotID         string `json:"bot_id,omitempty"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
	WorkspaceIcon string `json:"workspace_icon,omitempty"`
}

type StartNotionOAuthInput struct {
	RootPageURL     string
	AutoSyncEnabled bool
	Interval        time.Duration
}

type NotionOAuthAuthorizationResult struct {
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type CompleteNotionOAuthInput struct {
	Code          string
	ProviderError string
	State         string
}

type NotionOAuthCallbackResult struct {
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
}

type NotionOAuthAuthorization struct {
	ID                      string
	StateHash               string
	ProjectID               string
	UserID                  string
	RootPageID              string
	RootPageURL             string
	AutoSyncEnabled         bool
	AutoSyncIntervalSeconds int
	Status                  string
	ExpiresAt               time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type NotionOAuthTokens struct {
	AccessToken   string
	RefreshToken  string
	BotID         string
	WorkspaceID   string
	WorkspaceName string
	WorkspaceIcon string
}

type QuestionDetail struct {
	Question       Question          `json:"question"`
	LatestSnapshot *Snapshot         `json:"latest_snapshot,omitempty"`
	Snapshots      []SnapshotSummary `json:"snapshots"`
}

type CreateQuestionInput struct {
	Code         string
	Title        string
	NotionPageID string
	Position     int
}

type UpdateQuestionInput struct {
	Code         *string
	Title        *string
	NotionPageID *string
	Position     *int
}

type UpdateSnapshotInput struct {
	Tags        *[]string
	VersionNote *string
}

type SourceConfig struct {
	ProjectID       string
	RootPageID      string
	RootPageURL     string
	AutoSyncEnabled bool
	Interval        time.Duration
	ActorID         string
	SettingsVersion int64
}

type DiscoverResult struct {
	Mode      string               `json:"mode"`
	SyncID    string               `json:"sync_id"`
	RootTitle string               `json:"root_title"`
	Pages     []DiscoverResultPage `json:"pages"`
}

type DiscoverResultPage struct {
	PageID       string  `json:"page_id"`
	ParentPageID *string `json:"parent_page_id"`
	Title        string  `json:"title"`
	URL          string  `json:"url"`
	Depth        int     `json:"depth"`
	HasChildren  bool    `json:"has_children"`
}

type SnapshotResult struct {
	Mode            string        `json:"mode"`
	SyncID          string        `json:"sync_id"`
	QuestionID      string        `json:"question_id"`
	Title           string        `json:"title"`
	ContentHash     string        `json:"content_hash"`
	Summary         string        `json:"summary"`
	Outline         []OutlineItem `json:"outline"`
	Blocks          []Block       `json:"blocks"`
	ContentMarkdown string        `json:"content_markdown"`
	ContentText     string        `json:"content_text"`
	Media           []MediaResult `json:"media"`
}

type MediaResult struct {
	SourceBlockID     string `json:"source_block_id"`
	URL               string `json:"url,omitempty"`
	Filename          string `json:"filename"`
	MIMEType          string `json:"mime_type"`
	ArtifactID        string `json:"artifact_id,omitempty"`
	ArtifactVersionID string `json:"artifact_version_id,omitempty"`
}

type Store interface {
	GetSource(context.Context, string) (Source, error)
	UpsertSource(context.Context, SourceConfig, string, time.Time) (Source, error)
	DisableSource(context.Context, string, string, time.Time) error
	ListSourcePages(context.Context, string) ([]SourcePage, error)
	ListQuestions(context.Context, string) ([]Question, error)
	GetQuestion(context.Context, string, string) (Question, error)
	CreateQuestion(context.Context, Question) (Question, error)
	UpdateQuestion(context.Context, string, string, UpdateQuestionInput, string, time.Time) (Question, error)
	ArchiveQuestion(context.Context, string, string, string, time.Time) error
	ListSnapshotSummaries(context.Context, string, string) ([]SnapshotSummary, error)
	GetSnapshot(context.Context, string, string, string) (Snapshot, error)
	UpdateSnapshot(context.Context, string, string, string, UpdateSnapshotInput, string, time.Time) (Snapshot, error)
	CreateSync(context.Context, Sync, jobs.CreateInput, *time.Time) (Sync, error)
	CreateSyncInTransaction(context.Context, transaction.Tx, Sync, jobs.CreateInput, *time.Time) (Sync, error)
	GetSyncByJob(context.Context, string) (Sync, error)
	LatestSnapshotHash(context.Context, string) (string, error)
	ClaimSyncInTransaction(context.Context, transaction.Tx, jobs.Job, time.Time) error
	CompleteDiscoverInTransaction(context.Context, transaction.Tx, jobs.Job, DiscoverResult, time.Time) (Sync, error)
	ListDiscoveredQuestionsInTransaction(context.Context, transaction.Tx, string) ([]Question, error)
	CompleteSnapshotInTransaction(context.Context, transaction.Tx, jobs.Job, SnapshotResult, time.Time) error
	FailSyncInTransaction(context.Context, transaction.Tx, jobs.Job, jobs.Failure, time.Time) error
	ClaimDueSources(context.Context, string, time.Time, time.Duration, int) ([]Source, error)
	AdvanceSchedule(context.Context, string, string, time.Time, time.Time) error
	CreateNotionOAuthAuthorization(context.Context, NotionOAuthAuthorization) error
	ClaimNotionOAuthAuthorization(context.Context, string, string, time.Time) (NotionOAuthAuthorization, error)
	CompleteNotionOAuthAuthorization(context.Context, string, string, time.Time) error
}

type Access interface {
	Authenticate(context.Context, string) (auth.Identity, error)
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

type AuditRecorder interface {
	Record(context.Context, audit.Event) error
}

type JobAccess interface {
	ClaimedWorkerJob(context.Context, auth.Identity, string) (jobs.Job, error)
}

type SettingResolver interface {
	Resolve(context.Context, settings.Scope, string, string) (settings.ResolvedSetting, error)
}

type OAuthSettingManager interface {
	Delete(context.Context, auth.Identity, settings.Scope, string, string) error
	RotateSecrets(context.Context, string, settings.Scope, string, string, map[string]string) error
	Update(context.Context, auth.Identity, settings.Scope, string, string, map[string]interface{}) (settings.Setting, error)
}

type NotionOAuthProvider interface {
	Available() bool
	AuthorizationURL(string) (string, error)
	Exchange(context.Context, string) (NotionOAuthTokens, error)
	Refresh(context.Context, string) (NotionOAuthTokens, error)
	Revoke(context.Context, string) error
}

type NotionExporter interface {
	Export(context.Context, string, NotionExportRequest) (NotionExport, error)
	Check(context.Context, string, string) (string, error)
}

type ArtifactImporter interface {
	ImportModelFile(context.Context, ModelFileImport) (ModelFileReference, error)
}

type ModelFileImport struct {
	ProjectID      string
	CreatedBy      string
	SourceObjectID string
	SourceBlockID  string
	URL            string
	Filename       string
	MIMEType       string
}

type ModelFileReference struct {
	ArtifactID string
	VersionID  string
	Filename   string
	MIMEType   string
}

type Service struct {
	Access        Access
	Artifacts     ArtifactImporter
	Audit         AuditRecorder
	Clock         clock.Clock
	Generator     identity.Generator
	Jobs          JobAccess
	Notion        NotionExporter
	OAuth         NotionOAuthProvider
	OAuthSettings OAuthSettingManager
	Settings      SettingResolver
	Store         Store
}

func (service Service) Authenticate(ctx context.Context, authorization string) (auth.Identity, error) {
	return service.Access.Authenticate(ctx, authorization)
}

func (service Service) GetOverview(ctx context.Context, caller auth.Identity, projectID string) (Overview, error) {
	if err := service.authorize(ctx, caller, projectID, false); err != nil {
		return Overview{}, err
	}
	overview := Overview{ProjectID: projectID, GeneratedAt: service.now(), DiscoveredPages: []SourcePage{}, Questions: []Question{}}
	source, err := service.Store.GetSource(ctx, projectID)
	if errors.Is(err, ErrNotConfigured) || errors.Is(err, ErrNotFound) {
		return overview, nil
	}
	if err != nil {
		return Overview{}, err
	}
	service.addCountdown(&source)
	pages, err := service.Store.ListSourcePages(ctx, projectID)
	if err != nil {
		return Overview{}, err
	}
	questions, err := service.Store.ListQuestions(ctx, projectID)
	if err != nil {
		return Overview{}, err
	}
	overview.Configured = true
	overview.Source = &source
	overview.DiscoveredPages = pages
	overview.Questions = questions
	return overview, nil
}

func (service Service) GetSource(ctx context.Context, caller auth.Identity, projectID string) (Source, error) {
	if err := service.authorize(ctx, caller, projectID, false); err != nil {
		return Source{}, err
	}
	source, err := service.Store.GetSource(ctx, projectID)
	if err == nil {
		service.addCountdown(&source)
	}
	return source, err
}

func (service Service) ListQuestions(ctx context.Context, caller auth.Identity, projectID string) ([]Question, error) {
	if err := service.authorize(ctx, caller, projectID, false); err != nil {
		return nil, err
	}
	return service.Store.ListQuestions(ctx, projectID)
}

func (service Service) GetQuestion(ctx context.Context, caller auth.Identity, projectID, questionID string) (QuestionDetail, error) {
	if err := service.authorize(ctx, caller, projectID, false); err != nil {
		return QuestionDetail{}, err
	}
	question, err := service.Store.GetQuestion(ctx, projectID, questionID)
	if err != nil {
		return QuestionDetail{}, err
	}
	summaries, err := service.Store.ListSnapshotSummaries(ctx, projectID, questionID)
	if err != nil {
		return QuestionDetail{}, err
	}
	detail := QuestionDetail{Question: question, Snapshots: summaries}
	if question.LatestSnapshotID != "" {
		snapshot, getErr := service.Store.GetSnapshot(ctx, projectID, questionID, question.LatestSnapshotID)
		if getErr != nil {
			return QuestionDetail{}, getErr
		}
		detail.LatestSnapshot = &snapshot
	}
	return detail, nil
}

func (service Service) CreateQuestion(ctx context.Context, caller auth.Identity, projectID string, input CreateQuestionInput) (Question, error) {
	if err := service.authorize(ctx, caller, projectID, true); err != nil {
		return Question{}, err
	}
	input.Code = strings.TrimSpace(input.Code)
	input.Title = strings.TrimSpace(input.Title)
	input.NotionPageID = normalizePageID(input.NotionPageID)
	if !questionCodePattern.MatchString(input.Code) || input.Title == "" || len([]rune(input.Title)) > 255 || input.NotionPageID == "" || input.Position < 0 {
		return Question{}, ErrInvalid
	}
	pages, err := service.Store.ListSourcePages(ctx, projectID)
	if err != nil {
		return Question{}, err
	}
	var selected *SourcePage
	for index := range pages {
		if pages[index].NotionPageID == input.NotionPageID {
			selected = &pages[index]
			break
		}
	}
	if selected == nil {
		return Question{}, ErrPageUndiscovered
	}
	source, err := service.Store.GetSource(ctx, projectID)
	if err != nil {
		return Question{}, err
	}
	id, err := service.Generator.New()
	if err != nil {
		return Question{}, err
	}
	now := service.now()
	question := Question{ID: id, ProjectID: projectID, SourceID: source.ID, Code: input.Code, Title: input.Title, NotionPageID: selected.NotionPageID, NotionPageURL: selected.URL, Position: input.Position, SnapshotCount: 0, SyncStatus: SyncIdle, CreatedBy: caller.User.ID, UpdatedBy: caller.User.ID, CreatedAt: now, UpdatedAt: now}
	created, err := service.Store.CreateQuestion(ctx, question)
	service.record(ctx, caller, projectID, "model.question.created", outcome(err), id, map[string]interface{}{"code": input.Code, "notion_page_id": input.NotionPageID})
	return created, err
}

func (service Service) UpdateQuestion(ctx context.Context, caller auth.Identity, projectID, questionID string, input UpdateQuestionInput) (Question, error) {
	if err := service.authorize(ctx, caller, projectID, true); err != nil {
		return Question{}, err
	}
	if input.Code == nil && input.Title == nil && input.NotionPageID == nil && input.Position == nil {
		return Question{}, ErrInvalid
	}
	if input.Code != nil {
		value := strings.TrimSpace(*input.Code)
		if !questionCodePattern.MatchString(value) {
			return Question{}, ErrInvalid
		}
		input.Code = &value
	}
	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if value == "" || len([]rune(value)) > 255 {
			return Question{}, ErrInvalid
		}
		input.Title = &value
	}
	if input.Position != nil && *input.Position < 0 {
		return Question{}, ErrInvalid
	}
	if input.NotionPageID != nil {
		value := normalizePageID(*input.NotionPageID)
		pages, err := service.Store.ListSourcePages(ctx, projectID)
		if err != nil {
			return Question{}, err
		}
		found := false
		for _, page := range pages {
			if page.NotionPageID == value {
				found = true
				break
			}
		}
		if !found {
			return Question{}, ErrPageUndiscovered
		}
		input.NotionPageID = &value
	}
	updated, err := service.Store.UpdateQuestion(ctx, projectID, questionID, input, caller.User.ID, service.now())
	service.record(ctx, caller, projectID, "model.question.updated", outcome(err), questionID, map[string]interface{}{})
	return updated, err
}

func (service Service) DeleteQuestion(ctx context.Context, caller auth.Identity, projectID, questionID string) error {
	if err := service.authorize(ctx, caller, projectID, true); err != nil {
		return err
	}
	err := service.Store.ArchiveQuestion(ctx, projectID, questionID, caller.User.ID, service.now())
	service.record(ctx, caller, projectID, "model.question.archived", outcome(err), questionID, map[string]interface{}{})
	return err
}

func (service Service) ListSnapshots(ctx context.Context, caller auth.Identity, projectID, questionID string) ([]SnapshotSummary, error) {
	if err := service.authorize(ctx, caller, projectID, false); err != nil {
		return nil, err
	}
	if _, err := service.Store.GetQuestion(ctx, projectID, questionID); err != nil {
		return nil, err
	}
	return service.Store.ListSnapshotSummaries(ctx, projectID, questionID)
}

func (service Service) GetSnapshot(ctx context.Context, caller auth.Identity, projectID, questionID, snapshotID string) (Snapshot, error) {
	if err := service.authorize(ctx, caller, projectID, false); err != nil {
		return Snapshot{}, err
	}
	return service.Store.GetSnapshot(ctx, projectID, questionID, snapshotID)
}

func (service Service) UpdateSnapshot(ctx context.Context, caller auth.Identity, projectID, questionID, snapshotID string, input UpdateSnapshotInput) (Snapshot, error) {
	if err := service.authorize(ctx, caller, projectID, true); err != nil {
		return Snapshot{}, err
	}
	if input.Tags == nil && input.VersionNote == nil {
		return Snapshot{}, ErrInvalid
	}
	if input.Tags != nil {
		tags, err := normalizeTags(*input.Tags)
		if err != nil {
			return Snapshot{}, err
		}
		input.Tags = &tags
	}
	if input.VersionNote != nil {
		value := strings.TrimSpace(*input.VersionNote)
		if len([]rune(value)) > 4000 {
			return Snapshot{}, ErrInvalid
		}
		input.VersionNote = &value
	}
	updated, err := service.Store.UpdateSnapshot(ctx, projectID, questionID, snapshotID, input, caller.User.ID, service.now())
	service.record(ctx, caller, projectID, "model.snapshot.metadata.updated", outcome(err), snapshotID, map[string]interface{}{})
	return updated, err
}

func (service Service) Diff(ctx context.Context, caller auth.Identity, projectID, questionID, fromID, toID string) (Diff, error) {
	if err := service.authorize(ctx, caller, projectID, false); err != nil {
		return Diff{}, err
	}
	from, err := service.Store.GetSnapshot(ctx, projectID, questionID, fromID)
	if err != nil {
		return Diff{}, err
	}
	to, err := service.Store.GetSnapshot(ctx, projectID, questionID, toID)
	if err != nil {
		return Diff{}, err
	}
	return CompareSnapshots(from, to), nil
}

func (service Service) RequestSourceSync(ctx context.Context, caller auth.Identity, projectID, trigger string) (Sync, error) {
	if err := service.authorizeSync(ctx, caller, projectID); err != nil {
		return Sync{}, err
	}
	source, err := service.Store.GetSource(ctx, projectID)
	if err != nil {
		return Sync{}, err
	}
	return service.requestSync(ctx, caller.User.ID, source, Question{}, SyncScopeSource, trigger, true)
}

func (service Service) RequestQuestionSync(ctx context.Context, caller auth.Identity, projectID, questionID, trigger string) (Sync, error) {
	if err := service.authorizeSync(ctx, caller, projectID); err != nil {
		return Sync{}, err
	}
	source, err := service.Store.GetSource(ctx, projectID)
	if err != nil {
		return Sync{}, err
	}
	question, err := service.Store.GetQuestion(ctx, projectID, questionID)
	if err != nil {
		return Sync{}, err
	}
	pages, err := service.Store.ListSourcePages(ctx, projectID)
	if err != nil {
		return Sync{}, err
	}
	if !questionPageDiscovered(question, pages) {
		return Sync{}, ErrPageUndiscovered
	}
	return service.requestSync(ctx, caller.User.ID, source, question, SyncScopeQuestion, trigger, trigger == SyncTriggerManual)
}

func (service Service) requestSync(ctx context.Context, actorID string, source Source, question Question, scope, trigger string, resetCountdown bool) (Sync, error) {
	sync, jobInput, next, err := service.newSync(actorID, source, question, scope, trigger, resetCountdown)
	if err != nil {
		return Sync{}, err
	}
	created, err := service.Store.CreateSync(ctx, sync, jobInput, next)
	resourceID := sync.ID
	if created.ID != "" {
		resourceID = created.ID
	}
	service.record(ctx, auth.Identity{Kind: "system", User: auth.User{ID: actorID}}, source.ProjectID, "model.sync.requested", outcome(err), resourceID, map[string]interface{}{"scope": scope, "trigger": trigger, "reused_active": created.ID != "" && created.ID != sync.ID})
	return created, err
}

func (service Service) requestSyncInTransaction(ctx context.Context, tx transaction.Tx, actorID string, source Source, question Question, scope, trigger string) (Sync, error) {
	sync, jobInput, _, err := service.newSync(actorID, source, question, scope, trigger, false)
	if err != nil {
		return Sync{}, err
	}
	return service.Store.CreateSyncInTransaction(ctx, tx, sync, jobInput, nil)
}

func (service Service) newSync(actorID string, source Source, question Question, scope, trigger string, resetCountdown bool) (Sync, jobs.CreateInput, *time.Time, error) {
	if trigger != SyncTriggerManual && trigger != SyncTriggerScheduled && trigger != SyncTriggerSettings {
		return Sync{}, jobs.CreateInput{}, nil, ErrInvalid
	}
	syncID, err := service.Generator.New()
	if err != nil {
		return Sync{}, jobs.CreateInput{}, nil, err
	}
	now := service.now()
	jobType := JobTypeDiscover
	payload := map[string]interface{}{"project_id": source.ProjectID, "source_id": source.ID, "sync_id": syncID, "mode": "discover"}
	if scope == SyncScopeQuestion {
		jobType = JobTypeSnapshot
		payload["mode"] = "snapshot"
		payload["question_id"] = question.ID
		payload["notion_page_id"] = question.NotionPageID
	}
	jobInput := jobs.CreateInput{ProjectID: source.ProjectID, JobType: jobType, Payload: payload, IdempotencyKey: "model-sync-" + syncID, MaxAttempts: 3, TimeoutSeconds: 900}
	sync := Sync{ID: syncID, ProjectID: source.ProjectID, SourceID: source.ID, QuestionID: question.ID, Scope: scope, Trigger: trigger, Status: SyncQueued, RequestedBy: actorID, RequestedAt: now, UpdatedAt: now}
	var next *time.Time
	if resetCountdown && source.AutoSyncEnabled {
		value := now.Add(time.Duration(source.AutoSyncIntervalSeconds) * time.Second)
		next = &value
	}
	return sync, jobInput, next, nil
}

func (service Service) ApplySettingEvent(ctx context.Context, event contract.EventEnvelope) error {
	typeKey, _ := event.Payload["type_key"].(string)
	scope, _ := event.Payload["scope"].(string)
	projectID, _ := event.Payload["scope_id"].(string)
	if typeKey != SettingTypeNotion || scope != string(settings.ScopeProject) || projectID == "" {
		return nil
	}
	actorID := event.Actor["user_id"]
	if event.EventType == "settings.deleted" {
		return service.Store.DisableSource(ctx, projectID, actorID, service.now())
	}
	resolved, err := service.Settings.Resolve(ctx, settings.ScopeProject, projectID, SettingTypeNotion)
	if err != nil {
		return err
	}
	config, err := sourceConfig(projectID, actorID, resolved)
	if err != nil {
		return err
	}
	sourceID, err := service.Generator.New()
	if err != nil {
		return err
	}
	source, err := service.Store.UpsertSource(ctx, config, sourceID, service.now())
	if err != nil {
		return err
	}
	_, err = service.requestSync(ctx, actorID, source, Question{}, SyncScopeSource, SyncTriggerSettings, true)
	if errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}

func (service Service) authorize(ctx context.Context, caller auth.Identity, projectID string, manage bool) error {
	permission := project.PermissionModelRead
	if manage {
		permission = project.PermissionModelManage
	}
	return service.Access.Authorize(ctx, caller, projectID, permission)
}

func (service Service) authorizeSync(ctx context.Context, caller auth.Identity, projectID string) error {
	return service.Access.Authorize(ctx, caller, projectID, project.PermissionModelSync)
}

func (service Service) now() time.Time { return service.Clock.Now().UTC() }

func (service Service) addCountdown(source *Source) {
	if !source.AutoSyncEnabled || source.NextSyncAt == nil {
		return
	}
	seconds := int(source.NextSyncAt.Sub(service.now()).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	source.CountdownSeconds = &seconds
}

func (service Service) record(ctx context.Context, caller auth.Identity, projectID, action, result, resourceID string, metadata map[string]interface{}) {
	if service.Audit == nil {
		return
	}
	_ = service.Audit.Record(ctx, audit.Event{Action: action, ActorID: caller.User.ID, ActorKind: caller.Kind, Category: "model", Outcome: result, ProjectID: projectID, ResourceID: resourceID, ResourceType: "model", Source: "core", Metadata: metadata})
}

func outcome(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}

func normalizeTags(values []string) ([]string, error) {
	if len(values) > 20 {
		return nil, ErrInvalid
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || len([]rune(value)) > 64 {
			return nil, ErrInvalid
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func sourceConfig(projectID, actorID string, resolved settings.ResolvedSetting) (SourceConfig, error) {
	rootURL, _ := resolved.Values["root_page_url"].(string)
	token, _ := resolved.Values["access_token"].(string)
	if strings.TrimSpace(token) == "" {
		token, _ = resolved.Values["integration_token"].(string)
	}
	if strings.TrimSpace(token) == "" {
		return SourceConfig{}, ErrNotConfigured
	}
	pageID, normalizedURL, err := parseNotionPageURL(rootURL)
	if err != nil {
		return SourceConfig{}, err
	}
	enabled, ok := resolved.Values["auto_sync_enabled"].(bool)
	if !ok {
		enabled = true
	}
	interval := defaultSyncInterval
	if number, ok := resolved.Values["auto_sync_interval_seconds"].(float64); ok {
		interval = time.Duration(int(number)) * time.Second
	}
	if interval < minimumSyncInterval || interval > maximumSyncInterval {
		return SourceConfig{}, ErrInvalid
	}
	return SourceConfig{ProjectID: projectID, RootPageID: pageID, RootPageURL: normalizedURL, AutoSyncEnabled: enabled, Interval: interval, ActorID: actorID, SettingsVersion: resolved.Version}, nil
}

func parseNotionPageURL(raw string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	hostname := strings.ToLower(parsed.Hostname())
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || !isNotionPageHost(hostname) {
		return "", "", ErrInvalid
	}
	pageID := normalizePageID(parsed.Path)
	if pageID == "" {
		return "", "", ErrInvalid
	}
	parsed.Fragment = ""
	return pageID, parsed.String(), nil
}

func isNotionPageHost(hostname string) bool {
	return hostname == "notion.so" || strings.HasSuffix(hostname, ".notion.so") ||
		hostname == "notion.com" || strings.HasSuffix(hostname, ".notion.com") ||
		hostname == "notion.site" || strings.HasSuffix(hostname, ".notion.site")
}

func normalizePageID(raw string) string {
	if value := uuidPageIDPattern.FindString(raw); value != "" {
		return strings.ToLower(value)
	}
	value := hexPageIDPattern.FindString(raw)
	if value == "" {
		return ""
	}
	value = strings.ToLower(value)
	return value[0:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:32]
}

func numberValue(value interface{}) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(typed)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func questionPageDiscovered(question Question, pages []SourcePage) bool {
	for _, page := range pages {
		if page.NotionPageID == question.NotionPageID {
			return true
		}
	}
	return false
}

func validateSnapshotResult(result SnapshotResult) error {
	if result.Mode != "snapshot" || result.SyncID == "" || result.QuestionID == "" || strings.TrimSpace(result.Title) == "" || !sha256Pattern.MatchString(result.ContentHash) || result.Blocks == nil || result.Outline == nil || result.Media == nil {
		return ErrInvalid
	}
	if len(result.Title) > 255 || len(result.Summary) > 10000 {
		return ErrInvalid
	}
	return nil
}

func validateDiscoverResult(result DiscoverResult) error {
	if result.Mode != "discover" || result.SyncID == "" || strings.TrimSpace(result.RootTitle) == "" || len(result.RootTitle) > 255 || result.Pages == nil {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for index := range result.Pages {
		page := &result.Pages[index]
		page.PageID = normalizePageID(page.PageID)
		if page.ParentPageID != nil {
			value := normalizePageID(*page.ParentPageID)
			page.ParentPageID = &value
		}
		if page.PageID == "" || page.Depth < 1 || page.Depth > 64 || strings.TrimSpace(page.Title) == "" || len(page.Title) > 255 {
			return ErrInvalid
		}
		if _, exists := seen[page.PageID]; exists {
			return ErrInvalid
		}
		seen[page.PageID] = struct{}{}
	}
	return nil
}

func settingString(resolved settings.ResolvedSetting, key string) string {
	value, _ := resolved.Values[key].(string)
	return strings.TrimSpace(value)
}

func (config SourceConfig) String() string {
	return fmt.Sprintf("%s:%s:%d", config.ProjectID, config.RootPageID, config.SettingsVersion)
}
