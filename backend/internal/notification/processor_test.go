package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/settings"
)

type processorDeliveryStub struct {
	delivery       *Delivery
	notification   Notification
	claimedOwner   string
	completed      bool
	cancelled      bool
	failed         bool
	failureCode    string
	failureRetry   bool
	failureDelay   time.Duration
	cancelledError string
}

func (stub *processorDeliveryStub) EnqueueDelivery(context.Context, Notification, string, int64) (Delivery, error) {
	return Delivery{}, nil
}
func (stub *processorDeliveryStub) ClaimDelivery(_ context.Context, owner string, _ time.Duration) (*Delivery, Notification, error) {
	stub.claimedOwner = owner
	return stub.delivery, stub.notification, nil
}
func (stub *processorDeliveryStub) CompleteDelivery(context.Context, string, string) error {
	stub.completed = true
	return nil
}
func (stub *processorDeliveryStub) FailDelivery(_ context.Context, _ string, _ string, code string, _ int, _ string, retryable bool, delay time.Duration) error {
	stub.failed = true
	stub.failureCode = code
	stub.failureRetry = retryable
	stub.failureDelay = delay
	return nil
}
func (stub *processorDeliveryStub) CancelDelivery(_ context.Context, _ string, _ string, message string) error {
	stub.cancelled = true
	stub.cancelledError = message
	return nil
}
func (stub *processorDeliveryStub) CancelPending(context.Context, string, string) error { return nil }

type processorSettingsStub struct {
	resolved settings.ResolvedSetting
	err      error
}

func (stub processorSettingsStub) Resolve(context.Context, settings.Scope, string, string) (settings.ResolvedSetting, error) {
	return stub.resolved, stub.err
}

type processorAdapter struct {
	key       string
	sendError error
}

func (adapter processorAdapter) Key() string                                        { return adapter.key }
func (adapter processorAdapter) ValidateConfig(map[string]interface{}) error        { return nil }
func (adapter processorAdapter) Test(context.Context, map[string]interface{}) error { return nil }
func (adapter processorAdapter) Render(context.Context, Notification, int) (RenderedMessage, error) {
	return RenderedMessage{Body: []byte("{}"), ContentType: "application/json"}, nil
}
func (adapter processorAdapter) Send(context.Context, map[string]interface{}, string, string, RenderedMessage) error {
	return adapter.sendError
}

func TestDeliveryProcessorRequiresDependencies(t *testing.T) {
	if _, err := (DeliveryProcessor{}).RunOnce(context.Background()); !errors.Is(err, ErrNotReady) {
		t.Fatalf("expected not ready, got %v", err)
	}
}

func TestDeliveryProcessorCancelsDisabledChannel(t *testing.T) {
	delivery := &Delivery{ID: "delivery-1", ProjectID: "project-1", ChannelKey: "notification.generic_webhook"}
	store := &processorDeliveryStub{delivery: delivery}
	registry := NewAdapterRegistry()
	if err := registry.Register(processorAdapter{key: delivery.ChannelKey}); err != nil {
		t.Fatal(err)
	}
	processor := DeliveryProcessor{
		Adapters: registry, Deliveries: store, Owner: "processor-1", Settings: processorSettingsStub{resolved: settings.ResolvedSetting{Values: map[string]interface{}{"enabled": false}}},
	}
	worked, err := processor.RunOnce(context.Background())
	if err != nil || !worked || !store.cancelled || store.completed || store.failed {
		t.Fatalf("disabled channel result: worked=%t err=%v store=%#v", worked, err, store)
	}
}

func TestDeliveryProcessorClassifiesProviderRetry(t *testing.T) {
	delivery := &Delivery{ID: "delivery-2", ProjectID: "project-1", ChannelKey: "notification.generic_webhook", Attempts: 2}
	store := &processorDeliveryStub{delivery: delivery, notification: Notification{TypeKey: TypeReminderDue, TemplateVersion: 1}}
	registry := NewAdapterRegistry()
	providerError := ProviderError{Code: "provider_retryable", StatusCode: 429, Retryable: true, RetryAfter: 7 * time.Second}
	if err := registry.Register(processorAdapter{key: delivery.ChannelKey, sendError: providerError}); err != nil {
		t.Fatal(err)
	}
	processor := DeliveryProcessor{
		Adapters: registry, Deliveries: store, Owner: "processor-1", Settings: processorSettingsStub{resolved: settings.ResolvedSetting{Values: map[string]interface{}{"enabled": true}}},
	}
	worked, err := processor.RunOnce(context.Background())
	if err != nil || !worked || !store.failed || store.failureCode != providerError.Code || !store.failureRetry || store.failureDelay != providerError.RetryAfter {
		t.Fatalf("provider retry result: worked=%t err=%v store=%#v", worked, err, store)
	}
}

func TestDeliveryProcessorCompletesSuccessfulSend(t *testing.T) {
	delivery := &Delivery{ID: "delivery-3", ProjectID: "project-1", ChannelKey: "notification.generic_webhook"}
	store := &processorDeliveryStub{delivery: delivery, notification: Notification{TypeKey: TypeReminderDue, TemplateVersion: 1}}
	registry := NewAdapterRegistry()
	if err := registry.Register(processorAdapter{key: delivery.ChannelKey}); err != nil {
		t.Fatal(err)
	}
	processor := DeliveryProcessor{
		Adapters: registry, Deliveries: store, Owner: "processor-1", Settings: processorSettingsStub{resolved: settings.ResolvedSetting{Values: map[string]interface{}{"enabled": true}}},
	}
	worked, err := processor.RunOnce(context.Background())
	if err != nil || !worked || !store.completed || store.failed || store.cancelled {
		t.Fatalf("successful send result: worked=%t err=%v store=%#v", worked, err, store)
	}
}
