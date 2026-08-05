package project

import (
	"context"
	"errors"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
)

type InvitationExpiryStore interface {
	ExpireInvitations(context.Context, time.Time, int) (int, error)
}

// InvitationExpiryProcessor advances invitation lifecycle state without
// depending on a user opening the Project invitation list.
type InvitationExpiryProcessor struct {
	BatchSize int
	Clock     clock.Clock
	Poll      time.Duration
	Store     InvitationExpiryStore
}

func (processor InvitationExpiryProcessor) Run(ctx context.Context, onError func(error)) {
	options := processor.options()
	for {
		processed, err := processor.RunBatch(ctx)
		if err != nil && !errors.Is(err, context.Canceled) && onError != nil {
			onError(err)
		}
		if ctx.Err() != nil {
			return
		}
		if err == nil && processed == options.BatchSize {
			continue
		}
		timer := time.NewTimer(options.Poll)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func (processor InvitationExpiryProcessor) RunBatch(ctx context.Context) (int, error) {
	options := processor.options()
	if processor.Store == nil {
		return 0, ErrInvalid
	}
	return processor.Store.ExpireInvitations(ctx, options.Clock.Now().UTC(), options.BatchSize)
}

func (processor InvitationExpiryProcessor) options() InvitationExpiryProcessor {
	options := processor
	if options.BatchSize < 1 {
		options.BatchSize = 100
	}
	if options.Clock == nil {
		options.Clock = clock.System{}
	}
	if options.Poll <= 0 {
		options.Poll = 30 * time.Second
	}
	return options
}
