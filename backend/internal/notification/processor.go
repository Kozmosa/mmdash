package notification

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

type DeliveryProcessor struct {
	Adapters   *AdapterRegistry
	Clock      func() time.Time
	Deliveries DeliveryStore
	Lease      time.Duration
	Owner      string
	Settings   SettingsResolver
	Metrics    *metrics.Registry
}

func (processor DeliveryProcessor) Run(ctx context.Context, onError func(error)) {
	for {
		worked, err := processor.RunOnce(ctx)
		if err != nil && onError != nil {
			onError(err)
		}
		if !worked {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (processor DeliveryProcessor) RunOnce(ctx context.Context) (bool, error) {
	if processor.Deliveries == nil || processor.Settings == nil || processor.Adapters == nil || processor.Owner == "" {
		return false, ErrNotReady
	}
	lease := processor.Lease
	if lease <= 0 {
		lease = time.Minute
	}
	delivery, notification, err := processor.Deliveries.ClaimDelivery(ctx, processor.Owner, lease)
	if err != nil || delivery == nil {
		return false, err
	}
	started := time.Now()
	outcome := ""
	defer func() {
		if processor.Metrics != nil && outcome != "" {
			processor.Metrics.ObserveNotificationDelivery(outcome, time.Since(started))
		}
	}()
	adapter, ok := processor.Adapters.Get(delivery.ChannelKey)
	if !ok {
		outcome = "failed"
		return true, processor.Deliveries.FailDelivery(ctx, delivery.ID, processor.Owner, "adapter_not_registered", 0, "adapter is not registered", false, 0)
	}
	resolved, err := processor.Settings.Resolve(ctx, settings.ScopeProject, delivery.ProjectID, delivery.ChannelKey)
	if err != nil {
		if errors.Is(err, settings.ErrNotFound) {
			outcome = "cancelled"
			return true, processor.Deliveries.CancelDelivery(ctx, delivery.ID, processor.Owner, "Notification channel is unavailable")
		}
		outcome = "retrying"
		return true, processor.Deliveries.FailDelivery(ctx, delivery.ID, processor.Owner, "configuration_unavailable", 0, "channel configuration unavailable", true, backoff(delivery.Attempts))
	}
	if resolved.Values["enabled"] != true {
		outcome = "cancelled"
		return true, processor.Deliveries.CancelDelivery(ctx, delivery.ID, processor.Owner, "Notification channel is disabled")
	}
	message, err := adapter.Render(ctx, notification, notification.TemplateVersion)
	if err != nil {
		outcome = "failed"
		return true, processor.Deliveries.FailDelivery(ctx, delivery.ID, processor.Owner, "rendering_error", 0, "notification rendering failed", false, 0)
	}
	result, err := adapter.Send(ctx, resolved.Values, delivery.ID, notification.TypeKey, message)
	if err != nil {
		var providerErr ProviderError
		if errors.As(err, &providerErr) {
			delay := providerErr.RetryAfter
			if delay <= 0 {
				delay = backoff(delivery.Attempts)
			}
			if providerErr.Retryable {
				outcome = "retrying"
			} else {
				outcome = "failed"
			}
			return true, processor.Deliveries.FailDelivery(ctx, delivery.ID, processor.Owner, providerErr.Code, providerErr.StatusCode, providerErr.Message, providerErr.Retryable, delay)
		}
		outcome = "retrying"
		return true, processor.Deliveries.FailDelivery(ctx, delivery.ID, processor.Owner, "provider_error", 0, "provider send failed", true, backoff(delivery.Attempts))
	}
	outcome = "delivered"
	return true, processor.Deliveries.CompleteDelivery(ctx, delivery.ID, processor.Owner, result)
}

func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	base := time.Duration(1<<uint(attempt-1)) * time.Second
	jitter := time.Duration(rand.Int63n(int64(time.Second)))
	return base + jitter
}
