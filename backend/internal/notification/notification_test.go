package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
)

type adapterTestStub struct {
	intent Intent
	err    error
}

func (stub *adapterTestStub) Accept(_ context.Context, intent Intent) error {
	stub.intent = intent
	return stub.err
}

func TestReminderDueUsesNotificationAdapterBoundary(t *testing.T) {
	adapter := &adapterTestStub{}
	service := Service{Adapter: adapter, Generator: identity.Generator{}}
	projectID := "00000000-0000-4000-8000-000000000002"
	event := contract.EventEnvelope{
		EventID:       "00000000-0000-4000-8000-000000000003",
		EventType:     "progress.reminder.due",
		OccurredAt:    time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
		Payload:       map[string]interface{}{"reminder_id": "00000000-0000-4000-8000-000000000004"},
		ProjectID:     &projectID,
		Producer:      "progress",
		SchemaVersion: 1,
	}
	if err := service.HandleReminderDue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if adapter.intent.SourceEventID != event.EventID || adapter.intent.ProjectID != projectID || adapter.intent.ReminderID == "" || adapter.intent.Status != "accepted" {
		t.Fatalf("unexpected notification intent: %#v", adapter.intent)
	}
}

func TestReminderDuePropagatesAdapterFailure(t *testing.T) {
	want := errors.New("adapter unavailable")
	service := Service{Adapter: &adapterTestStub{err: want}, Generator: identity.Generator{}}
	projectID := "00000000-0000-4000-8000-000000000002"
	event := contract.EventEnvelope{EventID: "00000000-0000-4000-8000-000000000003", EventType: "progress.reminder.due", OccurredAt: time.Now(), Payload: map[string]interface{}{"reminder_id": "00000000-0000-4000-8000-000000000004"}, ProjectID: &projectID}
	if !errors.Is(service.HandleReminderDue(context.Background(), event), want) {
		t.Fatalf("adapter failure was not returned")
	}
}
