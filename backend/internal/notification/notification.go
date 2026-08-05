// Package notification owns the durable internal inbox and project-scoped
// external notification delivery boundary. Source modules publish events; they
// never write these tables or call a provider directly.
package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

const (
	TypeInvitationReceived = "project.invitation.received"
	TypeReminderDue        = "progress.reminder.due"
	OutcomeActive          = "active"
	OutcomeResolved        = "resolved"
	OutcomeRevoked         = "revoked"
	OutcomeExpired         = "expired"
)

type Notification struct {
	ID               string                 `json:"notification_id"`
	TypeKey          string                 `json:"type_key"`
	TemplateVersion  int                    `json:"template_version"`
	SourceEventID    string                 `json:"source_event_id"`
	ProjectID        string                 `json:"project_id,omitempty"`
	ActorID          string                 `json:"actor_id,omitempty"`
	ResourceType     string                 `json:"resource_type"`
	ResourceID       string                 `json:"resource_id"`
	Priority         string                 `json:"priority"`
	Data             map[string]interface{} `json:"data"`
	RenderedSnapshot map[string]interface{} `json:"rendered_snapshot,omitempty"`
	Action           *Action                `json:"action,omitempty"`
	OccurredAt       time.Time              `json:"occurred_at"`
	CreatedAt        time.Time              `json:"created_at"`
}

// Action is a browser-safe, typed command hint. It never contains a token,
// secret, signed URL, or arbitrary external URL.
type Action struct {
	Type       string `json:"action_type"`
	ResourceID string `json:"action_resource_id"`
	Route      string `json:"route,omitempty"`
}

type Recipient struct {
	ID              string     `json:"recipient_id"`
	NotificationID  string     `json:"notification_id"`
	RecipientKey    string     `json:"recipient_key"`
	UserID          string     `json:"user_id,omitempty"`
	NormalizedEmail string     `json:"normalized_email,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

type InboxItem struct {
	ID           string       `json:"inbox_item_id"`
	Notification Notification `json:"notification"`
	Recipient    Recipient    `json:"recipient"`
	ReadState    string       `json:"read_state"`
	ArchivedAt   *time.Time   `json:"archived_at,omitempty"`
	Outcome      string       `json:"outcome"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type Page struct {
	Items      []InboxItem `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
	HasMore    bool        `json:"has_more"`
}

type DeliveryPage struct {
	Items      []Delivery `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
	HasMore    bool       `json:"has_more"`
}

type Filter struct {
	ProjectID string
	TypeKey   string
	ReadState string
	Archived  string
	Outcome   string
}

type RecipientInput struct {
	Key             string
	UserID          string
	NormalizedEmail string
	ExpiresAt       *time.Time
}

type Rule struct {
	ProjectID       string    `json:"project_id"`
	TypeKey         string    `json:"type_key"`
	InboxEnabled    bool      `json:"inbox_enabled"`
	ExternalEnabled bool      `json:"external_enabled"`
	ChannelKeys     []string  `json:"channel_keys"`
	MinimumPriority string    `json:"minimum_priority"`
	Version         int64     `json:"version"`
	UpdatedBy       string    `json:"updated_by"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Delivery struct {
	ID              string     `json:"delivery_id"`
	NotificationID  string     `json:"notification_id"`
	RecipientID     string     `json:"recipient_id,omitempty"`
	ProjectID       string     `json:"project_id"`
	ChannelKey      string     `json:"channel_key"`
	TargetKey       string     `json:"target_key"`
	RuleVersion     int64      `json:"rule_version"`
	SettingsVersion int64      `json:"settings_version"`
	DeliveryKey     string     `json:"delivery_key"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	MaxAttempts     int        `json:"max_attempts"`
	ProviderMessage string     `json:"provider_message_id,omitempty"`
	LastErrorCode   string     `json:"last_error_code,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	ResponseSummary string     `json:"response_summary,omitempty"`
	AvailableAt     time.Time  `json:"available_at"`
	DeliveredAt     *time.Time `json:"delivered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Channel struct {
	Key             string `json:"channel_key"`
	Enabled         bool   `json:"enabled"`
	Configured      bool   `json:"configured"`
	SettingsVersion int64  `json:"settings_version"`
}

type Registry struct{ types map[string]Descriptor }

type Descriptor struct {
	TypeKey                     string
	SchemaVersion               int64
	SourceEventTypes            []string
	AcceptedEventSchemaVersions []int64
	Scope                       string
	InboxPolicy                 string
	ExternalAllowed             bool
	Priority                    string
	TemplateKey                 string
	TemplateVersion             int
	AllowedTemplateFields       []string
	RecipientResolver           string
	Renderer                    string
}

func NewRegistry() *Registry { return &Registry{types: map[string]Descriptor{}} }

func (registry *Registry) Register(descriptor Descriptor) error {
	if strings.TrimSpace(descriptor.TypeKey) == "" || descriptor.SchemaVersion < 1 ||
		descriptor.TemplateVersion < 1 || len(descriptor.SourceEventTypes) == 0 ||
		len(descriptor.AcceptedEventSchemaVersions) == 0 || descriptor.TemplateKey == "" ||
		descriptor.RecipientResolver == "" || descriptor.Renderer == "" ||
		len(descriptor.AllowedTemplateFields) == 0 {
		return fmt.Errorf("invalid notification type descriptor")
	}
	if descriptor.Scope != "project" && descriptor.Scope != "system" {
		return fmt.Errorf("invalid notification scope")
	}
	if descriptor.InboxPolicy != "required" && descriptor.InboxPolicy != "default_on" && descriptor.InboxPolicy != "optional" && descriptor.InboxPolicy != "disabled" {
		return fmt.Errorf("invalid notification inbox policy")
	}
	if descriptor.Priority != "low" && descriptor.Priority != "normal" && descriptor.Priority != "high" && descriptor.Priority != "urgent" {
		return fmt.Errorf("invalid notification priority")
	}
	knownEvents := map[string]bool{"project.member.invited": true, "progress.reminder.due": true}
	for _, source := range descriptor.SourceEventTypes {
		if !knownEvents[source] {
			return fmt.Errorf("unknown notification source event: %s", source)
		}
	}
	for _, field := range descriptor.AllowedTemplateFields {
		lower := strings.ToLower(field)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
			strings.Contains(lower, "password") || strings.Contains(lower, "credential") ||
			strings.Contains(lower, "signed_url") {
			return fmt.Errorf("notification template field is unsafe")
		}
	}
	if _, exists := registry.types[descriptor.TypeKey]; exists {
		return fmt.Errorf("notification type already registered: %s", descriptor.TypeKey)
	}
	registry.types[descriptor.TypeKey] = descriptor
	return nil
}

func (registry *Registry) Get(key string) (Descriptor, bool) {
	descriptor, ok := registry.types[key]
	return descriptor, ok
}

func DefaultRegistry() (*Registry, error) {
	registry := NewRegistry()
	if err := registry.Register(Descriptor{
		TypeKey: TypeInvitationReceived, SchemaVersion: 1,
		SourceEventTypes: []string{"project.member.invited"}, AcceptedEventSchemaVersions: []int64{1},
		Scope: "project", InboxPolicy: "required", ExternalAllowed: false, Priority: "high", TemplateKey: "project-invitation", TemplateVersion: 1,
		AllowedTemplateFields: []string{"project_id", "project_name", "invitation_id", "role", "normalized_email", "invited_by_user_id", "expires_at"},
		RecipientResolver:     "invitation.email", Renderer: "code",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(Descriptor{
		TypeKey: TypeReminderDue, SchemaVersion: 1,
		SourceEventTypes: []string{"progress.reminder.due"}, AcceptedEventSchemaVersions: []int64{1},
		Scope: "project", InboxPolicy: "default_on", ExternalAllowed: true, Priority: "normal", TemplateKey: "progress-reminder", TemplateVersion: 1,
		AllowedTemplateFields: []string{"project_id", "reminder_id", "task_id", "milestone_id", "title", "status"},
		RecipientResolver:     "event.actor_or_assignee", Renderer: "code",
	}); err != nil {
		return nil, err
	}
	return registry, nil
}

type Authenticator interface {
	Authenticate(context.Context, string) (auth.Identity, error)
}
type ProjectAccess interface {
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

type Store interface {
	CreateEvent(context.Context, Notification, []RecipientInput, bool, []DeliveryIntent) error
	ClaimEmailRecipients(context.Context, string, string) error
	ApplyInvitationOutcome(context.Context, string, string) error
	ListInbox(context.Context, string, Filter, pagination.Request) (Page, error)
	GetInbox(context.Context, string, string) (InboxItem, error)
	UpdateInbox(context.Context, string, string, *string, *bool) (InboxItem, error)
	MarkAllRead(context.Context, string, Filter) error
	UnreadCount(context.Context, string, string) (int64, error)
	GetRule(context.Context, string, string) (Rule, error)
	UpsertRule(context.Context, Rule) (Rule, error)
	ListDeliveries(context.Context, string, string, pagination.Request) (DeliveryPage, error)
	CreateRetry(context.Context, string, string, string) (Delivery, error)
}

type SettingsResolver interface {
	Resolve(context.Context, settings.Scope, string, string) (settings.ResolvedSetting, error)
}

type DeliveryStore interface {
	EnqueueDelivery(context.Context, Notification, string, int64) (Delivery, error)
	ClaimDelivery(context.Context, string, time.Duration) (*Delivery, Notification, error)
	CompleteDelivery(context.Context, string, string) error
	FailDelivery(context.Context, string, string, string, int, string, bool, time.Duration) error
	CancelDelivery(context.Context, string, string, string) error
	CancelPending(context.Context, string, string) error
}

type DeliveryIntent struct {
	ChannelKey      string
	RecipientID     string
	TargetKey       string
	RuleVersion     int64
	SettingsVersion int64
}

func (service Service) HandleSettingsEvent(ctx context.Context, event contract.EventEnvelope) error {
	if event.EventType != "settings.deleted" && event.EventType != "settings.updated" {
		return ErrInvalid
	}
	if service.Deliveries == nil || event.Payload == nil {
		return ErrNotReady
	}
	if stringValue(event.Payload["scope"]) != string(settings.ScopeProject) {
		return nil
	}
	if event.EventType == "settings.updated" && service.Settings != nil {
		resolved, err := service.Settings.Resolve(ctx, settings.ScopeProject, stringValue(event.Payload["scope_id"]), stringValue(event.Payload["type_key"]))
		if err != nil {
			if errors.Is(err, settings.ErrNotFound) {
				return service.Deliveries.CancelPending(ctx, stringValue(event.Payload["scope_id"]), stringValue(event.Payload["type_key"]))
			}
			return err
		}
		if resolved.Values["enabled"] == true {
			return nil
		}
	}
	return service.Deliveries.CancelPending(ctx, stringValue(event.Payload["scope_id"]), stringValue(event.Payload["type_key"]))
}

type Service struct {
	Audit interface {
		Record(context.Context, audit.Event) error
	}
	Adapter    Adapter
	Auth       Authenticator
	Access     ProjectAccess
	Clock      clock.Clock
	Generator  identity.Generator
	Registry   *Registry
	Store      Store
	Settings   SettingsResolver
	Deliveries DeliveryStore
	Metrics    *metrics.Registry
}

func (service Service) HandleEvent(ctx context.Context, event contract.EventEnvelope) error {
	if event.EventType == "project.invitation.revoked" || event.EventType == "project.invitation.expired" || event.EventType == "project.member.joined" {
		if event.Payload == nil {
			return ErrInvalid
		}
		outcome := OutcomeResolved
		if event.EventType == "project.invitation.revoked" {
			outcome = OutcomeRevoked
		}
		if event.EventType == "project.invitation.expired" {
			outcome = OutcomeExpired
		}
		invitationID := stringValue(event.Payload["invitation_id"])
		if invitationID == "" {
			return ErrInvalid
		}
		if err := service.Store.ApplyInvitationOutcome(ctx, invitationID, outcome); err != nil {
			return err
		}
		service.recordLifecycleAudit(ctx, event, "notification.invitation.outcome", invitationID, outcome)
		return nil
	}
	if event.EventType == "user.registered" {
		if event.Payload == nil {
			return ErrInvalid
		}
		email := strings.ToLower(stringValue(event.Payload["email"]))
		userID := stringValue(event.Payload["user_id"])
		if email == "" || userID == "" {
			return ErrInvalid
		}
		return service.Store.ClaimEmailRecipients(ctx, email, userID)
	}
	descriptors := service.typesForEvent(event.EventType)
	if len(descriptors) == 0 || event.ProjectID == nil || event.SchemaVersion < 1 {
		return ErrInvalid
	}
	handled := false
	for _, descriptor := range descriptors {
		if !containsVersion(descriptor.AcceptedEventSchemaVersions, event.SchemaVersion) {
			continue
		}
		handled = true
		if err := service.handleRegisteredEvent(ctx, event, descriptor); err != nil {
			return err
		}
	}
	if !handled {
		return ErrInvalid
	}
	return nil
}

func (service Service) typesForEvent(eventType string) []Descriptor {
	if service.Registry == nil {
		return nil
	}
	result := make([]Descriptor, 0)
	for _, descriptor := range service.Registry.types {
		for _, source := range descriptor.SourceEventTypes {
			if source == eventType {
				result = append(result, descriptor)
				break
			}
		}
	}
	return result
}

func (service Service) handleRegisteredEvent(ctx context.Context, event contract.EventEnvelope, descriptor Descriptor) error {
	data := allowedData(event.Payload, descriptor.AllowedTemplateFields)
	projectID := *event.ProjectID
	actorID := event.Actor["user_id"]
	resourceType, _ := data["resource_type"].(string)
	resourceID, _ := data["resource_id"].(string)
	if resourceType == "" && descriptor.TypeKey == TypeInvitationReceived {
		resourceType = "invitation"
	}
	if resourceID == "" {
		resourceID = stringValue(data["invitation_id"])
	}
	if resourceID == "" {
		resourceID = stringValue(data["reminder_id"])
	}
	id, err := service.Generator.New()
	if err != nil {
		return err
	}
	rule, err := service.Store.GetRule(ctx, projectID, descriptor.TypeKey)
	if err != nil {
		return err
	}
	if descriptor.InboxPolicy == "required" {
		rule.InboxEnabled = true
	}
	inboxEnabled := descriptor.InboxPolicy != "disabled" && rule.InboxEnabled
	notification := Notification{ID: id, TypeKey: descriptor.TypeKey, TemplateVersion: descriptor.TemplateVersion, SourceEventID: event.EventID, ProjectID: projectID, ActorID: actorID, ResourceType: resourceType, ResourceID: resourceID, Priority: descriptor.Priority, Data: data, RenderedSnapshot: data, OccurredAt: event.OccurredAt, CreatedAt: event.OccurredAt}
	if descriptor.TypeKey == TypeInvitationReceived {
		if invitationID := stringValue(data["invitation_id"]); invitationID != "" {
			notification.Action = &Action{
				Type:       "project.invitation.accept",
				ResourceID: invitationID,
				Route:      "/inbox",
			}
		}
	}
	intents := make([]DeliveryIntent, 0)
	if service.Settings != nil && service.Deliveries != nil && descriptor.ExternalAllowed && rule.ExternalEnabled && priorityAtLeast(notification.Priority, rule.MinimumPriority) {
		channels := rule.ChannelKeys
		for _, channelKey := range channels {
			if !allowedChannel(channelKey) {
				return ErrInvalid
			}
			resolved, resolveErr := service.Settings.Resolve(ctx, settings.ScopeProject, projectID, channelKey)
			if errors.Is(resolveErr, settings.ErrNotFound) {
				continue
			}
			if resolveErr != nil {
				return resolveErr
			}
			if resolved.Values["enabled"] == true {
				intents = append(intents, DeliveryIntent{ChannelKey: channelKey, TargetKey: "project-channel:" + channelKey, RuleVersion: rule.Version, SettingsVersion: resolved.Version})
			}
		}
	}
	err = service.Store.CreateEvent(ctx, notification, resolveRecipients(event, data), inboxEnabled, intents)
	if err == nil && service.Metrics != nil {
		service.Metrics.ObserveNotificationCreated()
	}
	return err
}
func (service Service) HandleReminderDue(ctx context.Context, event contract.EventEnvelope) error {
	if event.EventType != "progress.reminder.due" || event.ProjectID == nil {
		return ErrInvalid
	}
	if event.Payload == nil {
		return ErrInvalid
	}
	reminderID := stringValue(event.Payload["reminder_id"])
	if reminderID == "" {
		return ErrInvalid
	}
	if service.Adapter == nil {
		return service.HandleEvent(ctx, event)
	}
	id, err := service.Generator.New()
	if err != nil {
		return err
	}
	return service.Adapter.Accept(ctx, Intent{ID: id, SourceEventID: event.EventID, ProjectID: *event.ProjectID, TypeKey: TypeReminderDue, ReminderID: reminderID, Status: "accepted", CreatedAt: event.OccurredAt, Recipients: resolveRecipients(event, event.Payload)})
}

func (service Service) recordLifecycleAudit(ctx context.Context, event contract.EventEnvelope, action, resourceID, outcome string) {
	if service.Audit == nil {
		return
	}
	projectID := ""
	if event.ProjectID != nil {
		projectID = *event.ProjectID
	}
	_ = service.Audit.Record(ctx, audit.Event{
		Action: action, ActorID: event.Actor["user_id"], ActorKind: "event",
		Category: "notification", Metadata: map[string]interface{}{"source_event_id": event.EventID},
		OccurredAt: event.OccurredAt, Outcome: outcome, ProjectID: projectID,
		ResourceID: resourceID, ResourceType: "notification-invitation", Source: "notification",
		RecordedAt: service.now(),
	})
}

func allowedChannel(channelKey string) bool {
	return channelKey == "notification.feishu_webhook" || channelKey == "notification.generic_webhook"
}

func priorityAtLeast(priority, minimum string) bool {
	rank := map[string]int{"low": 1, "normal": 2, "high": 3, "urgent": 4}
	if minimum == "" {
		minimum = "normal"
	}
	return rank[priority] >= rank[minimum]
}

func (service Service) ListInbox(ctx context.Context, identity auth.Identity, filter Filter, page pagination.Request) (Page, error) {
	if err := humanInboxCaller(identity); err != nil {
		return Page{}, err
	}
	page, err := page.Normalize()
	if err != nil {
		return Page{}, ErrInvalid
	}
	return service.Store.ListInbox(ctx, identity.User.ID, filter, page)
}
func (service Service) GetInbox(ctx context.Context, identity auth.Identity, id string) (InboxItem, error) {
	if err := humanInboxCaller(identity); err != nil {
		return InboxItem{}, err
	}
	return service.Store.GetInbox(ctx, identity.User.ID, id)
}
func (service Service) UpdateInbox(ctx context.Context, identity auth.Identity, id string, readState *string, archived *bool) (InboxItem, error) {
	if err := humanInboxCaller(identity); err != nil {
		return InboxItem{}, err
	}
	if readState != nil && *readState != "read" && *readState != "unread" {
		return InboxItem{}, ErrInvalid
	}
	return service.Store.UpdateInbox(ctx, identity.User.ID, id, readState, archived)
}
func (service Service) MarkAllRead(ctx context.Context, identity auth.Identity, filter Filter) error {
	if err := humanInboxCaller(identity); err != nil {
		return err
	}
	return service.Store.MarkAllRead(ctx, identity.User.ID, filter)
}
func (service Service) UnreadCount(ctx context.Context, identity auth.Identity, projectID string) (int64, error) {
	if err := humanInboxCaller(identity); err != nil {
		return 0, err
	}
	return service.Store.UnreadCount(ctx, identity.User.ID, projectID)
}
func humanInboxCaller(identity auth.Identity) error {
	if identity.Kind == "agent" || identity.Kind == "box" {
		return project.ErrForbidden
	}
	return nil
}
func (service Service) GetRule(ctx context.Context, identity auth.Identity, projectID, typeKey string) (Rule, error) {
	if err := service.authorizeProject(ctx, identity, projectID, project.PermissionSettingsRead); err != nil {
		return Rule{}, err
	}
	if service.Registry == nil {
		return Rule{}, ErrNotReady
	}
	if _, ok := service.Registry.Get(typeKey); !ok {
		return Rule{}, ErrNotFound
	}
	return service.Store.GetRule(ctx, projectID, typeKey)
}
func (service Service) UpsertRule(ctx context.Context, identity auth.Identity, rule Rule) (Rule, error) {
	if err := service.authorizeProject(ctx, identity, rule.ProjectID, project.PermissionSettingsManage); err != nil {
		return Rule{}, err
	}
	if service.Registry == nil {
		return Rule{}, ErrNotReady
	}
	descriptor, ok := service.Registry.Get(rule.TypeKey)
	if !ok {
		return Rule{}, ErrNotFound
	}
	if descriptor.InboxPolicy == "required" {
		rule.InboxEnabled = true
	}
	if !descriptor.ExternalAllowed {
		rule.ExternalEnabled = false
		rule.ChannelKeys = nil
	}
	if rule.MinimumPriority == "" {
		rule.MinimumPriority = "normal"
	}
	if !priorityAtLeast("urgent", rule.MinimumPriority) || !priorityAtLeast(rule.MinimumPriority, "low") {
		return Rule{}, ErrInvalid
	}
	seenChannels := map[string]bool{}
	for _, channelKey := range rule.ChannelKeys {
		if !allowedChannel(channelKey) || seenChannels[channelKey] {
			return Rule{}, ErrInvalid
		}
		seenChannels[channelKey] = true
		if rule.ExternalEnabled && service.Settings != nil {
			resolved, resolveErr := service.Settings.Resolve(ctx, settings.ScopeProject, rule.ProjectID, channelKey)
			if resolveErr != nil || resolved.Values["enabled"] != true {
				return Rule{}, ErrInvalid
			}
		}
	}
	if rule.ExternalEnabled && len(rule.ChannelKeys) == 0 {
		return Rule{}, ErrInvalid
	}
	rule.UpdatedBy = identity.User.ID
	rule.UpdatedAt = service.now()
	result, err := service.Store.UpsertRule(ctx, rule)
	if err == nil && service.Audit != nil {
		_ = service.Audit.Record(ctx, audit.Event{Action: "notification.rule.updated", ActorID: identity.User.ID, ActorKind: identity.Kind, Category: "notification", Metadata: map[string]interface{}{"type_key": rule.TypeKey, "external_enabled": rule.ExternalEnabled, "inbox_enabled": rule.InboxEnabled}, OccurredAt: service.now(), Outcome: "success", ProjectID: rule.ProjectID, ResourceID: rule.TypeKey, ResourceType: "notification-rule", Source: "notification", RecordedAt: service.now()})
	}
	return result, err
}
func (service Service) ListDeliveries(ctx context.Context, identity auth.Identity, projectID, channelKey string, page pagination.Request) (DeliveryPage, error) {
	if err := service.authorizeProject(ctx, identity, projectID, project.PermissionSettingsRead); err != nil {
		return DeliveryPage{}, err
	}
	page, err := page.Normalize()
	if err != nil {
		return DeliveryPage{}, ErrInvalid
	}
	return service.Store.ListDeliveries(ctx, projectID, channelKey, page)
}
func (service Service) RetryDelivery(ctx context.Context, identity auth.Identity, projectID, deliveryID, reason string) (Delivery, error) {
	if err := service.authorizeProject(ctx, identity, projectID, project.PermissionSettingsManage); err != nil {
		return Delivery{}, err
	}
	if strings.TrimSpace(reason) == "" || len(reason) > 1000 {
		return Delivery{}, ErrInvalid
	}
	result, err := service.Store.CreateRetry(ctx, projectID, deliveryID, reason)
	if err == nil && service.Audit != nil {
		_ = service.Audit.Record(ctx, audit.Event{Action: "notification.delivery.retried", ActorID: identity.User.ID, ActorKind: identity.Kind, Category: "notification", Metadata: map[string]interface{}{"reason_length": len(reason)}, OccurredAt: service.now(), Outcome: "success", ProjectID: projectID, ResourceID: deliveryID, ResourceType: "notification-delivery", Source: "notification", RecordedAt: service.now()})
	}
	return result, err
}
func (service Service) authorizeProject(ctx context.Context, identity auth.Identity, projectID string, permission project.Permission) error {
	if service.Access == nil {
		return nil
	}
	return service.Access.Authorize(ctx, identity, projectID, permission)
}
func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock.Now().UTC()
}

func resolveRecipients(event contract.EventEnvelope, data map[string]interface{}) []RecipientInput {
	var expiresAt *time.Time
	if value := timeValue(data["expires_at"]); !value.IsZero() {
		expiresAt = &value
	}
	if userID := stringValue(data["user_id"]); userID != "" {
		return []RecipientInput{{Key: "user:" + userID, UserID: userID, ExpiresAt: expiresAt}}
	}
	if userID := stringValue(data["assignee_id"]); userID != "" {
		return []RecipientInput{{Key: "user:" + userID, UserID: userID, ExpiresAt: expiresAt}}
	}
	if userID := event.Actor["user_id"]; userID != "" && event.EventType == "progress.reminder.due" {
		return []RecipientInput{{Key: "user:" + userID, UserID: userID, ExpiresAt: expiresAt}}
	}
	emailValue := stringValue(data["normalized_email"])
	if emailValue == "" {
		emailValue = stringValue(data["email"])
	}
	if email := emailValue; email != "" {
		email = strings.ToLower(strings.TrimSpace(email))
		return []RecipientInput{{Key: "email:" + email, NormalizedEmail: email, ExpiresAt: expiresAt}}
	}
	return nil
}

func timeValue(value interface{}) time.Time {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC()
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(typed))
		if err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}
func allowedData(data map[string]interface{}, allowed []string) map[string]interface{} {
	result := map[string]interface{}{}
	for _, key := range allowed {
		if value, ok := data[key]; ok {
			result[key] = value
		}
	}
	return result
}
func stringValue(value interface{}) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}
func containsVersion(values []int64, wanted int64) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

var (
	ErrInvalid  = errors.New("invalid notification request")
	ErrNotFound = errors.New("notification not found")
	ErrNotReady = errors.New("notification service is not ready")
	ErrConflict = errors.New("notification rule version conflict")
)

// Adapter is retained as the small Progress seam for compatibility with the
// Stage 4 consumer. Complete provider adapters implement ProviderAdapter below.
type Adapter interface {
	Accept(context.Context, Intent) error
}
type ProviderAdapter interface {
	Key() string
	ValidateConfig(map[string]interface{}) error
	Test(context.Context, map[string]interface{}) error
	Render(context.Context, Notification, int) (RenderedMessage, error)
	Send(context.Context, map[string]interface{}, string, string, RenderedMessage) error
}
type RenderedMessage struct {
	Body        []byte
	ContentType string
	Headers     map[string]string
}

type PersistenceAdapter struct{ Store Store }
type Intent struct {
	ID            string    `json:"notification_id"`
	SourceEventID string    `json:"source_event_id"`
	ProjectID     string    `json:"project_id"`
	TypeKey       string    `json:"type_key"`
	ReminderID    string    `json:"reminder_id"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	Notification  Notification
	Recipients    []RecipientInput
}

func (adapter PersistenceAdapter) Accept(ctx context.Context, intent Intent) error {
	if adapter.Store == nil {
		return ErrNotReady
	}
	if intent.Notification.ID == "" {
		data := map[string]interface{}{"reminder_id": intent.ReminderID}
		intent.Notification = Notification{ID: intent.ID, TypeKey: TypeReminderDue, TemplateVersion: 1, SourceEventID: intent.SourceEventID, ProjectID: intent.ProjectID, ResourceType: "reminder", ResourceID: intent.ReminderID, Priority: "normal", Data: data, RenderedSnapshot: data, OccurredAt: intent.CreatedAt, CreatedAt: intent.CreatedAt}
	}
	return adapter.Store.CreateEvent(ctx, intent.Notification, intent.Recipients, true, nil)
}
func (store PostgresStore) SaveReminderIntent(ctx context.Context, intent Intent) error {
	return store.CreateEvent(ctx, intent.Notification, intent.Recipients, true, nil)
}

// PostgresStore is the canonical Notification persistence implementation.
type PostgresStore struct {
	Clock       clock.Clock
	DB          *sql.DB
	Generator   identity.Generator
	Transaction transaction.Manager
}

func (store PostgresStore) CreateEvent(ctx context.Context, notification Notification, recipients []RecipientInput, inboxEnabled bool, deliveries []DeliveryIntent) error {
	if store.DB == nil || store.Transaction.DB == nil {
		return ErrNotReady
	}
	data := notification.Data
	if data == nil {
		data = map[string]interface{}{}
	}
	snapshot := notification.RenderedSnapshot
	if snapshot == nil {
		snapshot = data
	}
	now := notification.CreatedAt
	if now.IsZero() {
		now = store.now()
	}
	if notification.OccurredAt.IsZero() {
		notification.OccurredAt = now
	}
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		return store.createEventTx(ctx, tx, notification, data, snapshot, recipients, inboxEnabled, deliveries, now)
	})
}

func (store PostgresStore) createEventTx(ctx context.Context, tx transaction.Tx, notification Notification, data, snapshot map[string]interface{}, recipients []RecipientInput, inboxEnabled bool, deliveries []DeliveryIntent, now time.Time) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	actionType, actionResourceID, actionRoute := "", "", ""
	if notification.Action != nil {
		actionType = notification.Action.Type
		actionResourceID = notification.Action.ResourceID
		actionRoute = notification.Action.Route
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO notification_notifications(notification_id,type_key,template_version,source_event_id,project_id,actor_id,resource_type,resource_id,priority,data,rendered_snapshot,action_type,action_resource_id,action_route,occurred_at,created_at) VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8,$9,$10,$11,NULLIF($12,''),NULLIF($13,''),NULLIF($14,''),$15,$16) ON CONFLICT(source_event_id,type_key) DO UPDATE SET action_type=COALESCE(notification_notifications.action_type,EXCLUDED.action_type),action_resource_id=COALESCE(notification_notifications.action_resource_id,EXCLUDED.action_resource_id),action_route=COALESCE(notification_notifications.action_route,EXCLUDED.action_route) RETURNING notification_id`, notification.ID, notification.TypeKey, notification.TemplateVersion, notification.SourceEventID, notification.ProjectID, notification.ActorID, notification.ResourceType, notification.ResourceID, notification.Priority, dataJSON, snapshotJSON, actionType, actionResourceID, actionRoute, notification.OccurredAt, now).Scan(&notification.ID); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if recipient.UserID == "" && recipient.NormalizedEmail != "" {
			var userID string
			lookupErr := tx.QueryRowContext(ctx, `SELECT user_id FROM auth_users WHERE LOWER(email)=LOWER($1) AND status='active'`, recipient.NormalizedEmail).Scan(&userID)
			if lookupErr == nil {
				recipient.UserID = userID
				recipient.Key = "user:" + userID
			}
			if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
				return lookupErr
			}
		}
		if strings.TrimSpace(recipient.Key) == "" {
			return ErrInvalid
		}
		rid, err := store.Generator.New()
		if err != nil {
			return err
		}
		var recipientID string
		if err := tx.QueryRowContext(ctx, `INSERT INTO notification_recipients(recipient_id,notification_id,recipient_key,user_id,normalized_email,expires_at,created_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,''),$6,$7) ON CONFLICT(notification_id,recipient_key) DO UPDATE SET recipient_key=notification_recipients.recipient_key RETURNING recipient_id`, rid, notification.ID, recipient.Key, recipient.UserID, recipient.NormalizedEmail, recipient.ExpiresAt, now).Scan(&recipientID); err != nil {
			return err
		}
		if inboxEnabled {
			inboxID, err := store.Generator.New()
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO notification_inbox_items(inbox_item_id,notification_id,recipient_id,created_at,updated_at) VALUES($1,$2,$3,$4,$4) ON CONFLICT(recipient_id) DO NOTHING`, inboxID, notification.ID, recipientID, now); err != nil {
				return err
			}
		}
	}
	for _, delivery := range deliveries {
		if !allowedChannel(delivery.ChannelKey) {
			return ErrInvalid
		}
		deliveryID, err := store.Generator.New()
		if err != nil {
			return err
		}
		targetKey := delivery.TargetKey
		if targetKey == "" {
			targetKey = "project-channel:" + delivery.ChannelKey
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO notification_deliveries(delivery_id,notification_id,recipient_id,project_id,channel_key,target_key,rule_version,settings_version,delivery_key,status,max_attempts,available_at,created_at,updated_at) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,'live','pending',5,$9,$9,$9) ON CONFLICT(notification_id,channel_key,target_key,delivery_key) DO NOTHING`, deliveryID, notification.ID, delivery.RecipientID, notification.ProjectID, delivery.ChannelKey, targetKey, delivery.RuleVersion, delivery.SettingsVersion, now); err != nil {
			return err
		}
	}
	return nil
}
func (store PostgresStore) now() time.Time {
	if store.Clock == nil {
		return time.Now().UTC()
	}
	return store.Clock.Now().UTC()
}
