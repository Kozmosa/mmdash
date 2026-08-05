package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
)

func TestRegistryRejectsUnsafeAndIncompleteDescriptors(t *testing.T) {
	registry := NewRegistry()
	base := Descriptor{
		TypeKey: "test.notification", SchemaVersion: 1,
		SourceEventTypes: []string{"progress.reminder.due"}, AcceptedEventSchemaVersions: []int64{1},
		Scope: "project", InboxPolicy: "default_on", Priority: "normal", TemplateKey: "test", TemplateVersion: 1,
		AllowedTemplateFields: []string{"title"}, RecipientResolver: "test", Renderer: "test",
	}
	if err := registry.Register(base); err != nil {
		t.Fatalf("register base descriptor: %v", err)
	}
	if err := registry.Register(base); err == nil {
		t.Fatal("duplicate descriptor was accepted")
	}
	unsafe := base
	unsafe.TypeKey = "test.unsafe"
	unsafe.AllowedTemplateFields = []string{"api_token"}
	if err := registry.Register(unsafe); err == nil {
		t.Fatal("unsafe template field was accepted")
	}
	missingVersion := base
	missingVersion.TypeKey = "test.missing-version"
	missingVersion.AcceptedEventSchemaVersions = nil
	if err := registry.Register(missingVersion); err == nil {
		t.Fatal("descriptor without accepted schema versions was accepted")
	}
	unknownEvent := base
	unknownEvent.TypeKey = "test.unknown-event"
	unknownEvent.SourceEventTypes = []string{"unknown.event"}
	if err := registry.Register(unknownEvent); err == nil {
		t.Fatal("unknown source event was accepted")
	}
}

type notificationStoreStub struct {
	created           []Notification
	invitationOutcome InvitationOutcome
	listInboxHit      bool
	markAllReadCalled bool
	markAllReadFilter Filter
	markAllReadUserID string
	upsertedRule      Rule
}

func (stub *notificationStoreStub) CreateEvent(_ context.Context, notification Notification, _ []RecipientInput, _ bool, _ []DeliveryIntent) error {
	stub.created = append(stub.created, notification)
	return nil
}
func (*notificationStoreStub) ClaimEmailRecipients(context.Context, string, string) error { return nil }
func (stub *notificationStoreStub) ApplyInvitationOutcome(_ context.Context, outcome InvitationOutcome) error {
	stub.invitationOutcome = outcome
	return nil
}
func (stub *notificationStoreStub) ListInbox(context.Context, string, Filter, pagination.Request) (Page, error) {
	stub.listInboxHit = true
	return Page{}, nil
}
func (*notificationStoreStub) GetInbox(context.Context, string, string) (InboxItem, error) {
	return InboxItem{}, nil
}
func (*notificationStoreStub) UpdateInbox(context.Context, string, string, *string, *bool) (InboxItem, error) {
	return InboxItem{}, nil
}
func (stub *notificationStoreStub) MarkAllRead(_ context.Context, userID string, filter Filter) error {
	stub.markAllReadCalled = true
	stub.markAllReadUserID = userID
	stub.markAllReadFilter = filter
	return nil
}
func (*notificationStoreStub) UnreadCount(context.Context, string, string) (int64, error) {
	return 0, nil
}
func (*notificationStoreStub) GetRule(context.Context, string, string) (Rule, error) {
	return Rule{InboxEnabled: true, MinimumPriority: "normal"}, nil
}
func (stub *notificationStoreStub) UpsertRule(_ context.Context, rule Rule) (Rule, error) {
	stub.upsertedRule = rule
	return rule, nil
}
func (*notificationStoreStub) ListDeliveries(context.Context, string, string, pagination.Request) (DeliveryPage, error) {
	return DeliveryPage{}, nil
}
func (*notificationStoreStub) CreateRetry(context.Context, string, string, string) (Delivery, error) {
	return Delivery{}, nil
}

func TestHandleEventCreatesEveryRegisteredTypeForOneEvent(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	extra := Descriptor{
		TypeKey: "progress.reminder.digest", SchemaVersion: 1,
		SourceEventTypes: []string{"progress.reminder.due"}, AcceptedEventSchemaVersions: []int64{1},
		Scope: "project", InboxPolicy: "default_on", Priority: "normal", TemplateKey: "digest", TemplateVersion: 1,
		AllowedTemplateFields: []string{"reminder_id"}, RecipientResolver: "test", Renderer: "test",
	}
	if err := registry.Register(extra); err != nil {
		t.Fatal(err)
	}
	store := &notificationStoreStub{}
	projectID := "00000000-0000-4000-8000-000000000001"
	service := Service{Registry: registry, Store: store, Generator: identity.Generator{}}
	err = service.HandleEvent(context.Background(), contract.EventEnvelope{
		EventID: "00000000-0000-4000-8000-000000000002", EventType: "progress.reminder.due",
		OccurredAt: time.Now().UTC(), Payload: map[string]interface{}{"reminder_id": "reminder-1"},
		ProjectID: &projectID, SchemaVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.created) != 2 {
		t.Fatalf("expected two notifications, got %d", len(store.created))
	}
}

func TestHandleEventRejectsInvalidSchemaVersion(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	projectID := "00000000-0000-4000-8000-000000000001"
	err = (Service{Registry: registry, Store: &notificationStoreStub{}, Generator: identity.Generator{}}).HandleEvent(context.Background(), contract.EventEnvelope{
		EventID: "00000000-0000-4000-8000-000000000002", EventType: "progress.reminder.due",
		OccurredAt: time.Now().UTC(), Payload: map[string]interface{}{"reminder_id": "reminder-1"},
		ProjectID: &projectID, SchemaVersion: 2,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected invalid schema error, got %v", err)
	}
}
