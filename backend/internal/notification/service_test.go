package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type notificationProjectAccessStub struct {
	err         error
	permissions *[]project.Permission
}

func (stub notificationProjectAccessStub) Authorize(_ context.Context, _ auth.Identity, _ string, permission project.Permission) error {
	if stub.permissions != nil {
		*stub.permissions = append(*stub.permissions, permission)
	}
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
	if store.upsertedRule.ExternalEnabled || store.upsertedRule.ChannelKeys != nil {
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

func TestNotificationSettingsAndDiagnosticsRequireManagePermission(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	permissions := []project.Permission{}
	service := Service{Registry: registry, Store: &notificationStoreStub{}, Access: notificationProjectAccessStub{permissions: &permissions}}
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "owner-1"}}
	if _, err := service.GetRule(context.Background(), identity, "project-1", TypeReminderDue); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListDeliveries(context.Background(), identity, "project-1", "", pagination.Request{}); err != nil {
		t.Fatal(err)
	}
	if len(permissions) != 2 || permissions[0] != project.PermissionSettingsManage || permissions[1] != project.PermissionSettingsManage {
		t.Fatalf("unexpected permissions: %#v", permissions)
	}
}

func TestInboxRejectsConflictingAndInvalidFilters(t *testing.T) {
	store := &notificationStoreStub{}
	service := Service{Store: store}
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}
	from := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	to := from.Add(-time.Hour)
	filters := []Filter{
		{ReadState: "unknown"},
		{Outcome: OutcomeActive, OutcomeGroup: "processed"},
		{OccurredFrom: &from, OccurredTo: &to},
	}
	for _, filter := range filters {
		if _, err := service.ListInbox(context.Background(), identity, filter, pagination.Request{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid filter accepted: %#v -> %v", filter, err)
		}
	}
	if store.listInboxHit {
		t.Fatal("invalid Inbox filter reached the store")
	}
}
