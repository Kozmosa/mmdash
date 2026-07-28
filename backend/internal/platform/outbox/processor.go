package outbox

import (
	"context"
	"fmt"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

// EventBus is the in-process consumer registry used by durable delivery.
type EventBus interface {
	Deliver(context.Context, string, contract.EventEnvelope) error
	Matching(string) []string
}

// ProcessorOptions controls leases, retries, and idle polling.
type ProcessorOptions struct {
	DeliveryLease time.Duration
	EventLease    time.Duration
	IdlePoll      time.Duration
	RetryDelay    time.Duration
}

// Processor publishes Outbox rows and delivers each consumer independently.
type Processor struct {
	Bus     EventBus
	Options ProcessorOptions
	Owner   string
	Store   DeliveryStore
}

// Run continuously drains publication and delivery work until cancellation.
func (processor Processor) Run(
	ctx context.Context,
	onError func(error),
) {
	options := processor.options()
	for {
		worked, err := processor.RunOnce(ctx)
		if err != nil && onError != nil {
			onError(err)
		}
		delay := time.Duration(0)
		if err != nil || !worked {
			delay = options.IdlePoll
		}
		if delay == 0 {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		timer := time.NewTimer(delay)
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

// RunOnce publishes at most one event and processes at most one delivery.
func (processor Processor) RunOnce(ctx context.Context) (bool, error) {
	if processor.Store == nil || processor.Bus == nil || processor.Owner == "" {
		return false, fmt.Errorf("outbox processor dependencies and owner are required")
	}
	options := processor.options()
	worked := false
	record, err := processor.Store.ClaimEvent(ctx, processor.Owner, options.EventLease)
	if err != nil {
		return false, err
	}
	if record != nil {
		worked = true
		consumers := processor.Bus.Matching(record.Envelope.EventType)
		if err := processor.Store.Publish(
			ctx,
			*record,
			processor.Owner,
			consumers,
		); err != nil {
			if releaseErr := processor.Store.FailEvent(
				ctx,
				*record,
				processor.Owner,
				err.Error(),
				options.RetryDelay,
			); releaseErr != nil {
				return true, fmt.Errorf(
					"publish event: %v; release publication lease: %w",
					err,
					releaseErr,
				)
			}
			return true, fmt.Errorf("publish event: %w", err)
		}
	}
	delivery, err := processor.Store.ClaimDelivery(
		ctx,
		processor.Owner,
		options.DeliveryLease,
	)
	if err != nil {
		return worked, err
	}
	if delivery == nil {
		return worked, nil
	}
	worked = true
	if err := processor.Bus.Deliver(
		ctx,
		delivery.ConsumerName,
		delivery.Envelope,
	); err != nil {
		if persistErr := processor.Store.FailDelivery(
			ctx,
			*delivery,
			err.Error(),
			options.RetryDelay,
		); persistErr != nil {
			return true, fmt.Errorf(
				"deliver event: %v; record delivery failure: %w",
				err,
				persistErr,
			)
		}
		return true, nil
	}
	if err := processor.Store.CompleteDelivery(
		ctx,
		delivery.ID,
		processor.Owner,
	); err != nil {
		return true, fmt.Errorf("complete event delivery: %w", err)
	}
	return true, nil
}

func (processor Processor) options() ProcessorOptions {
	options := processor.Options
	if options.EventLease <= 0 {
		options.EventLease = 30 * time.Second
	}
	if options.DeliveryLease <= 0 {
		options.DeliveryLease = 30 * time.Second
	}
	if options.IdlePoll <= 0 {
		options.IdlePoll = 500 * time.Millisecond
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = 2 * time.Second
	}
	return options
}
