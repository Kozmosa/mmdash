package notification

import (
	"context"
	"errors"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type notificationProjectAccessStub struct {
	err error
}

func (stub notificationProjectAccessStub) Authorize(context.Context, auth.Identity, string, project.Permission) error {
	return stub.err
}

func TestInboxRejectsMachineIdentitiesBeforeStoreAccess(t *testing.T) {
	store := &notificationStoreStub{}
	service := Service{Store: store}
	for _, kind := range []string{"agent", "box"} {
		identity := auth.Identity{Kind: kind, User: auth.User{ID: "machine-1"}}
		if _, err := service.ListInbox(context.Background(), identity, Filter{}, pagination.Request{}); !errors.Is(err, project.ErrForbidden) {
			t.Fatalf("%s inbox access: got %v", kind, err)
		}
		if store.listInboxHit {
			t.Fatalf("%s reached the inbox store", kind)
		}
	}
}

func TestNotificationRuleEnforcesRegisteredTypeBoundaries(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	store := &notificationStoreStub{}
	service := Service{Registry: registry, Store: store, Access: notificationProjectAccessStub{}}
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "owner-1"}}
	projectID := "00000000-0000-4000-8000-000000000001"

	if _, err := service.UpsertRule(context.Background(), identity, Rule{
		ProjectID:       projectID,
		TypeKey:         TypeInvitationReceived,
		ExternalEnabled: true,
		ChannelKeys:     []string{"notification.generic_webhook"},
		MinimumPriority: "normal",
	}); err != nil {
		t.Fatalf("upsert invitation rule: %v", err)
	}
	if store.upsertedRule.ExternalEnabled || store.upsertedRule.ChannelKeys != nil || !store.upsertedRule.InboxEnabled {
		t.Fatalf("invitation rule escaped registry boundary: %#v", store.upsertedRule)
	}

	for _, invalid := range []Rule{
		{ProjectID: projectID, TypeKey: TypeReminderDue, ChannelKeys: []string{"notification.unknown"}, MinimumPriority: "normal"},
		{ProjectID: projectID, TypeKey: TypeReminderDue, MinimumPriority: "critical"},
	} {
		if _, err := service.UpsertRule(context.Background(), identity, invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid rule accepted: %#v -> %v", invalid, err)
		}
	}
}

func TestNotificationRuleHonorsProjectAuthorization(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	service := Service{Registry: registry, Store: &notificationStoreStub{}, Access: notificationProjectAccessStub{err: project.ErrForbidden}}
	_, err = service.GetRule(context.Background(), auth.Identity{Kind: "session", User: auth.User{ID: "viewer-1"}}, "project-1", TypeReminderDue)
	if !errors.Is(err, project.ErrForbidden) {
		t.Fatalf("unauthorized rule read: got %v", err)
	}
}
