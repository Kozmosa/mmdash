package repo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

type webhookStoreStub struct {
	deliveries []WebhookDelivery
	err        error
}

func (store *webhookStoreStub) RecordWebhook(
	_ context.Context,
	delivery WebhookDelivery,
) (bool, error) {
	if store.err != nil {
		return false, store.err
	}
	for _, existing := range store.deliveries {
		if existing.DeliveryID == delivery.DeliveryID {
			return true, nil
		}
	}
	store.deliveries = append(store.deliveries, delivery)
	return false, nil
}

func TestServiceAcceptsSignedMappedWebhookAndDeduplicates(t *testing.T) {
	const secret = "webhook-test-secret"
	body := []byte(`{
		"ref":"refs/heads/main",
		"before":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"after":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	}`)
	repository := Repository{
		ID:        "00000000-0000-4000-8000-000000000011",
		ProjectID: "00000000-0000-4000-8000-000000000010",
		Provider:  ProviderGitHub,
		Webhook: Webhook{
			HookID: "00000000-0000-4000-8000-000000000012",
		},
		Workspaces: []Workspace{{
			RemoteBranch: "main", Workspace: WorkspaceCode,
		}},
	}
	store := &webhookStoreStub{}
	service := Service{
		Clock: clock.Fixed{
			Time: time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC),
		},
		Settings: &serviceSettings{resolved: settings.ResolvedSetting{
			Values: map[string]interface{}{"webhook_secret": secret},
		}},
		Store: &serviceStore{value: repository}, Webhooks: store,
	}
	request := WebhookRequest{
		Body: body, DeliveryID: "delivery-1", Event: "push",
		HookID:    repository.Webhook.HookID,
		Signature: webhookTestSignature(secret, body),
	}
	accepted, err := service.AcceptGitHubWebhook(
		context.Background(), request,
	)
	if err != nil {
		t.Fatalf("accept signed webhook: %v", err)
	}
	if !accepted.Accepted || accepted.Duplicate ||
		len(store.deliveries) != 1 {
		t.Fatalf("unexpected acceptance: %#v %#v", accepted, store.deliveries)
	}
	delivery := store.deliveries[0]
	if !delivery.RequestSync || delivery.Status != "accepted" ||
		delivery.Ref == nil || *delivery.Ref != "refs/heads/main" ||
		delivery.BeforeSHA == nil || delivery.AfterSHA == nil ||
		delivery.Workspace == nil || *delivery.Workspace != WorkspaceCode {
		t.Fatalf("mapped push metadata is incomplete: %#v", delivery)
	}
	replayed, err := service.AcceptGitHubWebhook(
		context.Background(), request,
	)
	if err != nil || !replayed.Duplicate || len(store.deliveries) != 1 {
		t.Fatalf(
			"delivery replay was not idempotent: %#v %v",
			replayed, err,
		)
	}
}

func TestServiceRejectsBadWebhookSignatureBeforePersistence(t *testing.T) {
	body := []byte(`{"zen":"safe"}`)
	repository := Repository{
		ID:        "00000000-0000-4000-8000-000000000011",
		ProjectID: "00000000-0000-4000-8000-000000000010",
		Provider:  ProviderGitHub,
		Webhook: Webhook{
			HookID: "00000000-0000-4000-8000-000000000012",
		},
	}
	store := &webhookStoreStub{}
	service := Service{
		Clock: clock.Fixed{Time: time.Now()},
		Settings: &serviceSettings{resolved: settings.ResolvedSetting{
			Values: map[string]interface{}{"webhook_secret": "expected"},
		}},
		Store: &serviceStore{value: repository}, Webhooks: store,
	}
	_, err := service.AcceptGitHubWebhook(
		context.Background(), WebhookRequest{
			Body: body, DeliveryID: "delivery-1", Event: "ping",
			HookID:    repository.Webhook.HookID,
			Signature: webhookTestSignature("wrong", body),
		},
	)
	if err != ErrWebhookSignature {
		t.Fatalf("expected signature failure, got %v", err)
	}
	if len(store.deliveries) != 0 {
		t.Fatalf("rejected webhook was persisted: %#v", store.deliveries)
	}
}

func TestServiceReportsWebhookPersistenceFailure(t *testing.T) {
	const secret = "webhook-test-secret"
	body := []byte(`{"zen":"safe"}`)
	repository := Repository{
		ID:        "00000000-0000-4000-8000-000000000011",
		ProjectID: "00000000-0000-4000-8000-000000000010",
		Provider:  ProviderGitHub,
		Webhook: Webhook{
			HookID: "00000000-0000-4000-8000-000000000012",
		},
	}
	expected := errors.New("webhook persistence unavailable")
	store := &webhookStoreStub{err: expected}
	var observed error
	service := Service{
		Clock: clock.Fixed{Time: time.Now()},
		Settings: &serviceSettings{resolved: settings.ResolvedSetting{
			Values: map[string]interface{}{"webhook_secret": secret},
		}},
		Store: &serviceStore{value: repository}, Webhooks: store,
		WebhookError: func(_ context.Context, err error) {
			observed = err
		},
	}
	_, err := service.AcceptGitHubWebhook(context.Background(), WebhookRequest{
		Body: body, DeliveryID: "delivery-1", Event: "ping",
		HookID: repository.Webhook.HookID, Signature: webhookTestSignature(secret, body),
	})
	if !errors.Is(err, expected) || !errors.Is(observed, expected) {
		t.Fatalf("persistence failure err=%v observed=%v", err, observed)
	}
}

func TestServiceIgnoresUnmappedAndUnknownWebhookEvents(t *testing.T) {
	const secret = "webhook-test-secret"
	repository := Repository{
		ID:        "00000000-0000-4000-8000-000000000011",
		ProjectID: "00000000-0000-4000-8000-000000000010",
		Provider:  ProviderGitHub,
		Webhook: Webhook{
			HookID: "00000000-0000-4000-8000-000000000012",
		},
		Workspaces: []Workspace{{
			RemoteBranch: "main", Workspace: WorkspaceCode,
		}},
	}
	store := &webhookStoreStub{}
	service := Service{
		Clock: clock.Fixed{Time: time.Now()},
		Settings: &serviceSettings{resolved: settings.ResolvedSetting{
			Values: map[string]interface{}{"webhook_secret": secret},
		}},
		Store: &serviceStore{value: repository}, Webhooks: store,
	}
	for _, request := range []WebhookRequest{
		{
			Body: []byte(
				`{"ref":"refs/heads/other","before":"` +
					strings.Repeat("0", 40) + `","after":"` +
					strings.Repeat("a", 40) + `"}`,
			),
			DeliveryID: "unmapped", Event: "push",
			HookID: repository.Webhook.HookID,
		},
		{
			Body:       []byte(`{"action":"opened"}`),
			DeliveryID: "unknown", Event: "issues",
			HookID: repository.Webhook.HookID,
		},
	} {
		request.Signature = webhookTestSignature(secret, request.Body)
		if _, err := service.AcceptGitHubWebhook(
			context.Background(), request,
		); err != nil {
			t.Fatalf("accept ignored event: %v", err)
		}
	}
	if len(store.deliveries) != 2 {
		t.Fatalf("ignored deliveries missing: %#v", store.deliveries)
	}
	for _, delivery := range store.deliveries {
		if delivery.RequestSync || delivery.Status != "ignored" {
			t.Fatalf("ignored event requested sync: %#v", delivery)
		}
	}
}

func TestServiceRotatesWebhookSecretOnlyForManager(t *testing.T) {
	access := &serviceAccess{}
	repository := Repository{
		ID: "repository-1", ProjectID: "project-1",
		Provider: ProviderGitHub,
	}
	settingSource := &serviceSettings{resolved: settings.ResolvedSetting{
		Values: map[string]interface{}{
			"webhook_secret": "old-secret",
		},
	}}
	service := Service{
		Access: access, Settings: settingSource,
		Store: &serviceStore{value: repository},
	}
	rotated, err := service.RotateWebhookSecret(
		context.Background(),
		auth.Identity{User: auth.User{ID: "user-1"}},
		"project-1",
	)
	if err != nil {
		t.Fatalf("rotate webhook secret: %v", err)
	}
	if access.permission != project.PermissionRepoManage ||
		rotated.Webhook.Secret == "" ||
		rotated.Webhook.Secret == "old-secret" ||
		!rotated.Webhook.SecretConfigured {
		t.Fatalf("unexpected rotated repository: %#v", rotated)
	}
}

func TestModuleWebhookPreservesSignedBodyAndEnforcesLimit(t *testing.T) {
	const secret = "webhook-test-secret"
	body := []byte(`{"zen":"safe"}`)
	repository := Repository{
		ID:        "00000000-0000-4000-8000-000000000011",
		ProjectID: "00000000-0000-4000-8000-000000000010",
		Provider:  ProviderGitHub,
		Webhook: Webhook{
			HookID: "00000000-0000-4000-8000-000000000012",
		},
	}
	module := Module{Service: Service{
		Clock: clock.Fixed{Time: time.Now()},
		Settings: &serviceSettings{resolved: settings.ResolvedSetting{
			Values: map[string]interface{}{"webhook_secret": secret},
		}},
		Store:    &serviceStore{value: repository},
		Webhooks: &webhookStoreStub{},
	}}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/repo/webhooks/github/"+repository.Webhook.HookID,
		strings.NewReader(string(body)),
	)
	request.Header.Set("X-GitHub-Delivery", "delivery-1")
	request.Header.Set("X-GitHub-Event", "ping")
	request.Header.Set(
		"X-Hub-Signature-256", webhookTestSignature(secret, body),
	)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted ||
		!strings.Contains(response.Body.String(), `"accepted":true`) {
		t.Fatalf(
			"unexpected webhook response %d: %s",
			response.Code, response.Body.String(),
		)
	}

	oversized := httptest.NewRequest(
		http.MethodPost,
		"/v1/repo/webhooks/github/"+repository.Webhook.HookID,
		strings.NewReader(strings.Repeat("x", int(maximumWebhookBodyBytes)+1)),
	)
	oversizedResponse := httptest.NewRecorder()
	mux.ServeHTTP(oversizedResponse, oversized)
	if oversizedResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf(
			"oversized webhook returned %d: %s",
			oversizedResponse.Code, oversizedResponse.Body.String(),
		)
	}
}

func webhookTestSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
