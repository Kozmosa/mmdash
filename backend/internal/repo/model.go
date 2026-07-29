// Package repo owns managed Git repositories and every Git operation in Core.
package repo

import "time"

// Provider identifies a reviewed remote transport.
type Provider string

const (
	ProviderGitHub Provider = "github"
	ProviderLocal  Provider = "local"
)

// WorkspaceKind is a stable logical workspace independent of remote branch names.
type WorkspaceKind string

const (
	WorkspaceCode    WorkspaceKind = "code"
	WorkspaceArticle WorkspaceKind = "article"
	WorkspaceResult  WorkspaceKind = "result"
)

// RepositoryStatus describes the managed repository lifecycle.
type RepositoryStatus string

const (
	StatusPending      RepositoryStatus = "pending"
	StatusCloning      RepositoryStatus = "cloning"
	StatusConfiguring  RepositoryStatus = "configuring"
	StatusReady        RepositoryStatus = "ready"
	StatusSyncing      RepositoryStatus = "syncing"
	StatusError        RepositoryStatus = "error"
	StatusDisconnected RepositoryStatus = "disconnected"
)

// WorkspaceStatus describes one logical worktree.
type WorkspaceStatus string

const (
	WorkspacePending WorkspaceStatus = "pending"
	WorkspaceReady   WorkspaceStatus = "ready"
	WorkspaceMissing WorkspaceStatus = "missing"
	WorkspaceDirty   WorkspaceStatus = "dirty"
	WorkspaceError   WorkspaceStatus = "error"
)

// Repository is the authoritative repository configuration and runtime status.
type Repository struct {
	CanonicalRemoteURL string           `json:"-"`
	CleanupAfter       *time.Time       `json:"-"`
	ConnectedAt        *time.Time       `json:"-"`
	CreatedAt          time.Time        `json:"created_at"`
	CreatedBy          string           `json:"-"`
	DefaultBranch      string           `json:"default_branch"`
	DisplayName        string           `json:"display_name"`
	ID                 string           `json:"repository_id"`
	LastErrorCode      *string          `json:"last_error_code"`
	LastErrorMessage   *string          `json:"last_error_message"`
	LastSyncedAt       *time.Time       `json:"last_synced_at"`
	NextSyncAt         *time.Time       `json:"-"`
	ProjectID          string           `json:"project_id"`
	Provider           Provider         `json:"provider"`
	RemoteURL          *string          `json:"remote_url"`
	SettingsVersion    int64            `json:"settings_version"`
	Status             RepositoryStatus `json:"status"`
	StorageKey         string           `json:"-"`
	SyncAttempts       int              `json:"-"`
	SyncLeaseExpiresAt *time.Time       `json:"-"`
	SyncLockedBy       *string          `json:"-"`
	SyncRequestedAt    *time.Time       `json:"-"`
	SyncStartedAt      *time.Time       `json:"-"`
	UpdatedAt          time.Time        `json:"updated_at"`
	Webhook            Webhook          `json:"webhook"`
	Workspaces         []Workspace      `json:"workspaces"`
}

// Webhook is the browser-safe GitHub webhook configuration.
type Webhook struct {
	HookID           string `json:"hook_id"`
	PublicURL        string `json:"public_url"`
	Secret           string `json:"secret,omitempty"`
	SecretConfigured bool   `json:"secret_configured"`
}

// Workspace maps a logical kind to one real remote branch and managed worktree.
type Workspace struct {
	HeadCommitSHA   *string         `json:"head_commit_sha"`
	LocalBranch     string          `json:"local_branch"`
	RemoteBranch    string          `json:"remote_branch"`
	Status          WorkspaceStatus `json:"status"`
	TreeSHA         *string         `json:"tree_sha"`
	UpdatedAt       time.Time       `json:"updated_at"`
	Workspace       WorkspaceKind   `json:"workspace"`
	WorktreeRelpath string          `json:"-"`
}

// WorkspaceMappings always contains all three logical workspaces.
type WorkspaceMappings struct {
	CodeBranch    string
	ArticleBranch string
	ResultBranch  string
}

// ConnectionSnapshot is the secret-free, tested settings snapshot persisted by Repo.
type ConnectionSnapshot struct {
	CanonicalRemoteURL string
	DefaultBranch      string
	DisplayName        string
	ProjectID          string
	Provider           Provider
	SettingsVersion    int64
	Workspaces         WorkspaceMappings
}

// SyncClaim is one repository lease owned by a Core instance.
type SyncClaim struct {
	Repository Repository
	Requested  time.Time
	Source     string
}

// SyncedWorkspace is the immutable result of one fetched workspace.
type SyncedWorkspace struct {
	HeadCommitSHA    string
	HistoryRewritten bool
	Status           WorkspaceStatus
	TreeSHA          string
	Workspace        WorkspaceKind
}

// SyncResult is produced by Git outside a database transaction.
type SyncResult struct {
	Commits    []Commit
	Initial    bool
	Source     string
	Workspaces []SyncedWorkspace
}

// Commit records immutable metadata observed by Core.
type Commit struct {
	Author       GitIdentity   `json:"author"`
	Changes      []ChangedPath `json:"changes"`
	CommitSHA    string        `json:"commit_sha"`
	Committer    GitIdentity   `json:"committer"`
	FirstSeenAt  time.Time     `json:"first_seen_at"`
	Message      string        `json:"message"`
	ParentSHAs   []string      `json:"parent_shas"`
	RepositoryID string        `json:"repository_id"`
	Source       string        `json:"source"`
	TreeSHA      string        `json:"tree_sha"`
}

// GitIdentity is immutable author or committer metadata.
type GitIdentity struct {
	Email string    `json:"email"`
	Name  string    `json:"name"`
	Time  time.Time `json:"time"`
}

// ChangedPath is a normalized Git diff entry.
type ChangedPath struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previous_path,omitempty"`
	Status       string `json:"status"`
}

// Checkout is a detached, leased worktree without a public server path.
type Checkout struct {
	CheckoutID      string     `json:"checkout_id"`
	CheckoutRelpath string     `json:"-"`
	CommitSHA       string     `json:"commit_sha"`
	CreatedAt       time.Time  `json:"created_at"`
	CreatedBy       string     `json:"-"`
	ExpiresAt       time.Time  `json:"expires_at"`
	Purpose         string     `json:"purpose"`
	ReleasedAt      *time.Time `json:"released_at"`
	RepositoryID    string     `json:"repository_id"`
	Status          string     `json:"status"`
}

func localBranch(kind WorkspaceKind) string {
	return "mmdash/" + string(kind)
}

func worktreePath(kind WorkspaceKind) string {
	return "worktrees/" + string(kind)
}

func mappingList(mappings WorkspaceMappings, now time.Time) []Workspace {
	return []Workspace{
		{
			LocalBranch: localBranch(WorkspaceCode), RemoteBranch: mappings.CodeBranch,
			Status: WorkspacePending, UpdatedAt: now, Workspace: WorkspaceCode,
			WorktreeRelpath: worktreePath(WorkspaceCode),
		},
		{
			LocalBranch: localBranch(WorkspaceArticle), RemoteBranch: mappings.ArticleBranch,
			Status: WorkspacePending, UpdatedAt: now, Workspace: WorkspaceArticle,
			WorktreeRelpath: worktreePath(WorkspaceArticle),
		},
		{
			LocalBranch: localBranch(WorkspaceResult), RemoteBranch: mappings.ResultBranch,
			Status: WorkspacePending, UpdatedAt: now, Workspace: WorkspaceResult,
			WorktreeRelpath: worktreePath(WorkspaceResult),
		},
	}
}
