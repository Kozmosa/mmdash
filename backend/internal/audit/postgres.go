package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PostgresStore writes and searches the append-only audit ledger.
type PostgresStore struct {
	Clock     clock.Clock
	DB        *sql.DB
	Generator identity.Generator
}

func (store PostgresStore) Record(ctx context.Context, event Event) (Event, error) {
	return store.record(ctx, store.DB, event)
}

// RecordInTransaction appends an audit event in the caller's business
// transaction.
func (store PostgresStore) RecordInTransaction(
	ctx context.Context,
	tx transaction.Tx,
	event Event,
) (Event, error) {
	return store.record(ctx, tx, event)
}

type auditExecutor interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

func (store PostgresStore) record(
	ctx context.Context,
	executor auditExecutor,
	event Event,
) (Event, error) {
	if event.ID == "" {
		generated, err := store.Generator.New()
		if err != nil {
			return Event{}, err
		}
		event.ID = generated
	}
	if event.RecordedAt.IsZero() {
		event.RecordedAt = store.Clock.Now().UTC()
	}
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return Event{}, fmt.Errorf("encode audit metadata: %w", err)
	}
	_, err = executor.ExecContext(ctx, `
		INSERT INTO audit_events (
			audit_id, occurred_at, recorded_at, request_id,
			actor_id, actor_kind, project_id, category, action, outcome,
			source, resource_type, resource_id, duration_ms, error_code, metadata
		) VALUES (
			$1, $2, $3, $4, NULLIF($5, '')::uuid, $6,
			NULLIF($7, '')::uuid, $8, $9, $10, $11, $12, $13, $14, $15, $16
		)
	`, event.ID, event.OccurredAt, event.RecordedAt, event.RequestID,
		event.ActorID, event.ActorKind, event.ProjectID, event.Category,
		event.Action, event.Outcome, event.Source, event.ResourceType,
		event.ResourceID, event.DurationMS, event.ErrorCode, metadata)
	if err != nil {
		return Event{}, wrap("insert audit event", err)
	}
	return event, nil
}

func (store PostgresStore) List(
	ctx context.Context,
	filter Filter,
	page pagination.Request,
) (Page, error) {
	cursorTime, cursorID, err := decodeCursor(page.Cursor)
	if err != nil {
		return Page{}, ErrInvalid
	}
	rows, err := store.DB.QueryContext(ctx, `
		SELECT audit_id, occurred_at, recorded_at, request_id,
		       COALESCE(actor_id::text, ''), actor_kind,
		       COALESCE(project_id::text, ''), category, action, outcome,
		       source, resource_type, resource_id, duration_ms, error_code, metadata
		FROM audit_events
		WHERE ($1 = '' OR project_id = NULLIF($1, '')::uuid)
		  AND ($2 = '' OR actor_id = NULLIF($2, '')::uuid)
		  AND ($3 = '' OR category = $3)
		  AND ($4 = '' OR action = $4)
		  AND ($5 = '' OR outcome = $5)
		  AND ($6 = '' OR source = $6)
		  AND ($7 = '' OR request_id = $7)
		  AND (
		    NULLIF($8, '') IS NULL
		    OR (occurred_at, audit_id) <
		       (NULLIF($8, '')::timestamptz, NULLIF($9, '')::uuid)
		  )
		ORDER BY occurred_at DESC, audit_id DESC
		LIMIT $10
	`, filter.ProjectID, filter.ActorID, filter.Category, filter.Action,
		filter.Outcome, filter.Source, filter.RequestID,
		cursorTime, cursorID, page.Limit+1)
	if err != nil {
		return Page{}, wrap("list audit events", err)
	}
	defer rows.Close()
	items := make([]Event, 0, page.Limit)
	for rows.Next() {
		event, scanErr := scanEvent(rows.Scan)
		if scanErr != nil {
			return Page{}, scanErr
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	hasMore := len(items) > page.Limit
	if hasMore {
		items = items[:page.Limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor, err = pagination.Encode(pagination.Cursor{
			ID: last.ID, SortValue: last.OccurredAt.Format(time.RFC3339Nano),
		})
		if err != nil {
			return Page{}, err
		}
	}
	return Page{HasMore: hasMore, Items: items, NextCursor: nextCursor}, nil
}

type scanFunction func(...interface{}) error

func scanEvent(scan scanFunction) (Event, error) {
	var event Event
	var duration sql.NullInt64
	var metadata []byte
	err := scan(
		&event.ID, &event.OccurredAt, &event.RecordedAt, &event.RequestID,
		&event.ActorID, &event.ActorKind, &event.ProjectID, &event.Category,
		&event.Action, &event.Outcome, &event.Source, &event.ResourceType,
		&event.ResourceID, &duration, &event.ErrorCode, &metadata,
	)
	if err != nil {
		return Event{}, err
	}
	if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
		return Event{}, fmt.Errorf("decode audit metadata: %w", err)
	}
	if duration.Valid {
		event.DurationMS = &duration.Int64
	}
	return event, nil
}

func decodeCursor(value string) (string, string, error) {
	if value == "" {
		return "", "", nil
	}
	cursor, err := pagination.Decode(value)
	if err != nil {
		return "", "", err
	}
	if _, err := time.Parse(time.RFC3339Nano, cursor.SortValue); err != nil {
		return "", "", err
	}
	return cursor.SortValue, cursor.ID, nil
}
