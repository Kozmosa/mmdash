// Package audit owns the append-only, queryable security and activity trail.
package audit

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/logging"
	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/project"
)

var (
	categoryPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	actionPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`)
	sourcePattern   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	uuidPattern     = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`,
	)
)

// Event is one immutable, secret-free audit record.
type Event struct {
	Action       string                 `json:"action"`
	ActorID      string                 `json:"actor_id,omitempty"`
	ActorKind    string                 `json:"actor_kind"`
	Category     string                 `json:"category"`
	DurationMS   *int64                 `json:"duration_ms,omitempty"`
	ErrorCode    string                 `json:"error_code"`
	ID           string                 `json:"audit_id"`
	Metadata     map[string]interface{} `json:"metadata"`
	OccurredAt   time.Time              `json:"occurred_at"`
	Outcome      string                 `json:"outcome"`
	ProjectID    string                 `json:"project_id,omitempty"`
	RecordedAt   time.Time              `json:"recorded_at"`
	RequestID    string                 `json:"request_id"`
	ResourceID   string                 `json:"resource_id"`
	ResourceType string                 `json:"resource_type"`
	Source       string                 `json:"source"`
}

// Input is accepted from another authenticated mmdash process.
type Input struct {
	Action       string
	Category     string
	DurationMS   *int64
	ErrorCode    string
	Metadata     map[string]interface{}
	OccurredAt   *time.Time
	Outcome      string
	ProjectID    string
	ResourceID   string
	ResourceType string
	Source       string
}

// Filter is the stable audit search surface.
type Filter struct {
	Action    string
	ActorID   string
	Category  string
	Outcome   string
	ProjectID string
	RequestID string
	Source    string
}

type Page struct {
	HasMore    bool    `json:"has_more"`
	Items      []Event `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

type Store interface {
	List(context.Context, Filter, pagination.Request) (Page, error)
	Record(context.Context, Event) (Event, error)
}

type Access interface {
	Authenticate(context.Context, string) (auth.Identity, error)
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

// Service applies ingestion and query authorization.
type Service struct {
	Access Access
	Clock  interface{ Now() time.Time }
	Store  Store
}

func (service Service) Authenticate(ctx context.Context, authorization string) (auth.Identity, error) {
	return service.Access.Authenticate(ctx, authorization)
}

func (service Service) Ingest(
	ctx context.Context,
	identity auth.Identity,
	input Input,
) (Event, error) {
	if identity.Kind != "api" && identity.Kind != "box" {
		return Event{}, ErrForbidden
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	if identity.ProjectID != "" {
		if input.ProjectID == "" {
			input.ProjectID = identity.ProjectID
		}
		if input.ProjectID != identity.ProjectID {
			return Event{}, ErrForbidden
		}
	}
	if input.ProjectID != "" {
		if err := service.Access.Authorize(
			ctx, identity, input.ProjectID, project.PermissionAuditWrite,
		); err != nil {
			return Event{}, err
		}
	} else if identity.User.SystemRole != "admin" {
		return Event{}, ErrForbidden
	}
	now := service.Clock.Now().UTC()
	occurredAt := now
	if input.OccurredAt != nil {
		occurredAt = input.OccurredAt.UTC()
		if occurredAt.After(now.Add(5*time.Minute)) ||
			occurredAt.Before(now.Add(-30*24*time.Hour)) {
			return Event{}, ErrInvalid
		}
	}
	event := Event{
		Action: input.Action, ActorID: identity.User.ID, ActorKind: identity.Kind,
		Category: input.Category, DurationMS: input.DurationMS,
		ErrorCode: input.ErrorCode, Metadata: input.Metadata,
		OccurredAt: occurredAt, Outcome: input.Outcome, ProjectID: input.ProjectID,
		RequestID: requestctx.RequestID(ctx), ResourceID: input.ResourceID,
		ResourceType: input.ResourceType, Source: input.Source,
	}
	if err := validate(&event); err != nil {
		return Event{}, err
	}
	return service.Store.Record(ctx, event)
}

func (service Service) List(
	ctx context.Context,
	identity auth.Identity,
	filter Filter,
	page pagination.Request,
) (Page, error) {
	filter.ProjectID = strings.TrimSpace(filter.ProjectID)
	if identity.User.SystemRole != "admin" {
		if filter.ProjectID == "" {
			return Page{}, ErrForbidden
		}
		if err := service.Access.Authorize(
			ctx, identity, filter.ProjectID, project.PermissionAuditRead,
		); err != nil {
			return Page{}, err
		}
	}
	if identity.ProjectID != "" && identity.ProjectID != filter.ProjectID {
		return Page{}, ErrForbidden
	}
	if !validFilter(filter) {
		return Page{}, ErrInvalid
	}
	normalized, err := page.Normalize()
	if err != nil {
		return Page{}, ErrInvalid
	}
	return service.Store.List(ctx, filter, normalized)
}

func validFilter(filter Filter) bool {
	if filter.ProjectID != "" && !uuidPattern.MatchString(filter.ProjectID) {
		return false
	}
	if filter.ActorID != "" && !uuidPattern.MatchString(filter.ActorID) {
		return false
	}
	if filter.Category != "" && !categoryPattern.MatchString(filter.Category) {
		return false
	}
	if filter.Action != "" && !actionPattern.MatchString(filter.Action) {
		return false
	}
	if filter.Source != "" && !sourcePattern.MatchString(filter.Source) {
		return false
	}
	if filter.Outcome != "" &&
		filter.Outcome != "success" &&
		filter.Outcome != "denied" &&
		filter.Outcome != "error" {
		return false
	}
	return len(filter.RequestID) <= 128
}

func validate(event *Event) error {
	event.Action = strings.TrimSpace(event.Action)
	event.ActorKind = strings.TrimSpace(event.ActorKind)
	event.Category = strings.TrimSpace(event.Category)
	event.ErrorCode = strings.TrimSpace(event.ErrorCode)
	event.Outcome = strings.TrimSpace(event.Outcome)
	event.RequestID = strings.TrimSpace(event.RequestID)
	event.ResourceID = strings.TrimSpace(event.ResourceID)
	event.ResourceType = strings.TrimSpace(event.ResourceType)
	event.Source = strings.TrimSpace(event.Source)
	if !actionPattern.MatchString(event.Action) ||
		!categoryPattern.MatchString(event.Category) ||
		!sourcePattern.MatchString(event.Source) ||
		event.RequestID == "" ||
		(event.Outcome != "success" && event.Outcome != "denied" && event.Outcome != "error") {
		return ErrInvalid
	}
	if event.DurationMS != nil && *event.DurationMS < 0 {
		return ErrInvalid
	}
	if event.Metadata == nil {
		event.Metadata = map[string]interface{}{}
	}
	clean, ok := logging.Sanitize("metadata", event.Metadata).(map[string]interface{})
	if !ok {
		return ErrInvalid
	}
	event.Metadata = clean
	return nil
}

// Recorder is the unified Core interface used by middleware and domains.
type Recorder struct {
	Clock   interface{ Now() time.Time }
	Logger  *logging.Logger
	Metrics *metrics.Registry
	Store   Store
}

func (recorder Recorder) Record(ctx context.Context, event Event) error {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = recorder.Clock.Now().UTC()
	}
	if event.RequestID == "" {
		event.RequestID = requestctx.RequestID(ctx)
	}
	if event.ActorID == "" {
		values := requestctx.TrustedSnapshot(ctx)
		event.ActorID = values.ActorID
		event.ActorKind = values.ActorKind
		event.ProjectID = firstNonEmpty(event.ProjectID, values.ProjectID)
	}
	if event.ActorKind == "" {
		event.ActorKind = "anonymous"
	}
	if err := validate(&event); err != nil {
		return err
	}
	_, err := recorder.Store.Record(ctx, event)
	if err != nil {
		recorder.Metrics.IncrementAuditFailure()
		if recorder.Logger != nil {
			recorder.Logger.Error("audit.record.failed", map[string]interface{}{
				"action": event.Action, "error": err.Error(),
				"project_id": event.ProjectID, "request_id": event.RequestID,
			})
		}
	}
	return err
}

func firstNonEmpty(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}

var (
	ErrForbidden = project.ErrForbidden
	ErrInvalid   = errors.New("invalid audit input")
	ErrNotFound  = errors.New("audit event not found")
)

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
