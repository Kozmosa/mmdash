package repo

import (
	"context"
	"time"
)

// Store is the authoritative Repo persistence boundary.
type Store interface {
	ClaimSync(context.Context, string, time.Time, time.Duration, int) ([]SyncClaim, error)
	CompleteSync(context.Context, string, SyncClaim, SyncResult, time.Time) error
	CompleteReplacement(context.Context, string) error
	CreatePending(context.Context, string, ConnectionSnapshot) (Repository, error)
	ClaimReplacement(context.Context, string, time.Time, time.Duration) (Repository, error)
	Disconnect(context.Context, string, time.Time, time.Time) error
	FailSync(context.Context, string, string, string, string, time.Time, time.Time) error
	GetByHook(context.Context, string) (Repository, error)
	GetByID(context.Context, string) (Repository, error)
	GetByProject(context.Context, string) (Repository, error)
	ListRepositories(context.Context) ([]Repository, error)
	ReconnectPending(context.Context, ConnectionSnapshot, time.Time) (Repository, error)
	ReleaseReplacement(context.Context, string, time.Time, time.Time) error
	RenewSyncLease(context.Context, string, string, time.Time) error
	RequestPeriodicSyncs(context.Context, time.Time, time.Duration, int) (int, error)
	RequestSync(context.Context, string, time.Time) (Repository, error)
	RequestSyncSource(context.Context, string, time.Time, string) (Repository, error)
	RequestWorkspaceSyncSource(
		context.Context, string, WorkspaceKind, time.Time, string,
	) (Repository, error)
	UpdateMappings(
		context.Context, string, WorkspaceMappings, int64, time.Time,
	) (Repository, error)
}
