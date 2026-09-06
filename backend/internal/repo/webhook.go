package repo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

const maximumWebhookBodyBytes int64 = 1024 * 1024

var webhookHeaderPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,200}$`)

// WebhookRequest retains the exact signed body only for one request lifetime.
type WebhookRequest struct {
	Body       []byte
	DeliveryID string
	Event      string
	HookID     string
	Signature  string
}

// WebhookAcceptance is safe to return to unauthenticated GitHub callers.
type WebhookAcceptance struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate"`
	Event     string `json:"event"`
}

// WebhookDelivery is the bounded metadata persisted for deduplication.
type WebhookDelivery struct {
	AfterSHA     *string
	BeforeSHA    *string
	DeliveryID   string
	Event        string
	PayloadSHA   string
	ReceivedAt   time.Time
	Ref          *string
	RepositoryID string
	RequestSync  bool
	Status       string
	Workspace    *WorkspaceKind
}

// WebhookStore atomically records delivery identity and coalesces push syncs.
type WebhookStore interface {
	RecordWebhook(context.Context, WebhookDelivery) (bool, error)
}

func (service Service) RotateWebhookSecret(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
) (Repository, error) {
	if err := service.Access.Authorize(
		ctx, caller, projectID, project.PermissionRepoManage,
	); err != nil {
		return Repository{}, err
	}
	repository, err := service.Store.GetByProject(ctx, projectID)
	if err != nil {
		return Repository{}, err
	}
	if repository.Provider != ProviderGitHub {
		return Repository{}, provider.ErrUnsupported
	}
	secret, err := newWebhookSecret()
	if err != nil {
		return Repository{}, err
	}
	if _, err := service.Settings.Update(
		ctx, caller, settings.ScopeProject, projectID, SettingType,
		map[string]interface{}{"webhook_secret": secret},
	); err != nil {
		return Repository{}, err
	}
	service.record(
		ctx, "repo.webhook.secret.rotated", projectID,
		repository.ID, "success", "",
	)
	return service.decorate(ctx, repository, secret), nil
}

func (service Service) AcceptGitHubWebhook(
	ctx context.Context,
	request WebhookRequest,
) (WebhookAcceptance, error) {
	request.HookID = strings.TrimSpace(request.HookID)
	request.DeliveryID = strings.TrimSpace(request.DeliveryID)
	request.Event = strings.TrimSpace(request.Event)
	if !webhookHeaderPattern.MatchString(request.HookID) ||
		!webhookHeaderPattern.MatchString(request.DeliveryID) ||
		!webhookHeaderPattern.MatchString(request.Event) ||
		len(request.Body) == 0 ||
		int64(len(request.Body)) > maximumWebhookBodyBytes ||
		!json.Valid(request.Body) {
		return WebhookAcceptance{}, ErrInvalid
	}
	repository, err := service.Store.GetByHook(ctx, request.HookID)
	if err != nil {
		return WebhookAcceptance{}, err
	}
	if repository.Provider != ProviderGitHub || service.Webhooks == nil {
		return WebhookAcceptance{}, ErrNotConfigured
	}
	resolved, err := service.Settings.Resolve(
		ctx, settings.ScopeProject, repository.ProjectID, SettingType,
	)
	if err != nil {
		return WebhookAcceptance{}, err
	}
	secret, _ := resolved.Values["webhook_secret"].(string)
	if !validWebhookSignature(request.Signature, secret, request.Body) {
		service.record(
			ctx, "repo.webhook.rejected", repository.ProjectID,
			repository.ID, "denied", "REPO_WEBHOOK_SIGNATURE_INVALID",
		)
		return WebhookAcceptance{}, ErrWebhookSignature
	}

	delivery := WebhookDelivery{
		DeliveryID: request.DeliveryID, Event: request.Event,
		ReceivedAt:   service.Clock.Now().UTC(),
		RepositoryID: repository.ID, Status: "ignored",
	}
	sum := sha256.Sum256(request.Body)
	delivery.PayloadSHA = hex.EncodeToString(sum[:])
	switch request.Event {
	case "ping":
		delivery.Status = "processed"
	case "push":
		var push struct {
			After  string `json:"after"`
			Before string `json:"before"`
			Ref    string `json:"ref"`
		}
		if err := json.Unmarshal(request.Body, &push); err != nil {
			return WebhookAcceptance{}, ErrInvalid
		}
		if !strings.HasPrefix(push.Ref, "refs/heads/") {
			return WebhookAcceptance{}, ErrInvalid
		}
		branch := strings.TrimPrefix(push.Ref, "refs/heads/")
		if gitcli.ValidateBranch(branch) != nil {
			return WebhookAcceptance{}, ErrInvalid
		}
		delivery.Ref = stringPointer(push.Ref)
		delivery.BeforeSHA, err = webhookSHA(push.Before)
		if err != nil {
			return WebhookAcceptance{}, err
		}
		delivery.AfterSHA, err = webhookSHA(push.After)
		if err != nil {
			return WebhookAcceptance{}, err
		}
		for _, workspace := range repository.Workspaces {
			if workspace.RemoteBranch == branch {
				delivery.RequestSync = true
				delivery.Status = "accepted"
				kind := workspace.Workspace
				delivery.Workspace = &kind
				break
			}
		}
	}
	duplicate, err := service.Webhooks.RecordWebhook(ctx, delivery)
	if err != nil {
		if service.WebhookError != nil {
			service.WebhookError(ctx, err)
		}
		return WebhookAcceptance{}, err
	}
	service.record(
		ctx, "repo.webhook.accepted", repository.ProjectID,
		repository.ID, "success", "",
	)
	return WebhookAcceptance{
		Accepted: true, Duplicate: duplicate, Event: request.Event,
	}, nil
}

func validWebhookSignature(signature, secret string, body []byte) bool {
	const prefix = "sha256="
	if secret == "" || !strings.HasPrefix(signature, prefix) {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, prefix))
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}

func webhookSHA(value string) (*string, error) {
	if value == strings.Repeat("0", 40) ||
		value == strings.Repeat("0", 64) {
		return nil, nil
	}
	if gitcli.ValidateFullSHA(value) != nil {
		return nil, ErrInvalid
	}
	return stringPointer(value), nil
}

func stringPointer(value string) *string {
	return &value
}
