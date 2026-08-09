package notification

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/auth"
)

type notificationAuthStub struct{ identity auth.Identity }

func (stub notificationAuthStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return stub.identity, nil
}

func TestMarkAllReadUsesTheContractBodyScope(t *testing.T) {
	store := &notificationStoreStub{}
	module := Module{
		Auth:    notificationAuthStub{identity: auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}},
		Service: Service{Store: store},
	}
	projectID := "00000000-0000-4000-8000-000000000001"
	request := httptest.NewRequest(http.MethodPost, "/v1/inbox/mark-all-read", strings.NewReader(`{"project_id":"`+projectID+`","type_key":"progress.reminder.due"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	module.handleInbox(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("mark all read status: got %d body=%s", response.Code, response.Body.String())
	}
	if store.markAllFilter.ProjectID != projectID || store.markAllFilter.TypeKey != TypeReminderDue {
		t.Fatalf("mark all read scope was not decoded: %#v", store.markAllFilter)
	}
}

func TestInboxCollectionRejectsTheLegacyQueryAction(t *testing.T) {
	store := &notificationStoreStub{}
	module := Module{
		Auth:    notificationAuthStub{identity: auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}},
		Service: Service{Store: store},
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/inbox?action=mark-all-read", nil)
	response := httptest.NewRecorder()

	module.handleInboxCollection(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("legacy action status: got %d body=%s", response.Code, response.Body.String())
	}
}
