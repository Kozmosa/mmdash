// Package notification contains the Stage 4 NotificationAdapter boundary.
// It records reminder notification intent only; external providers, Inbox,
// rules, delivery retries, and channel secrets belong to Notification 3.17.
package notification

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
)

type Intent struct {
	ID            string    `json:"notification_id"`
	SourceEventID string    `json:"source_event_id"`
	ProjectID     string    `json:"project_id"`
	TypeKey       string    `json:"type_key"`
	ReminderID    string    `json:"reminder_id"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type Adapter interface {
	Accept(context.Context, Intent) error
}

type Store interface {
	SaveReminderIntent(context.Context, Intent) error
}

type Service struct {
	Adapter   Adapter
	Clock     clock.Clock
	Generator identity.Generator
}

func (service Service) HandleReminderDue(ctx context.Context, event contract.EventEnvelope) error {
	if event.ProjectID == nil || event.EventType != "progress.reminder.due" {
		return ErrInvalid
	}
	reminderID, _ := event.Payload["reminder_id"].(string)
	if strings.TrimSpace(reminderID) == "" {
		return ErrInvalid
	}
	id, err := service.Generator.New()
	if err != nil {
		return err
	}
	intent := Intent{ID: id, SourceEventID: event.EventID, ProjectID: *event.ProjectID, TypeKey: "progress.reminder.due", ReminderID: reminderID, Status: "accepted", CreatedAt: event.OccurredAt}
	if service.Adapter == nil {
		return ErrNotReady
	}
	return service.Adapter.Accept(ctx, intent)
}

type PersistenceAdapter struct{ Store Store }

func (adapter PersistenceAdapter) Accept(ctx context.Context, intent Intent) error {
	if adapter.Store == nil {
		return ErrNotReady
	}
	return adapter.Store.SaveReminderIntent(ctx, intent)
}

type PostgresStore struct {
	Clock clock.Clock
	DB    *sql.DB
}

func (store PostgresStore) SaveReminderIntent(ctx context.Context, intent Intent) error {
	if store.DB == nil {
		return ErrNotReady
	}
	_, err := store.DB.ExecContext(ctx, `INSERT INTO notification_intents(notification_id,source_event_id,project_id,type_key,reminder_id,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(source_event_id) DO NOTHING`, intent.ID, intent.SourceEventID, intent.ProjectID, intent.TypeKey, intent.ReminderID, intent.Status, intent.CreatedAt)
	return err
}

var (
	ErrInvalid  = errors.New("invalid notification intent")
	ErrNotReady = errors.New("notification adapter is not ready")
)
