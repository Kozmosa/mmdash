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

func TestArticleReleaseNotificationIsRegisteredAndSafe(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	descriptor, ok := registry.Get(TypeArticleRelease)
	if !ok || descriptor.TypeKey != TypeArticleRelease || descriptor.InboxPolicy != "default_on" || len(descriptor.SourceEventTypes) != 1 || descriptor.SourceEventTypes[0] != "article.release.created" {
		t.Fatalf("release descriptor missing: %#v", descriptor)
	}
	projectID := "00000000-0000-4000-8000-000000000001"
	event := contract.EventEnvelope{EventType: "article.release.created", ProjectID: &projectID, Actor: map[string]string{"actor_id": "user-1"}, Payload: map[string]interface{}{"release_id": "release-1", "tag": "v1", "title": "Paper", "secret": "must-not-render"}}
	data := allowedData(event.Payload, descriptor.AllowedTemplateFields)
	snapshot := renderInboxSnapshot(TypeArticleRelease, data)
	if snapshot["title"] != "Paper" || snapshot["body"] == "" || data["secret"] != nil {
		t.Fatalf("unsafe release rendering: data=%#v snapshot=%#v", data, snapshot)
	}
	recipients := resolveRecipients(event, data)
	if len(recipients) != 1 || recipients[0].UserID != "user-1" {
		t.Fatalf("release recipient mismatch: %#v", recipients)
	}
}
