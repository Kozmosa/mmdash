package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

func TestNotionOAuthClientUsesBasicAuthAndRotatesTokens(t *testing.T) {
	requests := []map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		clientID, clientSecret, ok := request.BasicAuth()
		if !ok || clientID != "client-id" || clientSecret != "client-secret" {
			t.Fatalf("unexpected Basic authentication: %q/%q", clientID, clientSecret)
		}
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, body)
		if request.URL.Path == "/revoke" {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		if body["grant_type"] == "refresh_token" {
			_, _ = response.Write([]byte(`{"access_token":"access-2","refresh_token":"refresh-2"}`))
			return
		}
		_, _ = response.Write([]byte(`{"access_token":"access-1","refresh_token":"refresh-1","bot_id":"bot-1","workspace_id":"workspace-1","workspace_name":"Research"}`))
	}))
	defer server.Close()
	client := &NotionOAuthClient{
		ClientID: "client-id", ClientSecret: "client-secret", RedirectURI: "https://mmdash.example/api/integrations/notion/oauth/callback",
		TokenEndpoint: server.URL + "/token", RevokeEndpoint: server.URL + "/revoke", HTTPClient: server.Client(),
	}

	authorizationURL, err := client.AuthorizationURL("state-value")
	if err != nil {
		t.Fatalf("authorization URL: %v", err)
	}
	parsed, _ := url.Parse(authorizationURL)
	if parsed.Query().Get("owner") != "user" || parsed.Query().Get("response_type") != "code" || parsed.Query().Get("state") != "state-value" {
		t.Fatalf("unexpected authorization query: %s", parsed.RawQuery)
	}
	exchanged, err := client.Exchange(context.Background(), "one-time-code")
	if err != nil || exchanged.WorkspaceID != "workspace-1" {
		t.Fatalf("exchange = %#v, %v", exchanged, err)
	}
	refreshed, err := client.Refresh(context.Background(), exchanged.RefreshToken)
	if err != nil || refreshed.AccessToken != "access-2" || refreshed.RefreshToken != "refresh-2" {
		t.Fatalf("refresh = %#v, %v", refreshed, err)
	}
	if err := client.Revoke(context.Background(), refreshed.AccessToken); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if requests[0]["redirect_uri"] != client.RedirectURI || requests[1]["refresh_token"] != "refresh-1" || requests[2]["token"] != "access-2" {
		t.Fatalf("unexpected OAuth requests: %#v", requests)
	}
}

type oauthStoreStub struct {
	Store
	authorization NotionOAuthAuthorization
	completed     string
	disabled      bool
}

func (store *oauthStoreStub) DisableSource(context.Context, string, string, time.Time) error {
	store.disabled = true
	return nil
}

func (store *oauthStoreStub) CreateNotionOAuthAuthorization(_ context.Context, item NotionOAuthAuthorization) error {
	store.authorization = item
	return nil
}

func (store *oauthStoreStub) ClaimNotionOAuthAuthorization(_ context.Context, stateHash, userID string, now time.Time) (NotionOAuthAuthorization, error) {
	if stateHash != store.authorization.StateHash || userID != store.authorization.UserID || store.authorization.Status != "pending" || !store.authorization.ExpiresAt.After(now) {
		return NotionOAuthAuthorization{}, ErrConflict
	}
	store.authorization.Status = "exchanging"
	return store.authorization, nil
}

func (store *oauthStoreStub) CompleteNotionOAuthAuthorization(_ context.Context, _ string, status string, _ time.Time) error {
	store.completed = status
	return nil
}

type oauthAccessStub struct{}

func (oauthAccessStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return auth.Identity{}, nil
}
func (oauthAccessStub) Authorize(context.Context, auth.Identity, string, project.Permission) error {
	return nil
}

type oauthProviderStub struct {
	tokens  NotionOAuthTokens
	revoked string
}

func (*oauthProviderStub) Available() bool { return true }
func (*oauthProviderStub) AuthorizationURL(state string) (string, error) {
	return "https://api.notion.com/v1/oauth/authorize?state=" + url.QueryEscape(state), nil
}
func (stub *oauthProviderStub) Exchange(context.Context, string) (NotionOAuthTokens, error) {
	return stub.tokens, nil
}
func (stub *oauthProviderStub) Refresh(context.Context, string) (NotionOAuthTokens, error) {
	return stub.tokens, nil
}
func (stub *oauthProviderStub) Revoke(_ context.Context, token string) error {
	stub.revoked = token
	return nil
}

type oauthSettingsStub struct {
	deleted  bool
	resolved settings.ResolvedSetting
	rotated  map[string]string
	values   map[string]interface{}
}

func (stub *oauthSettingsStub) Resolve(context.Context, settings.Scope, string, string) (settings.ResolvedSetting, error) {
	return stub.resolved, nil
}

func (stub *oauthSettingsStub) Delete(context.Context, auth.Identity, settings.Scope, string, string) error {
	stub.deleted = true
	return nil
}

func (stub *oauthSettingsStub) RotateSecrets(_ context.Context, _ string, _ settings.Scope, _, _ string, secrets map[string]string) error {
	stub.rotated = secrets
	return nil
}
func (stub *oauthSettingsStub) Update(_ context.Context, _ auth.Identity, scope settings.Scope, scopeID, typeKey string, values map[string]interface{}) (settings.Setting, error) {
	stub.values = values
	return settings.Setting{Scope: scope, ScopeID: scopeID, TypeKey: typeKey}, nil
}

type oauthNotionStub struct {
	checkedToken string
	checkedPage  string
	err          error
}

func (stub *oauthNotionStub) Check(_ context.Context, token, pageID string) (string, error) {
	stub.checkedToken, stub.checkedPage = token, pageID
	return "Root", stub.err
}
func (*oauthNotionStub) Export(context.Context, string, NotionExportRequest) (NotionExport, error) {
	return NotionExport{}, errors.New("unused")
}

func TestNotionOAuthAuthorizationBindsStateUserProjectAndRootPage(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	store := &oauthStoreStub{}
	provider := &oauthProviderStub{tokens: NotionOAuthTokens{AccessToken: "access", RefreshToken: "refresh", BotID: "bot", WorkspaceID: "workspace", WorkspaceName: "Research"}}
	settingsStub := &oauthSettingsStub{}
	notion := &oauthNotionStub{}
	service := Service{
		Access: oauthAccessStub{}, Clock: clock.Fixed{Time: now}, Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		Notion: notion, OAuth: provider, OAuthSettings: settingsStub, Settings: settingsStub, Store: store,
	}
	caller := auth.Identity{Kind: "session", User: auth.User{ID: "00000000-0000-4000-8000-000000000002"}}
	projectID := "00000000-0000-4000-8000-000000000001"

	started, err := service.StartNotionOAuth(context.Background(), caller, projectID, StartNotionOAuthInput{
		RootPageURL: "https://nyaku.notion.site/3a4df00a545d801cae41e79dc52fbb51", AutoSyncEnabled: true, Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if settingsStub.values != nil {
		t.Fatalf("authorization start persisted credentials before callback: %#v", settingsStub.values)
	}
	parsed, _ := url.Parse(started.AuthorizationURL)
	state := parsed.Query().Get("state")
	if state == "" || store.authorization.StateHash != hashNotionOAuthState(state) || store.authorization.ProjectID != projectID || store.authorization.UserID != caller.User.ID {
		t.Fatalf("state binding was not persisted: %#v", store.authorization)
	}
	completed, err := service.CompleteNotionOAuth(context.Background(), caller, CompleteNotionOAuthInput{Code: "code", State: state})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.Status != "connected" || store.completed != "succeeded" || notion.checkedToken != "access" || notion.checkedPage != store.authorization.RootPageID {
		t.Fatalf("unexpected completion: %#v, status=%s check=%s/%s", completed, store.completed, notion.checkedToken, notion.checkedPage)
	}
	if settingsStub.values["integration_token"] != nil || settingsStub.values["access_token"] != "access" || settingsStub.values["root_page_url"] != store.authorization.RootPageURL {
		t.Fatalf("unexpected encrypted-setting patch inputs: %#v", settingsStub.values)
	}
}

func TestDisconnectNotionOAuthImmediatelyDisablesAutomaticSync(t *testing.T) {
	store := &oauthStoreStub{}
	provider := &oauthProviderStub{}
	settingsStub := &oauthSettingsStub{resolved: settings.ResolvedSetting{Values: map[string]interface{}{"access_token": "access-1"}}}
	service := Service{Access: oauthAccessStub{}, Clock: clock.Fixed{Time: time.Now()}, OAuth: provider, OAuthSettings: settingsStub, Settings: settingsStub, Store: store}
	caller := auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}

	if err := service.DisconnectNotionOAuth(context.Background(), caller, "project-1"); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if provider.revoked != "access-1" || !settingsStub.deleted || !store.disabled {
		t.Fatalf("disconnect did not revoke, delete, and disable: revoked=%q deleted=%t disabled=%t", provider.revoked, settingsStub.deleted, store.disabled)
	}
}

func TestRefreshNotionOAuthCredentialRotatesBothSecrets(t *testing.T) {
	settingsStub := &oauthSettingsStub{resolved: settings.ResolvedSetting{Values: map[string]interface{}{
		"access_token": "rejected-access", "refresh_token": "refresh-1",
	}}}
	provider := &oauthProviderStub{tokens: NotionOAuthTokens{AccessToken: "access-2", RefreshToken: "refresh-2"}}
	service := Service{OAuth: provider, OAuthSettings: settingsStub, Settings: settingsStub}

	accessToken, err := service.refreshNotionOAuthCredential(context.Background(), Source{ProjectID: "project-1", UpdatedBy: "user-1"}, "rejected-access")
	if err != nil {
		t.Fatalf("refresh credential: %v", err)
	}
	if accessToken != "access-2" || settingsStub.rotated["access_token"] != "access-2" || settingsStub.rotated["refresh_token"] != "refresh-2" {
		t.Fatalf("unexpected rotation: %q %#v", accessToken, settingsStub.rotated)
	}
}
