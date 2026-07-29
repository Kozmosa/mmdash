package repo

import (
	"context"
	"time"
)

// CheckoutReaper expires detached checkout leases and removes their worktrees.
type CheckoutReaper struct {
	Clock        interface{ Now() time.Time }
	Interval     time.Duration
	Limit        int
	OnError      func(error)
	Repositories interface {
		GetByID(context.Context, string) (Repository, error)
	}
	Runtime Runtime
	Store   CheckoutStore
}

func (reaper CheckoutReaper) Run(ctx context.Context) {
	interval := reaper.Interval
	if interval <= 0 {
		interval = time.Minute
	}
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

func (reaper CheckoutReaper) RunOnce(ctx context.Context) error {
	limit := reaper.Limit
	if limit < 1 {
		limit = 50
	}
	checkouts, err := reaper.Store.ExpireCheckouts(
		ctx, reaper.Clock.Now().UTC(), limit,
	)
	if err != nil {
		return err
	}
	var firstError error
	for _, checkout := range checkouts {
		repository, err := reaper.Repositories.GetByID(
			ctx, checkout.RepositoryID,
		)
		if err == nil {
			err = reaper.Runtime.ReleaseCheckout(
				ctx, repository, checkout.CheckoutRelpath,
			)
		}
		if err != nil {
			_ = reaper.Store.MarkCheckoutError(ctx, checkout.CheckoutID)
			if firstError == nil {
				firstError = err
			}
		}
	}
	return firstError
}
