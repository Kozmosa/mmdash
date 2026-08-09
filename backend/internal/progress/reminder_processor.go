package progress

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
)

type ReminderProcessorStore interface {
	ClaimDueReminders(context.Context, string, time.Duration, int) ([]Reminder, error)
	CompleteReminder(context.Context, string, string, string) (Reminder, error)
	FailReminder(context.Context, string, string, string, string, time.Duration) (Reminder, error)
}

type ReminderProcessor struct {
	BatchSize  int
	Lease      time.Duration
	Metrics    *metrics.Registry
	Owner      string
	Poll       time.Duration
	RetryDelay time.Duration
	Store      ReminderProcessorStore
}

func (processor ReminderProcessor) Run(ctx context.Context, onError func(error)) {
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

func (processor ReminderProcessor) RunBatch(ctx context.Context) (int, error) {
	options := processor.options()
	if processor.Store == nil || options.Owner == "" {
		return 0, ErrInvalid
	}
	items, err := processor.Store.ClaimDueReminders(ctx, options.Owner, options.Lease, options.BatchSize)
	if err != nil {
		return 0, err
	}
	var firstErr error
	for _, item := range items {
		if ctx.Err() != nil {
			return len(items), errors.Join(firstErr, ctx.Err())
		}
		_, completeErr := processor.Store.CompleteReminder(ctx, item.ID, options.Owner, item.CreatedBy)
		if completeErr == nil {
			if processor.Metrics != nil {
				processor.Metrics.ObserveProgressReminder("triggered")
			}
			continue
		}
		failed, failErr := processor.Store.FailReminder(ctx, item.ID, options.Owner, "event_write_failed", "Progress reminder event could not be recorded", options.RetryDelay)
		if failErr == nil && processor.Metrics != nil {
			processor.Metrics.ObserveProgressReminder(failed.Status)
		}
		firstErr = errors.Join(firstErr, fmt.Errorf("complete progress reminder: %w", completeErr), failErr)
	}
	return len(items), firstErr
}

func (processor ReminderProcessor) options() ReminderProcessor {
	options := processor
	if options.BatchSize < 1 {
		options.BatchSize = 20
	}
	if options.Lease <= 0 {
		options.Lease = 30 * time.Second
	}
	if options.Poll <= 0 {
		options.Poll = time.Second
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = 2 * time.Second
	}
	return options
}
