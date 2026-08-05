package notification

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type notificationProjectAccessStub struct {
	err error
}

func (stub notificationProjectAccessStub) Authorize(context.Context, auth.Identity, string, project.Permission) error {
	return stub.err
}

type notificationAuthenticatorStub struct {
	identity auth.Identity
}

func (stub notificationAuthenticatorStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return stub.identity, nil
}

func TestMarkAllReadUsesOnlyJSONBodyFilters(t *testing.T) {
	const (
		projectID      = "00000000-0000-4000-8000-000000000001"
		otherProjectID = "00000000-0000-4000-8000-000000000003"
		userID         = "00000000-0000-4000-8000-000000000002"
	)
	tests := []struct {
		name string
		path string
		body string
		want Filter
	}{
		{
			name: "body project and type",
			path: "/v1/inbox/mark-all-read",
			body: `{"project_id":"` + projectID + `","type_key":"` + TypeReminderDue + `"}`,
			want: Filter{ProjectID: projectID, TypeKey: TypeReminderDue},
		},
		{
			name: "no body",
			path: "/v1/inbox/mark-all-read",
			want: Filter{},
		},
		{
			name: "empty object",
			path: "/v1/inbox/mark-all-read?project_id=" + otherProjectID + "&type_key=query.type",
			body: `{}`,
			want: Filter{},
		},
		{
			name: "query filters ignored",
			path: "/v1/inbox/mark-all-read?project_id=" + otherProjectID + "&type_key=query.type&read_state=unread&archived=false&outcome=active",
			want: Filter{},
		},
		{
			name: "body wins over conflicting query",
			path: "/v1/inbox/mark-all-read?project_id=" + otherProjectID + "&type_key=query.type&read_state=unread",
			body: `{"project_id":"` + projectID + `","type_key":"` + TypeReminderDue + `"}`,
			want: Filter{ProjectID: projectID, TypeKey: TypeReminderDue},
		},
		{
			name: "collection route uses the same body semantics",
			path: "/v1/inbox?action=mark-all-read&project_id=" + otherProjectID + "&type_key=query.type",
			body: `{"project_id":"` + projectID + `","type_key":"` + TypeReminderDue + `"}`,
			want: Filter{ProjectID: projectID, TypeKey: TypeReminderDue},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &notificationStoreStub{}
			module := Module{
				Auth: notificationAuthenticatorStub{identity: auth.Identity{
					Kind: "session",
					User: auth.User{ID: userID},
				}},
				Service: Service{Store: store},
			}
			mux := http.NewServeMux()
			module.RegisterRoutes(mux)
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()

			mux.ServeHTTP(response, request)

			if response.Code != http.StatusNoContent {
				t.Fatalf("mark all read: got %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
			}
			if !store.markAllReadCalled {
				t.Fatal("mark all read did not reach the store")
			}
			if store.markAllReadUserID != userID {
				t.Fatalf("mark all read user: got %q, want %q", store.markAllReadUserID, userID)
			}
			if store.markAllReadFilter != test.want {
				t.Fatalf("mark all read filter: got %#v, want %#v", store.markAllReadFilter, test.want)
			}
		})
	}
}

func TestInvitationLifecycleEventPassesDurableOutcomeFact(t *testing.T) {
	projectID := "00000000-0000-4000-8000-000000000001"
	invitationID := "00000000-0000-4000-8000-000000000002"
	eventID := "00000000-0000-4000-8000-000000000003"
	occurredAt := time.Date(2026, 8, 6, 1, 0, 0, 0, time.UTC)
	store := &notificationStoreStub{}
	service := Service{Store: store}

	err := service.HandleEvent(context.Background(), contract.EventEnvelope{
		EventID:       eventID,
		EventType:     "project.invitation.revoked",
		OccurredAt:    occurredAt,
		Payload:       map[string]interface{}{"invitation_id": invitationID, "project_id": projectID},
		ProjectID:     &projectID,
		SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("handle invitation outcome: %v", err)
	}
	want := InvitationOutcome{
		InvitationID:  invitationID,
		ProjectID:     projectID,
		Outcome:       OutcomeRevoked,
		SourceEventID: eventID,
		OccurredAt:    occurredAt,
	}
	if store.invitationOutcome != want {
		t.Fatalf("invitation outcome: got %#v, want %#v", store.invitationOutcome, want)
	}
}

func TestRetryDeliveryReturnsStableStatusConflict(t *testing.T) {
	store := &notificationStoreStub{retryErr: ErrDeliveryRetryConflict}
	module := Module{
		Auth: notificationAuthenticatorStub{identity: auth.Identity{
			Kind: "session",
			User: auth.User{ID: "user-1"},
		}},
		Service: Service{Store: store},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/projects/project-1/notification-deliveries/delivery-1/retry",
		strings.NewReader(`{"reason":"operator retry"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	module.ProjectHandler().ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("retry status conflict: got %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"NOTIFICATION_DELIVERY_RETRY_CONFLICT"`) {
		t.Fatalf("retry status conflict code: %s", response.Body.String())
	}
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
