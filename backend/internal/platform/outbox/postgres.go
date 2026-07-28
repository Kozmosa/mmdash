package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PostgresStore owns publication, delivery, failure, idempotency, and replay records.
type PostgresStore struct {
	Clock       clock.Clock
	DB          *sql.DB
	Generator   identity.Generator
	Transaction transaction.Manager
}

// ClaimEvent leases the next pending Outbox event without blocking peers.
func (store PostgresStore) ClaimEvent(
	ctx context.Context,
	owner string,
	lease time.Duration,
) (*Record, error) {
	now := store.Clock.Now().UTC()
	var claimed *Record
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE system_outbox
			SET status = CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'failed' END,
			    available_at = CASE WHEN attempts < max_attempts THEN $1::timestamptz ELSE available_at END,
			    failed_at = CASE WHEN attempts < max_attempts THEN failed_at ELSE $1::timestamptz END,
			    last_error = 'Publisher lease expired',
			    locked_by = NULL,
			    lease_expires_at = NULL
			WHERE status = 'publishing' AND lease_expires_at <= $1
		`, now); err != nil {
			return err
		}
		record, err := scanRecord(tx.QueryRowContext(ctx, claimEventQuery, now).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		leaseExpiresAt := now.Add(lease)
		record, err = scanRecord(tx.QueryRowContext(ctx, `
			UPDATE system_outbox
			SET status = 'publishing',
			    attempts = attempts + 1,
			    locked_by = $2,
			    lease_expires_at = $3,
			    last_error = NULL
			WHERE event_id = $1
			RETURNING `+recordColumns,
			record.Envelope.EventID, owner, leaseExpiresAt).Scan)
		if err != nil {
			return err
		}
		claimed = &record
		return nil
	})
	return claimed, wrapDelivery("claim outbox event", err)
}

const claimEventQuery = `
	SELECT ` + recordColumns + `
	FROM system_outbox
	WHERE status = 'pending'
	  AND available_at <= $1
	  AND attempts < max_attempts
	ORDER BY available_at, occurred_at, event_id
	FOR UPDATE SKIP LOCKED
	LIMIT 1`

// Publish creates one live delivery per matching consumer and marks the event published.
func (store PostgresStore) Publish(
	ctx context.Context,
	record Record,
	owner string,
	consumerNames []string,
) error {
	now := store.Clock.Now().UTC()
	deliveryIDs := make([]string, len(consumerNames))
	for index := range consumerNames {
		deliveryID, err := store.Generator.New()
		if err != nil {
			return err
		}
		deliveryIDs[index] = deliveryID
	}
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		for index, consumerName := range consumerNames {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO system_event_deliveries (
					delivery_id, event_id, consumer_name, delivery_key,
					status, available_at, created_at, updated_at
				) VALUES ($1, $2, $3, 'live', 'pending', $4, $4, $4)
				ON CONFLICT (event_id, consumer_name, delivery_key) DO NOTHING
			`, deliveryIDs[index], record.Envelope.EventID, consumerName, now); err != nil {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE system_outbox
			SET status = 'published',
			    published_at = $3,
			    locked_by = NULL,
			    lease_expires_at = NULL,
			    last_error = NULL
			WHERE event_id = $1 AND status = 'publishing' AND locked_by = $2
		`, record.Envelope.EventID, owner, now)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrLeaseLost
		}
		return nil
	})
}

// FailEvent releases or terminally fails an Outbox publication lease.
func (store PostgresStore) FailEvent(
	ctx context.Context,
	record Record,
	owner string,
	message string,
	retryDelay time.Duration,
) error {
	now := store.Clock.Now().UTC()
	result, err := store.DB.ExecContext(ctx, `
		UPDATE system_outbox
		SET status = CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'failed' END,
		    available_at = CASE WHEN attempts < max_attempts THEN $4::timestamptz ELSE available_at END,
		    failed_at = CASE WHEN attempts < max_attempts THEN failed_at ELSE $3::timestamptz END,
		    last_error = $5,
		    locked_by = NULL,
		    lease_expires_at = NULL
		WHERE event_id = $1 AND status = 'publishing' AND locked_by = $2
	`, record.Envelope.EventID, owner, now, now.Add(retryDelay), safeError(message))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// ClaimDelivery leases the next pending consumer delivery without blocking peers.
func (store PostgresStore) ClaimDelivery(
	ctx context.Context,
	owner string,
	lease time.Duration,
) (*Delivery, error) {
	now := store.Clock.Now().UTC()
	var claimed *Delivery
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE system_event_deliveries
			SET status = CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'failed' END,
			    available_at = CASE WHEN attempts < max_attempts THEN $1::timestamptz ELSE available_at END,
			    completed_at = CASE WHEN attempts < max_attempts THEN completed_at ELSE $1::timestamptz END,
			    last_error = 'Consumer lease expired',
			    locked_by = NULL,
			    lease_expires_at = NULL,
			    updated_at = $1
			WHERE status = 'processing' AND lease_expires_at <= $1
		`, now); err != nil {
			return err
		}
		delivery, err := scanDelivery(tx.QueryRowContext(ctx, claimDeliveryQuery, now).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		leaseExpiresAt := now.Add(lease)
		result, err := tx.ExecContext(ctx, `
			UPDATE system_event_deliveries
			SET status = 'processing',
			    attempts = attempts + 1,
			    locked_by = $2,
			    lease_expires_at = $3,
			    last_error = NULL,
			    updated_at = $4
			WHERE delivery_id = $1
		`, delivery.ID, owner, leaseExpiresAt, now)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrLeaseLost
		}
		delivery.Attempts++
		delivery.Status = "processing"
		delivery.LockedBy = owner
		delivery.LeaseExpiresAt = &leaseExpiresAt
		delivery.UpdatedAt = now
		claimed = &delivery
		return nil
	})
	return claimed, wrapDelivery("claim event delivery", err)
}

const claimDeliveryQuery = `
	SELECT ` + deliveryColumns + `, ` + envelopeColumns + `
	FROM system_event_deliveries AS delivery
	JOIN system_outbox AS event USING (event_id)
	WHERE delivery.status = 'pending'
	  AND delivery.available_at <= $1
	  AND delivery.attempts < delivery.max_attempts
	ORDER BY delivery.available_at, delivery.created_at, delivery.delivery_id
	FOR UPDATE OF delivery SKIP LOCKED
	LIMIT 1`

// CompleteDelivery atomically records consumer idempotency and success.
func (store PostgresStore) CompleteDelivery(
	ctx context.Context,
	deliveryID string,
	owner string,
) error {
	now := store.Clock.Now().UTC()
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE system_event_deliveries
			SET status = 'succeeded',
			    completed_at = $3,
			    locked_by = NULL,
			    lease_expires_at = NULL,
			    last_error = NULL,
			    updated_at = $3
			WHERE delivery_id = $1 AND status = 'processing' AND locked_by = $2
		`, deliveryID, owner, now)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrLeaseLost
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO system_event_consumptions (
				event_id, consumer_name, delivery_key, delivery_id, consumed_at
			)
			SELECT event_id, consumer_name, delivery_key, delivery_id, $2
			FROM system_event_deliveries
			WHERE delivery_id = $1
			ON CONFLICT (event_id, consumer_name, delivery_key) DO NOTHING
		`, deliveryID, now)
		return err
	})
}

// FailDelivery records an attempt failure and applies retry policy.
func (store PostgresStore) FailDelivery(
	ctx context.Context,
	delivery Delivery,
	message string,
	retryDelay time.Duration,
) error {
	failureID, err := store.Generator.New()
	if err != nil {
		return err
	}
	now := store.Clock.Now().UTC()
	message = safeError(message)
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE system_event_deliveries
			SET status = CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'failed' END,
			    available_at = CASE WHEN attempts < max_attempts THEN $4::timestamptz ELSE available_at END,
			    completed_at = CASE WHEN attempts < max_attempts THEN NULL::timestamptz ELSE $3::timestamptz END,
			    locked_by = NULL,
			    lease_expires_at = NULL,
			    last_error = $5,
			    updated_at = $3
			WHERE delivery_id = $1 AND status = 'processing' AND locked_by = $2
		`, delivery.ID, delivery.LockedBy, now, now.Add(retryDelay), message)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrLeaseLost
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO system_event_failures (
				failure_id, delivery_id, event_id, consumer_name,
				delivery_key, attempt, error_message, failed_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, failureID, delivery.ID, delivery.Envelope.EventID, delivery.ConsumerName,
			delivery.DeliveryKey, delivery.Attempts, message, now)
		return err
	})
}

// CreateReplay creates explicit replay deliveries with a fresh idempotency key.
func (store PostgresStore) CreateReplay(
	ctx context.Context,
	eventID string,
	requestedConsumer string,
	consumerNames []string,
	requestedBy string,
	reason string,
) (Replay, error) {
	if len(consumerNames) == 0 {
		return Replay{}, ErrNoConsumers
	}
	replayID, err := store.Generator.New()
	if err != nil {
		return Replay{}, err
	}
	deliveryIDs := make([]string, len(consumerNames))
	for index := range consumerNames {
		deliveryIDs[index], err = store.Generator.New()
		if err != nil {
			return Replay{}, err
		}
	}
	now := store.Clock.Now().UTC()
	replay := Replay{
		ConsumerName: requestedConsumer,
		CreatedAt:    now,
		EventID:      eventID,
		ID:           replayID,
		Reason:       strings.TrimSpace(reason),
		RequestedBy:  requestedBy,
	}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM system_outbox WHERE event_id = $1)
		`, eventID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO system_event_replays (
				replay_id, event_id, consumer_name, requested_by, reason, created_at
			) VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6)
		`, replay.ID, eventID, requestedConsumer, requestedBy, replay.Reason, now); err != nil {
			return err
		}
		for index, consumerName := range consumerNames {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO system_event_deliveries (
					delivery_id, event_id, consumer_name, delivery_key,
					status, available_at, created_at, updated_at
				) VALUES ($1, $2, $3, $4, 'pending', $5, $5, $5)
			`, deliveryIDs[index], eventID, consumerName, replay.ID, now); err != nil {
				return err
			}
		}
		return nil
	})
	return replay, err
}

// GetState returns an event and every live or replay delivery.
func (store PostgresStore) GetState(ctx context.Context, eventID string) (State, error) {
	record, err := scanRecord(store.DB.QueryRowContext(ctx, `
		SELECT `+recordColumns+` FROM system_outbox WHERE event_id = $1
	`, eventID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return State{}, ErrNotFound
	}
	if err != nil {
		return State{}, err
	}
	rows, err := store.DB.QueryContext(ctx, `
		SELECT `+deliveryColumns+`
		FROM system_event_deliveries AS delivery
		WHERE event_id = $1
		ORDER BY created_at, delivery_id
	`, eventID)
	if err != nil {
		return State{}, err
	}
	defer rows.Close()
	deliveries := []Delivery{}
	for rows.Next() {
		delivery, err := scanDeliveryOnly(rows.Scan)
		if err != nil {
			return State{}, err
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return State{}, err
	}
	failures, err := store.listFailures(ctx, eventID)
	if err != nil {
		return State{}, err
	}
	replays, err := store.listReplays(ctx, eventID)
	if err != nil {
		return State{}, err
	}
	return State{
		Deliveries: deliveries,
		Failures:   failures,
		Record:     record,
		Replays:    replays,
	}, nil
}

func (store PostgresStore) listFailures(ctx context.Context, eventID string) ([]Failure, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT failure_id, delivery_id, event_id, consumer_name, delivery_key,
		       attempt, error_message, failed_at
		FROM system_event_failures
		WHERE event_id = $1
		ORDER BY failed_at, failure_id
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	failures := []Failure{}
	for rows.Next() {
		var failure Failure
		if err := rows.Scan(
			&failure.ID,
			&failure.DeliveryID,
			&failure.EventID,
			&failure.ConsumerName,
			&failure.DeliveryKey,
			&failure.Attempt,
			&failure.ErrorMessage,
			&failure.FailedAt,
		); err != nil {
			return nil, err
		}
		failures = append(failures, failure)
	}
	return failures, rows.Err()
}

func (store PostgresStore) listReplays(ctx context.Context, eventID string) ([]Replay, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT replay_id, event_id, COALESCE(consumer_name, ''),
		       requested_by, reason, created_at
		FROM system_event_replays
		WHERE event_id = $1
		ORDER BY created_at, replay_id
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	replays := []Replay{}
	for rows.Next() {
		var replay Replay
		if err := rows.Scan(
			&replay.ID,
			&replay.EventID,
			&replay.ConsumerName,
			&replay.RequestedBy,
			&replay.Reason,
			&replay.CreatedAt,
		); err != nil {
			return nil, err
		}
		replays = append(replays, replay)
	}
	return replays, rows.Err()
}

const recordColumns = `
	event_id, event_type, schema_version, occurred_at, producer,
	project_id, actor, correlation_id, causation_id, payload,
	status, attempts, max_attempts, available_at, published_at,
	COALESCE(locked_by, ''), lease_expires_at, failed_at,
	COALESCE(last_error, '')`

const deliveryColumns = `
	delivery.delivery_id, delivery.consumer_name, delivery.delivery_key,
	delivery.status, delivery.attempts, delivery.max_attempts,
	delivery.available_at, COALESCE(delivery.locked_by, ''),
	delivery.lease_expires_at, COALESCE(delivery.last_error, ''),
	delivery.completed_at, delivery.created_at, delivery.updated_at`

const envelopeColumns = `
	event.event_id, event.event_type, event.schema_version, event.occurred_at,
	event.producer, event.project_id, event.actor, event.correlation_id,
	event.causation_id, event.payload`

type scanFunction func(...interface{}) error

func scanRecord(scan scanFunction) (Record, error) {
	var record Record
	err := scanEnvelopeAndRecord(scan, &record.Envelope, &record)
	return record, err
}

func scanEnvelopeAndRecord(
	scan scanFunction,
	envelope *contract.EventEnvelope,
	record *Record,
) error {
	var actor []byte
	var payload []byte
	var projectID sql.NullString
	var correlationID sql.NullString
	var causationID sql.NullString
	err := scan(
		&envelope.EventID,
		&envelope.EventType,
		&envelope.SchemaVersion,
		&envelope.OccurredAt,
		&envelope.Producer,
		&projectID,
		&actor,
		&correlationID,
		&causationID,
		&payload,
		&record.Status,
		&record.Attempts,
		&record.MaxAttempts,
		&record.AvailableAt,
		&record.PublishedAt,
		&record.LockedBy,
		&record.LeaseExpiresAt,
		&record.FailedAt,
		&record.LastError,
	)
	if err != nil {
		return err
	}
	setEnvelopeOptionals(envelope, projectID, correlationID, causationID)
	if len(actor) > 0 && string(actor) != "null" {
		if err := json.Unmarshal(actor, &envelope.Actor); err != nil {
			return fmt.Errorf("decode event actor: %w", err)
		}
	}
	if err := json.Unmarshal(payload, &envelope.Payload); err != nil {
		return fmt.Errorf("decode event payload: %w", err)
	}
	return envelope.Validate()
}

func scanDelivery(scan scanFunction) (Delivery, error) {
	var delivery Delivery
	var actor []byte
	var payload []byte
	var projectID sql.NullString
	var correlationID sql.NullString
	var causationID sql.NullString
	err := scan(
		&delivery.ID,
		&delivery.ConsumerName,
		&delivery.DeliveryKey,
		&delivery.Status,
		&delivery.Attempts,
		&delivery.MaxAttempts,
		&delivery.AvailableAt,
		&delivery.LockedBy,
		&delivery.LeaseExpiresAt,
		&delivery.LastError,
		&delivery.CompletedAt,
		&delivery.CreatedAt,
		&delivery.UpdatedAt,
		&delivery.Envelope.EventID,
		&delivery.Envelope.EventType,
		&delivery.Envelope.SchemaVersion,
		&delivery.Envelope.OccurredAt,
		&delivery.Envelope.Producer,
		&projectID,
		&actor,
		&correlationID,
		&causationID,
		&payload,
	)
	if err != nil {
		return Delivery{}, err
	}
	setEnvelopeOptionals(&delivery.Envelope, projectID, correlationID, causationID)
	if len(actor) > 0 && string(actor) != "null" {
		if err := json.Unmarshal(actor, &delivery.Envelope.Actor); err != nil {
			return Delivery{}, err
		}
	}
	if err := json.Unmarshal(payload, &delivery.Envelope.Payload); err != nil {
		return Delivery{}, err
	}
	return delivery, delivery.Envelope.Validate()
}

func scanDeliveryOnly(scan scanFunction) (Delivery, error) {
	var delivery Delivery
	err := scan(
		&delivery.ID,
		&delivery.ConsumerName,
		&delivery.DeliveryKey,
		&delivery.Status,
		&delivery.Attempts,
		&delivery.MaxAttempts,
		&delivery.AvailableAt,
		&delivery.LockedBy,
		&delivery.LeaseExpiresAt,
		&delivery.LastError,
		&delivery.CompletedAt,
		&delivery.CreatedAt,
		&delivery.UpdatedAt,
	)
	return delivery, err
}

func setEnvelopeOptionals(
	envelope *contract.EventEnvelope,
	projectID sql.NullString,
	correlationID sql.NullString,
	causationID sql.NullString,
) {
	if projectID.Valid {
		envelope.ProjectID = &projectID.String
	}
	if correlationID.Valid {
		envelope.CorrelationID = &correlationID.String
	}
	if causationID.Valid {
		envelope.CausationID = &causationID.String
	}
}

func safeError(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "Unknown event delivery failure"
	}
	const limit = 1000
	if len(message) > limit {
		return message[:limit]
	}
	return message
}

func wrapDelivery(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
