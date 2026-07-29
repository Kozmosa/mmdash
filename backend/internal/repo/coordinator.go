package repo

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

// SyncStore is the lease and completion surface used by the coordinator.
type SyncStore interface {
	ClaimSync(context.Context, string, time.Time, time.Duration, int) ([]SyncClaim, error)
	CompleteSync(context.Context, string, SyncClaim, SyncResult, time.Time) error
	FailSync(context.Context, string, string, string, string, time.Time, time.Time) error
	RenewSyncLease(context.Context, string, string, time.Time) error
}

// Synchronizer performs Git I/O outside the database transaction.
type Synchronizer interface {
	Synchronize(context.Context, Repository, provider.Connection, string) (SyncResult, error)
}

// Coordinator owns bounded, leased synchronization inside Core.
type Coordinator struct {
	BatchSize  int
	Clock      interface{ Now() time.Time }
	Lease      time.Duration
	OnError    func(error)
	Owner      string
	Poll       time.Duration
	Providers  *provider.Registry
	Runtime    Synchronizer
	Settings   SettingsResolver
	Store      SyncStore
	RetryBase  time.Duration
	RetryLimit time.Duration
}

// Run polls until Core shutdown. Individual repository failures are persisted
// and reported without terminating the coordinator.
func (coordinator Coordinator) Run(ctx context.Context) {
	poll := coordinator.Poll
	if poll <= 0 {
		poll = 2 * time.Second
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := coordinator.RunOnce(ctx); err != nil {
				coordinator.report(err)
			}
			timer.Reset(poll)
		}
	}
}

// RunOnce claims a bounded batch and synchronizes different repositories in
// parallel. The Git client enforces the process-wide command concurrency cap.
func (coordinator Coordinator) RunOnce(ctx context.Context) error {
	if coordinator.Clock == nil ||
		coordinator.Owner == "" ||
		coordinator.Lease <= 0 ||
		coordinator.Providers == nil ||
		coordinator.Runtime == nil ||
		coordinator.Settings == nil ||
		coordinator.Store == nil {
		return ErrInvalid
	}
	batchSize := coordinator.BatchSize
	if batchSize < 1 {
		batchSize = 4
	}
	claims, err := coordinator.Store.ClaimSync(
		ctx, coordinator.Owner, coordinator.Clock.Now().UTC(),
		coordinator.Lease, batchSize,
	)
	if err != nil {
		return err
	}
	var firstError error
	var mutex sync.Mutex
	var group sync.WaitGroup
	for _, claim := range claims {
		claim := claim
		group.Add(1)
		go func() {
			defer group.Done()
			if err := coordinator.process(ctx, claim); err != nil {
				mutex.Lock()
				if firstError == nil {
					firstError = err
				}
				mutex.Unlock()
			}
		}()
	}
	group.Wait()
	return firstError
}

func (coordinator Coordinator) process(ctx context.Context, claim SyncClaim) error {
	syncContext, cancel := context.WithCancel(ctx)
	leaseDone := make(chan struct{})
	go coordinator.renewLease(syncContext, cancel, claim.Repository.ID, leaseDone)

	resolved, err := coordinator.Settings.Resolve(
		syncContext, settings.ScopeProject, claim.Repository.ProjectID, SettingType,
	)
	if err == nil {
		var config provider.Config
		config, err = providerConfig(resolved)
		if err == nil {
			var connection provider.Connection
			connection, err = coordinator.Providers.Test(syncContext, config)
			if err == nil {
				source := claim.Source
				if !validSyncSource(source) {
					source = "poll"
					claim.Source = source
				}
				var result SyncResult
				result, err = coordinator.Runtime.Synchronize(
					syncContext, claim.Repository, connection, source,
				)
				if err == nil {
					result.Source = source
					cancel()
					<-leaseDone
					return coordinator.Store.CompleteSync(
						ctx, coordinator.Owner, claim, result,
						coordinator.Clock.Now().UTC(),
					)
				}
			}
		}
	}
	cancel()
	<-leaseDone
	code, message := safeSyncFailure(err)
	now := coordinator.Clock.Now().UTC()
	retryAt := now.Add(coordinator.retryDelay(claim.Repository.SyncAttempts))
	if persistErr := coordinator.Store.FailSync(
		ctx, claim.Repository.ID, coordinator.Owner,
		code, message, retryAt, now,
	); persistErr != nil {
		return persistErr
	}
	coordinator.report(err)
	return nil
}

func (coordinator Coordinator) renewLease(
	ctx context.Context,
	cancel context.CancelFunc,
	repositoryID string,
	done chan<- struct{},
) {
	defer close(done)
	interval := coordinator.Lease / 3
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expiresAt := coordinator.Clock.Now().UTC().Add(coordinator.Lease)
			if err := coordinator.Store.RenewSyncLease(
				ctx, repositoryID, coordinator.Owner, expiresAt,
			); err != nil {
				coordinator.report(err)
				cancel()
				return
			}
		}
	}
}

func (coordinator Coordinator) retryDelay(attempts int) time.Duration {
	base := coordinator.RetryBase
	if base <= 0 {
		base = 2 * time.Second
	}
	limit := coordinator.RetryLimit
	if limit <= 0 {
		limit = 5 * time.Minute
	}
	if attempts < 0 {
		attempts = 0
	}
	if attempts > 16 {
		attempts = 16
	}
	delay := base * time.Duration(1<<attempts)
	if delay > limit {
		return limit
	}
	return delay
}

func (coordinator Coordinator) report(err error) {
	if err != nil && coordinator.OnError != nil {
		coordinator.OnError(err)
	}
}

func safeSyncFailure(err error) (string, string) {
	var safeError *SafeError
	switch {
	case errors.As(err, &safeError):
		return safeError.Code, safeError.Message
	case errors.Is(err, provider.ErrAuthentication),
		errors.Is(err, gitcli.ErrAuthentication):
		return "REPO_AUTH_FAILED", "Repository authentication failed"
	case errors.Is(err, provider.ErrBranchMissing):
		return "REPO_BRANCH_NOT_FOUND", "A mapped repository branch was not found"
	case errors.Is(err, provider.ErrRemoteNotFound):
		return "REPO_REMOTE_NOT_FOUND", "Repository was not found"
	case errors.Is(err, provider.ErrWritePermission):
		return "REPO_WRITE_PERMISSION_REQUIRED", "Repository write permission is required"
	case errors.Is(err, provider.ErrUnsupported):
		return "REPO_PROVIDER_UNSUPPORTED", "Repository provider is unsupported"
	case errors.Is(err, settings.ErrNotFound),
		errors.Is(err, settings.ErrTypeNotFound):
		return "REPO_SETTINGS_NOT_FOUND", "Repository settings are incomplete"
	case errors.Is(err, settings.ErrInvalid),
		errors.Is(err, provider.ErrInvalidConfig):
		return "REPO_SETTINGS_INVALID", "Repository settings are invalid"
	case errors.Is(err, gitcli.ErrTimeout),
		errors.Is(err, context.DeadlineExceeded):
		return "REPO_GIT_TIMEOUT", "Repository operation timed out"
	case errors.Is(err, gitcli.ErrOutputLimit):
		return "REPO_GIT_OUTPUT_LIMIT", "Repository command output exceeded its limit"
	case errors.Is(err, gitcli.ErrPathInvalid),
		errors.Is(err, gitcli.ErrStorageEscape):
		return "REPO_STORAGE_INVALID", "Repository storage failed a safety check"
	case errors.Is(err, ErrWorktreeDirty):
		return "REPO_WORKTREE_DIRTY", "A managed repository worktree contains changes"
	default:
		return "REPO_SYNC_FAILED", "Repository synchronization failed"
	}
}

var _ Synchronizer = Runtime{}
