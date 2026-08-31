package repo

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mmdash/mmdash/backend/internal/repo/provider"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

// SyncFailure is the bounded persistence record for one synchronization
// failure. Retryability controls automatic backoff; explicit manual or webhook
// requests may enqueue a new attempt after a terminal failure.
type SyncFailure struct {
	Code      string
	Message   string
	Retryable bool
}

// SyncFailureError carries safe structured observability while retaining the
// internal cause chain without serializing provider output or credentials.
type SyncFailureError struct {
	Cause        error
	Code         string
	Duration     time.Duration
	Provider     Provider
	RepositoryID string
	Retryable    bool
}

func (err *SyncFailureError) Error() string {
	return fmt.Sprintf("%s: repository synchronization failed", err.Code)
}

func (err *SyncFailureError) Unwrap() error {
	return err.Cause
}

// SyncStore is the lease and completion surface used by the coordinator.
type SyncStore interface {
	ClaimSync(context.Context, string, time.Time, time.Duration, int) ([]SyncClaim, error)
	CompleteSync(context.Context, string, SyncClaim, SyncResult, time.Time) error
	FailSync(context.Context, string, SyncClaim, SyncFailure, time.Time, time.Time) error
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
	Metrics    MetricSink
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

type projectSyncStore interface {
	SyncStore
	GetByProject(context.Context, string) (Repository, error)
	RequestSyncSource(context.Context, string, time.Time, string) (Repository, error)
}

// SyncProject requests and waits for one authoritative remote fetch. The same
// lease/transaction path used by the background coordinator remains the only
// way fetched heads become durable Repo state.
func (coordinator Coordinator) SyncProject(
	ctx context.Context,
	projectID string,
) (Repository, error) {
	store, ok := coordinator.Store.(projectSyncStore)
	if !ok || coordinator.Clock == nil || projectID == "" {
		return Repository{}, ErrInvalid
	}
	requestedAt := coordinator.Clock.Now().UTC()
	if _, err := store.RequestSyncSource(ctx, projectID, requestedAt, "manual"); err != nil {
		return Repository{}, err
	}
	for {
		// It is safe if the background loop wins the claim; the durable state
		// check below observes either coordinator's completion.
		_ = coordinator.RunOnce(ctx)
		repository, err := store.GetByProject(ctx, projectID)
		if err != nil {
			return Repository{}, err
		}
		if repository.SyncRequestedAt == nil && repository.SyncLockedBy == nil &&
			repository.LastSyncedAt != nil && !repository.LastSyncedAt.Before(requestedAt) {
			return repository, nil
		}
		if repository.SyncLockedBy == nil && repository.LastErrorCode != nil &&
			(repository.SyncRequestedAt == nil || repository.NextSyncAt == nil ||
				repository.NextSyncAt.After(coordinator.Clock.Now().UTC())) {
			message := "Repository synchronization failed"
			if repository.LastErrorMessage != nil {
				message = *repository.LastErrorMessage
			}
			return Repository{}, &SafeError{
				Code: *repository.LastErrorCode, Message: message,
				Retryable: retryableFailureCode(*repository.LastErrorCode),
			}
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Repository{}, ctx.Err()
		case <-timer.C:
		}
	}
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
	startedAt := time.Now()
	outcome := "error"
	defer func() {
		if coordinator.Metrics != nil {
			coordinator.Metrics.ObserveRepoOperation(
				"sync",
				outcome,
				string(claim.Repository.Provider),
				time.Since(startedAt),
			)
		}
	}()
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
					err = coordinator.Store.CompleteSync(
						ctx, coordinator.Owner, claim, result,
						coordinator.Clock.Now().UTC(),
					)
					if err == nil {
						outcome = "success"
					}
					return err
				}
			}
		}
	}
	cancel()
	<-leaseDone
	failure := classifySyncFailure(err)
	outcome = failure.Outcome
	now := coordinator.Clock.Now().UTC()
	retryAt := now.Add(coordinator.retryDelay(claim.Repository.SyncAttempts))
	if persistErr := coordinator.Store.FailSync(
		ctx, coordinator.Owner, claim,
		SyncFailure{
			Code: failure.Code, Message: failure.Message,
			Retryable: failure.Retryable,
		},
		retryAt, now,
	); persistErr != nil {
		return persistErr
	}
	coordinator.report(&SyncFailureError{
		Cause: err, Code: failure.Code, Duration: time.Since(startedAt),
		Provider:     claim.Repository.Provider,
		RepositoryID: claim.Repository.ID, Retryable: failure.Retryable,
	})
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

var _ Synchronizer = Runtime{}
