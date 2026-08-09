package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

type notificationAuthStub struct{ identity auth.Identity }

func (stub notificationAuthStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return stub.identity, nil
}

type settingsAccessStub struct {
	getErr  error
	setting settings.Setting
}

func (stub settingsAccessStub) Get(context.Context, auth.Identity, settings.Scope, string, string) (settings.Setting, error) {
	return stub.setting, stub.getErr
}
func (stub settingsAccessStub) Update(context.Context, auth.Identity, settings.Scope, string, string, map[string]interface{}) (settings.Setting, error) {
	return settings.Setting{}, nil
}
func (stub settingsAccessStub) Delete(context.Context, auth.Identity, settings.Scope, string, string) error {
	return nil
}
func (stub settingsAccessStub) TestConnection(context.Context, auth.Identity, settings.Scope, string, string) (settings.ConnectionTestResult, error) {
	return settings.ConnectionTestResult{}, nil
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

func TestChannelGetTreatsUnconfiguredAsDisabled(t *testing.T) {
	module := Module{
		Auth:     notificationAuthStub{identity: auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}},
		Service:  Service{Store: &notificationStoreStub{}},
		Settings: settingsAccessStub{getErr: settings.ErrNotFound},
	}
	projectID := "00000000-0000-4000-8000-000000000002"
	request := httptest.NewRequest(http.MethodGet, "/v1/projects/"+projectID+"/notification-channels/notification.generic_webhook", nil)
	response := httptest.NewRecorder()

	module.handleProject(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unconfigured channel status: got %d body=%s", response.Code, response.Body.String())
	}
	var projected map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &projected); err != nil {
		t.Fatalf("decode channel projection: %v", err)
	}
	if projected["configured"] != false || projected["enabled"] != false {
		t.Fatalf("unconfigured channel projection: %#v", projected)
	}
}

func TestChannelGetReadsConfiguredState(t *testing.T) {
	module := Module{
		Auth: notificationAuthStub{identity: auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}},
		Service: Service{
			Store: &notificationStoreStub{},
		},
		Settings: settingsAccessStub{setting: settings.Setting{
			Values:  map[string]interface{}{"enabled": true},
			Version: 3,
		}},
	}
	projectID := "00000000-0000-4000-8000-000000000003"
	request := httptest.NewRequest(http.MethodGet, "/v1/projects/"+projectID+"/notification-channels/notification.generic_webhook", nil)
	response := httptest.NewRecorder()

	module.handleProject(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("configured channel status: got %d body=%s", response.Code, response.Body.String())
	}
	var projected map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &projected); err != nil {
		t.Fatalf("decode channel projection: %v", err)
	}
	if projected["configured"] != true || projected["enabled"] != true || projected["settings_version"] != float64(3) {
		t.Fatalf("configured channel projection: %#v", projected)
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
