package notification

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

type webhookSettingsAccess struct{}

func (webhookSettingsAccess) Authorize(context.Context, auth.Identity, settings.Scope, string, bool) error {
	return nil
}

type webhookSettingsStore struct {
	setting settings.StoredSetting
	upserts int
}

func (store *webhookSettingsStore) Get(_ context.Context, scope settings.Scope, scopeID, typeKey string) (settings.StoredSetting, error) {
	if store.setting.TypeKey == "" || store.setting.Scope != scope || store.setting.ScopeID != scopeID || store.setting.TypeKey != typeKey {
		return settings.StoredSetting{}, settings.ErrNotFound
	}
	return store.setting, nil
}

func (store *webhookSettingsStore) Upsert(_ context.Context, actorID string, setting settings.StoredSetting) (settings.StoredSetting, error) {
	store.upserts++
	setting.UpdatedBy = actorID
	setting.Version++
	store.setting = setting
	return setting, nil
}

func (*webhookSettingsStore) Delete(context.Context, settings.Scope, string, string, string) error {
	return nil
}

func TestSettingTesterUsesWebhookURLPolicyWithoutLeakingConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		local       bool
		wantPassed  bool
		wantRequest bool
	}{
		{name: "production rejects http", endpoint: "http://127.0.0.1:8080/hook"},
		{name: "local rejects non-loopback http", endpoint: "http://example.test/hook", local: true},
		{name: "local allows loopback http", endpoint: "http://localhost:8080/hook", local: true, wantPassed: true, wantRequest: true},
		{name: "production allows https", endpoint: "https://example.test/hook", wantPassed: true, wantRequest: true},
		{name: "credentials are rejected safely", endpoint: "https://user:connection-secret@example.test/hook"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requested := false
			adapter := GenericWebhook{
				AllowHTTPLoopback: test.local,
				Client: httpDoerFunc(func(*http.Request) (*http.Response, error) {
					requested = true
					return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
				}),
			}
			checks, err := (SettingTester{Adapter: adapter}).Test(context.Background(), settings.ResolvedSetting{
				TypeKey: adapter.Key(),
				Values: map[string]interface{}{
					"endpoint": test.endpoint, "signing_secret": "signing-secret-that-must-not-leak",
				},
			})
			if err != nil {
				t.Fatalf("test setting: %v", err)
			}
			passed := len(checks) == 2 && checks[0].Status == "passed" && checks[1].Status == "passed"
			if passed != test.wantPassed || requested != test.wantRequest {
				t.Fatalf("checks=%#v requested=%t, want passed=%t requested=%t", checks, requested, test.wantPassed, test.wantRequest)
			}
			for _, check := range checks {
				if strings.Contains(check.Message, "connection-secret") || strings.Contains(check.Message, "signing-secret") {
					t.Fatalf("connection check leaked configuration: %#v", checks)
				}
			}
		})
	}
}

func TestWebhookSettingsSaveUsesAdapterPolicyAndExistingSecret(t *testing.T) {
	adapter := GenericWebhook{}
	registry := settings.NewRegistry()
	if err := registry.Register(settings.TypeDefinition{
		Description: "Generic Webhook test setting",
		Fields: []settings.FieldDefinition{
			{Key: "enabled", Kind: settings.FieldBoolean, Label: "Enabled", Required: true},
			{Key: "endpoint", Kind: settings.FieldURL, Label: "Endpoint", Required: true},
			{Key: "signing_secret", Kind: settings.FieldSecret, Label: "Signing secret", Required: true},
		},
		Key: "notification.generic_webhook", Order: 1, Owner: "notification",
		Scopes: []settings.Scope{settings.ScopeProject}, Title: "Generic Webhook", Validator: adapter,
	}); err != nil {
		t.Fatalf("register setting: %v", err)
	}
	codec, err := settings.NewSecretCodec("notification-settings-test-key-with-32-characters")
	if err != nil {
		t.Fatalf("create codec: %v", err)
	}
	store := &webhookSettingsStore{}
	service := settings.Service{
		Access: webhookSettingsAccess{}, Clock: clock.Fixed{Time: time.Now().UTC()},
		Codec: codec, Registry: registry, Store: store,
	}
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}
	credential := "save-path-signing-credential"
	_, err = service.Update(context.Background(), identity, settings.ScopeProject, "project-1", adapter.Key(), map[string]interface{}{
		"enabled": true, "endpoint": "http://127.0.0.1:8080/hook", "signing_secret": credential,
	})
	if !errors.Is(err, settings.ErrInvalid) || store.upserts != 0 {
		t.Fatalf("insecure production setting: err=%v upserts=%d", err, store.upserts)
	}
	if strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("save validation leaked configuration: %v", err)
	}

	if _, err := service.Update(context.Background(), identity, settings.ScopeProject, "project-1", adapter.Key(), map[string]interface{}{
		"enabled": true, "endpoint": "https://example.test/hook?provider=allowed", "signing_secret": credential,
	}); err != nil {
		t.Fatalf("save HTTPS setting: %v", err)
	}
	if _, err := service.Update(context.Background(), identity, settings.ScopeProject, "project-1", adapter.Key(), map[string]interface{}{
		"endpoint": "https://second.example.test/hook", "signing_secret": settings.RedactedSecret,
	}); err != nil {
		t.Fatalf("validate partial update with existing secret: %v", err)
	}
	if store.upserts != 2 {
		t.Fatalf("unexpected successful upserts: %d", store.upserts)
	}
}
