package repo

import (
	"context"
	"time"
)

// Store is the authoritative Repo persistence boundary.
type Store interface {
	ClaimSync(context.Context, string, time.Time, time.Duration, int) ([]SyncClaim, error)
	CompleteSync(context.Context, string, SyncClaim, SyncResult, time.Time) error
	CreatePending(context.Context, string, ConnectionSnapshot) (Repository, error)
	Disconnect(context.Context, string, time.Time, time.Time) error
	FailSync(context.Context, string, string, string, string, time.Time, time.Time) error
	GetByHook(context.Context, string) (Repository, error)
	GetByProject(context.Context, string) (Repository, error)
	RenewSyncLease(context.Context, string, string, time.Time) error
	RequestSync(context.Context, string, time.Time) (Repository, error)
	RequestSyncSource(context.Context, string, time.Time, string) (Repository, error)
	UpdateMappings(
		context.Context, string, WorkspaceMappings, int64, time.Time,
	) (Repository, error)
}
