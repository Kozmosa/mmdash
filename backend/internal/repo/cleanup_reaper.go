package repo

import (
	"context"
	"time"
)

// CleanupStore leases and completes delayed repository cleanup.
type CleanupStore interface {
	ClaimCleanup(context.Context, time.Time, time.Duration, int) ([]Repository, error)
	CompleteCleanup(context.Context, string) error
	RetryCleanup(context.Context, string, time.Time, time.Time) error
}

// RepositoryStorage removes one validated Core-generated storage key.
type RepositoryStorage interface {
	RemoveRepository(string) error
}

// CleanupReaper removes disconnected repositories only after their grace
// period, then deletes the corresponding metadata so the project may reconnect.
type CleanupReaper struct {
	Clock    interface{ Now() time.Time }
	Interval time.Duration
	Lease    time.Duration
	Limit    int
	Metrics  MetricSink
	OnError  func(error)
	Storage  RepositoryStorage
	Store    CleanupStore
}

func (reaper CleanupReaper) Run(ctx context.Context) {
	interval := reaper.interval()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := reaper.RunOnce(ctx); err != nil && reaper.OnError != nil {
				reaper.OnError(err)
			}
			timer.Reset(interval)
		}
	}
}

func (reaper CleanupReaper) RunOnce(ctx context.Context) error {
	if reaper.Clock == nil || reaper.Storage == nil || reaper.Store == nil {
		return ErrInvalid
	}
	now := reaper.Clock.Now().UTC()
	lease := reaper.Lease
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	limit := reaper.Limit
	if limit < 1 {
		limit = 10
	}
	repositories, err := reaper.Store.ClaimCleanup(ctx, now, lease, limit)
	if err != nil {
		return err
	}
	var firstError error
	for _, repository := range repositories {
		startedAt := time.Now()
		if err := reaper.Storage.RemoveRepository(repository.StorageKey); err != nil {
			if reaper.Metrics != nil {
				reaper.Metrics.ObserveRepoOperation(
					"cleanup",
					"error",
					string(repository.Provider),
					time.Since(startedAt),
				)
			}
			_ = reaper.Store.RetryCleanup(
				ctx, repository.ID, now.Add(reaper.interval()), now,
			)
			if firstError == nil {
				firstError = err
			}
			continue
		}
		if err := reaper.Store.CompleteCleanup(ctx, repository.ID); err != nil {
			if reaper.Metrics != nil {
				reaper.Metrics.ObserveRepoOperation(
					"cleanup",
					"error",
					string(repository.Provider),
					time.Since(startedAt),
				)
			}
			if firstError == nil {
				firstError = err
			}
		} else if reaper.Metrics != nil {
			reaper.Metrics.ObserveRepoOperation(
				"cleanup",
				"success",
				string(repository.Provider),
				time.Since(startedAt),
			)
		}
	}
	return firstError
}

func (reaper CleanupReaper) interval() time.Duration {
	if reaper.Interval > 0 {
		return reaper.Interval
	}
	return time.Minute
}
