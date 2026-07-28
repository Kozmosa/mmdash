// Package outbox writes domain events using the caller's business transaction.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// Event is the stable system_outbox envelope.
type Event struct {
	Actor         interface{}
	CausationID   string
	CorrelationID string
	EventID       string
	EventType     string
	OccurredAt    time.Time
	Payload       interface{}
	Producer      string
	ProjectID     string
	SchemaVersion int
}

// Writer persists events in the same transaction as business state.
type Writer struct {
	Clock     clock.Clock
	Generator identity.Generator
}

// Write validates and inserts an outbox event using the supplied transaction.
func (writer Writer) Write(ctx context.Context, tx transaction.Tx, event Event) (Event, error) {
	if event.EventType == "" || event.Producer == "" || event.Payload == nil {
		return Event{}, fmt.Errorf("outbox event type, producer, and payload are required")
	}
	if event.SchemaVersion == 0 {
		event.SchemaVersion = 1
	}
	if event.SchemaVersion < 1 {
		return Event{}, fmt.Errorf("outbox schema version must be positive")
	}
	if event.EventID == "" {
		generated, err := writer.Generator.New()
		if err != nil {
			return Event{}, err
		}
		event.EventID = generated
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = writer.Clock.Now().UTC()
	}
	actor, err := marshalOptional(event.Actor)
	if err != nil {
		return Event{}, fmt.Errorf("marshal outbox actor: %w", err)
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return Event{}, fmt.Errorf("marshal outbox payload: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO system_outbox (
			event_id, event_type, schema_version, occurred_at, producer,
			project_id, actor, correlation_id, causation_id, payload
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, NULLIF($8, ''), NULLIF($9, ''), $10)
	`,
		event.EventID,
		event.EventType,
		event.SchemaVersion,
		event.OccurredAt,
		event.Producer,
		event.ProjectID,
		actor,
		event.CorrelationID,
		event.CausationID,
		payload,
	)
	if err != nil {
		return Event{}, fmt.Errorf("insert outbox event: %w", err)
	}
	return event, nil
}

func marshalOptional(value interface{}) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}
