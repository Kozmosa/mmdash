package notification

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

type SettingsAccess interface {
	Get(context.Context, auth.Identity, settings.Scope, string, string) (settings.Setting, error)
	Update(context.Context, auth.Identity, settings.Scope, string, string, map[string]interface{}) (settings.Setting, error)
	Delete(context.Context, auth.Identity, settings.Scope, string, string) error
	TestConnection(context.Context, auth.Identity, settings.Scope, string, string) (settings.ConnectionTestResult, error)
}

type Module struct {
	Auth     Authenticator
	Service  Service
	Settings SettingsAccess
}

func (Module) Name() string { return "notification" }
func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/inbox", module.handleInboxCollection)
	mux.HandleFunc("/v1/inbox/", module.handleInbox)
}
func (module Module) ProjectHandler() http.Handler { return http.HandlerFunc(module.handleProject) }

func (module Module) handleInboxCollection(w http.ResponseWriter, r *http.Request) {
	identity, ok := module.identity(w, r)
	if !ok {
		return
	}
	if r.URL.Path != "/v1/inbox" {
		module.handleInbox(w, r)
		return
	}
	if r.Method == http.MethodPost {
		if r.URL.Query().Get("action") != "mark-all-read" {
			writeError(w, r, ErrInvalid)
			return
		}
		filter, ok := markAllReadFilter(w, r)
		if !ok {
			return
		}
		if err := module.Service.MarkAllRead(r.Context(), identity, filter); err != nil {
			writeError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !httpx.RequireMethod(w, r, http.MethodGet) {
		return
	}
	page, ok := notificationPage(r)
	if !ok {
		return
	}
	result, err := module.Service.ListInbox(r.Context(), identity, filterFromRequest(r), page)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (module Module) handleInbox(w http.ResponseWriter, r *http.Request) {
	identity, ok := module.identity(w, r)
	if !ok {
		return
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/inbox/"), "/"), "/")
	if len(segments) == 1 && segments[0] == "mark-all-read" {
		if !httpx.RequireMethod(w, r, http.MethodPost) {
			return
		}
		filter, ok := markAllReadFilter(w, r)
		if !ok {
			return
		}
		if err := module.Service.MarkAllRead(r.Context(), identity, filter); err != nil {
			writeError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(segments) == 1 && segments[0] == "unread-count" {
		if !httpx.RequireMethod(w, r, http.MethodGet) {
			return
		}
		count, err := module.Service.UnreadCount(r.Context(), identity, r.URL.Query().Get("project_id"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"count": count})
		return
	}
	if len(segments) != 1 || segments[0] == "" {
		writeError(w, r, ErrNotFound)
		return
	}
	id, err := url.PathUnescape(segments[0])
	if err != nil {
		writeError(w, r, ErrNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := module.Service.GetInbox(r.Context(), identity, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		var body struct {
			ReadState *string `json:"read_state"`
			Archived  *bool   `json:"archived"`
		}
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		item, err := module.Service.UpdateInbox(r.Context(), identity, id, body.ReadState, body.Archived)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	default:
		writeError(w, r, apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"))
	}
}

func (module Module) handleProject(w http.ResponseWriter, r *http.Request) {
	identity, ok := module.identity(w, r)
	if !ok {
		return
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/projects/"), "/"), "/")
	if len(segments) < 2 {
		writeError(w, r, ErrNotFound)
		return
	}
	projectID := segments[0]
	kind := segments[1]
	switch kind {
	case "notification-channels":
		module.handleChannel(w, r, identity, projectID, segments[2:])
	case "notification-rules":
		module.handleRule(w, r, identity, projectID, segments[2:])
	case "notification-deliveries":
		module.handleDelivery(w, r, identity, projectID, segments[2:])
	default:
		writeError(w, r, ErrNotFound)
	}
}

func (module Module) handleChannel(w http.ResponseWriter, r *http.Request, identity auth.Identity, projectID string, segments []string) {
	if module.Settings == nil || len(segments) < 1 || len(segments) > 2 {
		writeError(w, r, ErrNotFound)
		return
	}
	key, err := url.PathUnescape(segments[0])
	if err != nil {
		writeError(w, r, ErrNotFound)
		return
	}
	if len(segments) == 2 && segments[1] == "test" {
		if !httpx.RequireMethod(w, r, http.MethodPost) {
			return
		}
		result, err := module.Settings.TestConnection(r.Context(), identity, settings.ScopeProject, projectID, key)
		if err != nil {
			writeError(w, r, err)
			return
		}
		module.recordChannelAudit(r.Context(), identity, "notification.channel.tested", projectID, key, "success")
		httpx.WriteJSON(w, http.StatusOK, result)
		return
	}
	switch r.Method {
	case http.MethodGet:
		setting, err := module.Settings.Get(r.Context(), identity, settings.ScopeProject, projectID, key)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, channelProjection(key, setting))
	case http.MethodPatch:
		var body struct {
			Values map[string]interface{} `json:"values"`
		}
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		setting, err := module.Settings.Update(r.Context(), identity, settings.ScopeProject, projectID, key, body.Values)
		if err != nil {
			writeError(w, r, err)
			return
		}
		module.recordChannelAudit(r.Context(), identity, "notification.channel.updated", projectID, key, "success")
		httpx.WriteJSON(w, http.StatusOK, channelProjection(key, setting))
	case http.MethodDelete:
		if err := module.Settings.Delete(r.Context(), identity, settings.ScopeProject, projectID, key); err != nil {
			writeError(w, r, err)
			return
		}
		module.recordChannelAudit(r.Context(), identity, "notification.channel.deleted", projectID, key, "success")
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, r, apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"))
	}
}
func channelProjection(key string, setting settings.Setting) map[string]interface{} {
	return map[string]interface{}{"channel_key": key, "enabled": setting.Values["enabled"] == true, "configured": len(setting.Values) > 0, "settings_version": setting.Version}
}

func (module Module) handleRule(w http.ResponseWriter, r *http.Request, identity auth.Identity, projectID string, segments []string) {
	if len(segments) != 1 {
		writeError(w, r, ErrNotFound)
		return
	}
	typeKey, err := url.PathUnescape(segments[0])
	if err != nil {
		writeError(w, r, ErrNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		rule, err := module.Service.GetRule(r.Context(), identity, projectID, typeKey)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, rule)
	case http.MethodPut:
		var body contract.UpdateNotificationRuleRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		channelKeys := []string(nil)
		if body.ChannelKeys != nil {
			channelKeys = *body.ChannelKeys
		}
		minimumPriority := ""
		if body.MinimumPriority != nil {
			minimumPriority = *body.MinimumPriority
		}
		rule, err := module.Service.UpsertRule(r.Context(), identity, Rule{ProjectID: projectID, TypeKey: typeKey, InboxEnabled: body.InboxEnabled, ExternalEnabled: body.ExternalEnabled, ChannelKeys: channelKeys, MinimumPriority: minimumPriority, Version: body.Version})
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, rule)
	default:
		writeError(w, r, apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"))
	}
}

func (module Module) handleDelivery(w http.ResponseWriter, r *http.Request, identity auth.Identity, projectID string, segments []string) {
	if len(segments) > 1 {
		if len(segments) != 2 || segments[1] != "retry" || r.Method != http.MethodPost {
			writeError(w, r, ErrNotFound)
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		delivery, err := module.Service.RetryDelivery(r.Context(), identity, projectID, segments[0], body.Reason)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, delivery)
		return
	}
	if !httpx.RequireMethod(w, r, http.MethodGet) {
		return
	}
	page, ok := notificationPage(r)
	if !ok {
		return
	}
	result, err := module.Service.ListDeliveries(r.Context(), identity, projectID, r.URL.Query().Get("channel_key"), page)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (module Module) identity(w http.ResponseWriter, r *http.Request) (auth.Identity, bool) {
	if module.Auth == nil {
		writeError(w, r, auth.ErrUnauthenticated)
		return auth.Identity{}, false
	}
	identity, err := module.Auth.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, r, err)
		return auth.Identity{}, false
	}
	return identity, true
}

func (module Module) recordChannelAudit(ctx context.Context, identity auth.Identity, action, projectID, channelKey, outcome string) {
	if module.Service.Audit == nil {
		return
	}
	_ = module.Service.Audit.Record(ctx, audit.Event{
		Action: action, ActorID: identity.User.ID, ActorKind: identity.Kind,
		Category: "notification", Metadata: map[string]interface{}{"channel_key": channelKey},
		OccurredAt: module.Service.now(), Outcome: outcome, ProjectID: projectID,
		ResourceID: channelKey, ResourceType: "notification-channel", Source: "notification",
		RecordedAt: module.Service.now(),
	})
}
func notificationPage(r *http.Request) (pagination.Request, bool) {
	limit := pagination.DefaultLimit
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return pagination.Request{Limit: -1}, true
		}
		limit = parsed
	}
	return pagination.Request{Cursor: r.URL.Query().Get("cursor"), Limit: limit}, true
}
func filterFromRequest(r *http.Request) Filter {
	return Filter{ProjectID: r.URL.Query().Get("project_id"), TypeKey: r.URL.Query().Get("type_key"), ReadState: r.URL.Query().Get("read_state"), Archived: r.URL.Query().Get("archived"), Outcome: r.URL.Query().Get("outcome")}
}
func markAllReadFilter(w http.ResponseWriter, r *http.Request) (Filter, bool) {
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return Filter{}, true
	}
	var body contract.MarkAllInboxReadRequest
	if !httpx.DecodeJSON(w, r, &body) {
		return Filter{}, false
	}
	filter := Filter{}
	if body.ProjectID != nil {
		filter.ProjectID = *body.ProjectID
	}
	if body.TypeKey != nil {
		filter.TypeKey = *body.TypeKey
	}
	return filter, true
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	if w == nil {
		return
	}
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		httpx.WriteError(w, r, appErr)
		return
	}
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		httpx.WriteError(w, r, apperror.New(http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required"))
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, apperror.New(http.StatusNotFound, "NOT_FOUND", "Notification not found"))
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, r, apperror.New(http.StatusBadRequest, "INVALID_REQUEST", "Notification input is invalid"))
	case errors.Is(err, ErrConflict):
		httpx.WriteError(w, r, apperror.New(http.StatusConflict, "NOTIFICATION_RULE_CONFLICT", "Notification rule was changed by another request"))
	case errors.Is(err, project.ErrForbidden):
		httpx.WriteError(w, r, apperror.New(http.StatusForbidden, "FORBIDDEN", "Notification permission denied"))
	case errors.Is(err, settings.ErrForbidden):
		httpx.WriteError(w, r, apperror.New(http.StatusForbidden, "FORBIDDEN", "Notification channel permission denied"))
	case errors.Is(err, settings.ErrNotFound), errors.Is(err, settings.ErrTypeNotFound):
		httpx.WriteError(w, r, apperror.New(http.StatusNotFound, "NOT_FOUND", "Notification channel not found"))
	default:
		httpx.WriteError(w, r, err)
	}
}
