package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PostgresStore persists jobs, leases, worker heartbeats, logs, and results.
type PostgresStore struct {
	Clock       clock.Clock
	DB          *sql.DB
	Generator   identity.Generator
	Hooks       []LifecycleHook
	Outbox      outbox.Writer
	Transaction transaction.Manager
}

// Create inserts a queued job or returns the existing idempotent job.
func (store PostgresStore) Create(
	ctx context.Context,
	actorID string,
	input CreateInput,
) (Job, bool, error) {
	var job Job
	var created bool
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var err error
		job, created, err = store.CreateInTransaction(ctx, tx, actorID, input)
		return err
	})
	return job, created, wrap("create job", err)
}

// CreateInTransaction inserts a Job and job.created event in the caller's
// business transaction.
func (store PostgresStore) CreateInTransaction(
	ctx context.Context,
	tx transaction.Tx,
	actorID string,
	input CreateInput,
) (Job, bool, error) {
	jobID, err := store.Generator.New()
	if err != nil {
		return Job{}, false, err
	}
	now := store.Clock.Now().UTC()
	availableAt := now
	if input.AvailableAt != nil {
		availableAt = input.AvailableAt.UTC()
	}
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return Job{}, false, ErrInvalid
	}
	var job Job
	created := false
	job, err = scanJob(tx.QueryRowContext(ctx, `
		INSERT INTO jobs (
			job_id, project_id, job_type, payload, priority, idempotency_key,
			max_attempts, available_at, timeout_seconds, created_by,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, $10, $11, $11
		)
		ON CONFLICT (project_id, job_type, idempotency_key)
			WHERE idempotency_key IS NOT NULL
		DO NOTHING
		RETURNING `+jobColumns, jobID, input.ProjectID, input.JobType, payload,
		input.Priority, input.IdempotencyKey, input.MaxAttempts, availableAt,
		input.TimeoutSeconds, actorID, now).Scan)
	if errors.Is(err, sql.ErrNoRows) && input.IdempotencyKey != "" {
		job, err = scanJob(tx.QueryRowContext(ctx, `
			SELECT `+jobColumns+`
			FROM jobs
			WHERE project_id = $1 AND job_type = $2 AND idempotency_key = $3
		`, input.ProjectID, input.JobType, input.IdempotencyKey).Scan)
		return job, false, err
	}
	if err != nil {
		return Job{}, false, err
	}
	created = true
	_, err = store.Outbox.Write(ctx, tx, outbox.Event{
		Actor:     map[string]string{"user_id": actorID},
		EventType: "job.created",
		Payload: map[string]interface{}{
			"job_id":   job.ID,
			"job_type": job.JobType,
		},
		Producer:  "jobs",
		ProjectID: job.ProjectID,
	})
	return job, created, err
}

// Get returns one authoritative job.
func (store PostgresStore) Get(ctx context.Context, jobID string) (Job, error) {
	var job Job
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := recoverExpired(ctx, tx, store.Clock.Now().UTC(), jobID); err != nil {
			return err
		}
		var err error
		job, err = scanJob(tx.QueryRowContext(ctx, `
			SELECT `+jobColumns+` FROM jobs WHERE job_id = $1
		`, jobID).Scan)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return job, err
}

// Claim recovers expired work and atomically claims one eligible job.
func (store PostgresStore) Claim(ctx context.Context, input ClaimInput) (*Job, error) {
	now := store.Clock.Now().UTC()
	var claimed *Job
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := recoverExpired(ctx, tx, now, ""); err != nil {
			return err
		}
		query, args := buildClaimQuery(now, input)
		job, err := scanJob(tx.QueryRowContext(ctx, query, args...).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		leaseExpiresAt := now.Add(time.Duration(input.LeaseSeconds) * time.Second)
		updated, err := scanJob(tx.QueryRowContext(ctx, `
			UPDATE jobs
			SET status = 'running',
			    attempts = attempts + 1,
			    locked_by = $2,
			    lease_expires_at = $3,
			    started_at = COALESCE(started_at, $4),
			    timeout_at = COALESCE(timeout_at, $4 + timeout_seconds * INTERVAL '1 second'),
			    updated_at = $4
			WHERE job_id = $1
			RETURNING `+jobColumns,
			job.ID, input.WorkerID, leaseExpiresAt, now).Scan)
		if err != nil {
			return err
		}
		for _, hook := range store.Hooks {
			if err := hook.ClaimInTransaction(ctx, tx, updated); err != nil {
				return err
			}
		}
		claimed = &updated
		return nil
	})
	return claimed, wrap("claim job", err)
}

func buildClaimQuery(now time.Time, input ClaimInput) (string, []interface{}) {
	query := `
		SELECT ` + jobColumns + `
		FROM jobs AS job
		WHERE job.status = 'queued'
		  AND job.available_at <= $1
		  AND (
		    $2
		    OR EXISTS (
		      SELECT 1 FROM project_members AS member
		      WHERE member.project_id = job.project_id AND member.user_id = $3
		    )
		  )
		  AND (
		    $4::TEXT = ''
		    OR job.project_id = NULLIF($4::TEXT, '')::UUID
		  )`
	args := []interface{}{now, input.Admin, input.UserID, input.ProjectID}
	if len(input.JobTypes) > 0 {
		placeholders := make([]string, 0, len(input.JobTypes))
		for _, jobType := range input.JobTypes {
			args = append(args, jobType)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		query += " AND job.job_type IN (" + strings.Join(placeholders, ", ") + ")"
	}
	query += `
		ORDER BY job.priority DESC, job.available_at, job.created_at, job.job_id
		FOR UPDATE SKIP LOCKED
		LIMIT 1`
	return query, args
}

// Renew extends a live lease owned by the same worker.
func (store PostgresStore) Renew(
	ctx context.Context,
	jobID string,
	workerID string,
	leaseSeconds int,
) (Job, error) {
	now := store.Clock.Now().UTC()
	var job Job
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := recoverExpired(ctx, tx, now, jobID); err != nil {
			return err
		}
		var err error
		job, err = scanJob(tx.QueryRowContext(ctx, `
			UPDATE jobs
			SET lease_expires_at = $3, updated_at = $2
			WHERE job_id = $1
			  AND status = 'running'
			  AND locked_by = $4
			  AND lease_expires_at > $2
			  AND timeout_at > $2
			RETURNING `+jobColumns,
			jobID, now, now.Add(time.Duration(leaseSeconds)*time.Second), workerID).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLeaseLost
		}
		return err
	})
	return job, wrap("renew job lease", err)
}

// HeartbeatWorker upserts process liveness and capabilities.
func (store PostgresStore) HeartbeatWorker(
	ctx context.Context,
	heartbeat WorkerHeartbeat,
) error {
	now := store.Clock.Now().UTC()
	capabilities, _ := json.Marshal(heartbeat.Capabilities)
	metadata, err := json.Marshal(heartbeat.Metadata)
	if err != nil {
		return ErrInvalid
	}
	_, err = store.DB.ExecContext(ctx, `
		INSERT INTO worker_heartbeats (
			worker_id, version, capabilities, metadata,
			last_seen_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $5, $5)
		ON CONFLICT (worker_id)
		DO UPDATE SET
			version = EXCLUDED.version,
			capabilities = EXCLUDED.capabilities,
			metadata = EXCLUDED.metadata,
			last_seen_at = EXCLUDED.last_seen_at,
			updated_at = EXCLUDED.updated_at
	`, heartbeat.WorkerID, heartbeat.Version, capabilities, metadata, now)
	return wrap("record worker heartbeat", err)
}

// AppendLog inserts a log only while the worker owns a non-expired lease.
func (store PostgresStore) AppendLog(
	ctx context.Context,
	jobID string,
	workerID string,
	level string,
	message string,
	fields map[string]interface{},
) (Log, error) {
	logID, err := store.Generator.New()
	if err != nil {
		return Log{}, err
	}
	now := store.Clock.Now().UTC()
	encodedFields, err := json.Marshal(fields)
	if err != nil {
		return Log{}, ErrInvalid
	}
	var entry Log
	var rawFields []byte
	err = store.DB.QueryRowContext(ctx, `
		INSERT INTO job_logs (
			job_log_id, job_id, attempt, level, message, fields, worker_id, created_at
		)
		SELECT $1, job_id, attempts, $4, $5, $6, $3, $7
		FROM jobs
		WHERE job_id = $2
		  AND status = 'running'
		  AND locked_by = $3
		  AND lease_expires_at > $7
		  AND timeout_at > $7
		RETURNING job_log_id, attempt, level, message, fields, worker_id, created_at
	`, logID, jobID, workerID, level, message, encodedFields, now).Scan(
		&entry.ID,
		&entry.Attempt,
		&entry.Level,
		&entry.Message,
		&rawFields,
		&entry.WorkerID,
		&entry.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Log{}, ErrLeaseLost
	}
	if err == nil {
		err = json.Unmarshal(rawFields, &entry.Fields)
	}
	return entry, wrap("append job log", err)
}

// ListLogs returns deterministic append-only logs for a job.
func (store PostgresStore) ListLogs(ctx context.Context, jobID string) ([]Log, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT job_log_id, attempt, level, message, fields, worker_id, created_at
		FROM job_logs
		WHERE job_id = $1
		ORDER BY created_at, job_log_id
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := []Log{}
	for rows.Next() {
		var entry Log
		var fields []byte
		if err := rows.Scan(
			&entry.ID,
			&entry.Attempt,
			&entry.Level,
			&entry.Message,
			&fields,
			&entry.WorkerID,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(fields, &entry.Fields); err != nil {
			return nil, err
		}
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}

// Complete transitions a live lease to succeeded, cancelled, or timed_out.
func (store PostgresStore) Complete(
	ctx context.Context,
	jobID string,
	workerID string,
	result map[string]interface{},
) (Job, error) {
	encodedResult, err := json.Marshal(result)
	if err != nil {
		return Job{}, ErrInvalid
	}
	now := store.Clock.Now().UTC()
	var job Job
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		job, err = scanJob(tx.QueryRowContext(ctx, `
			UPDATE jobs
			SET status = CASE
			      WHEN cancel_requested_at IS NOT NULL THEN 'cancelled'
			      WHEN timeout_at <= $3 THEN 'timed_out'
			      ELSE 'succeeded'
			    END,
			    result = CASE
			      WHEN cancel_requested_at IS NULL AND timeout_at > $3 THEN $4
			      ELSE result
			    END,
			    finished_at = $3,
			    error_code = NULL,
			    error_message = NULL,
			    locked_by = NULL,
			    lease_expires_at = NULL,
			    updated_at = $3
			WHERE job_id = $1
			  AND status = 'running'
			  AND locked_by = $2
			  AND lease_expires_at > $3
			RETURNING `+jobColumns,
			jobID, workerID, now, encodedResult).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLeaseLost
		}
		if err != nil {
			return err
		}
		for _, hook := range store.Hooks {
			if err := hook.CompleteInTransaction(ctx, tx, job, result); err != nil {
				return err
			}
		}
		return store.writeStateEvent(ctx, tx, job, workerID)
	})
	return job, wrap("complete job", err)
}

// Fail applies cancellation/timeout precedence, retry policy, and terminal failure.
func (store PostgresStore) Fail(
	ctx context.Context,
	jobID string,
	failure Failure,
) (Job, error) {
	now := store.Clock.Now().UTC()
	availableAt := now.Add(time.Duration(failure.RetryDelaySeconds) * time.Second)
	var job Job
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		job, err := scanJob(tx.QueryRowContext(ctx, `
			UPDATE jobs
			SET status = CASE
			      WHEN cancel_requested_at IS NOT NULL THEN 'cancelled'
			      WHEN timeout_at <= $3 THEN 'timed_out'
			      WHEN $6 AND attempts < max_attempts THEN 'queued'
			      ELSE 'failed'
			    END,
			    available_at = CASE
			      WHEN cancel_requested_at IS NULL
			       AND timeout_at > $3
			       AND $6
			       AND attempts < max_attempts THEN $7
			      ELSE available_at
			    END,
			    error_code = $4,
			    error_message = $5,
			    finished_at = CASE
			      WHEN cancel_requested_at IS NULL
			       AND timeout_at > $3
			       AND $6
			       AND attempts < max_attempts THEN NULL
			      ELSE $3
			    END,
			    locked_by = NULL,
			    lease_expires_at = NULL,
			    updated_at = $3
			WHERE job_id = $1
			  AND status = 'running'
			  AND locked_by = $2
			  AND lease_expires_at > $3
			RETURNING `+jobColumns,
			jobID, failure.WorkerID, now, failure.Code, failure.Message,
			failure.Retryable, availableAt).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLeaseLost
		}
		if err != nil {
			return err
		}
		for _, hook := range store.Hooks {
			if err := hook.FailInTransaction(ctx, tx, job, failure); err != nil {
				return err
			}
		}
		return store.writeStateEvent(ctx, tx, job, failure.WorkerID)
	})
	return job, wrap("fail job", err)
}

// Cancel immediately cancels queued work or signals the current worker.
func (store PostgresStore) Cancel(
	ctx context.Context,
	actorID string,
	jobID string,
) (Job, error) {
	now := store.Clock.Now().UTC()
	var job Job
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		job, err := scanJob(tx.QueryRowContext(ctx, `
			UPDATE jobs
			SET status = CASE WHEN status = 'queued' THEN 'cancelled' ELSE status END,
			    cancel_requested_at = CASE
			      WHEN status IN ('queued', 'running') THEN COALESCE(cancel_requested_at, $2)
			      ELSE cancel_requested_at
			    END,
			    finished_at = CASE WHEN status = 'queued' THEN $2 ELSE finished_at END,
			    updated_at = CASE WHEN status IN ('queued', 'running') THEN $2 ELSE updated_at END
			WHERE job_id = $1 AND status IN ('queued', 'running')
			RETURNING `+jobColumns, jobID, now).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			job, err = scanJob(tx.QueryRowContext(ctx, `
				SELECT `+jobColumns+` FROM jobs WHERE job_id = $1
			`, jobID).Scan)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if err != nil {
			return err
		}
		_, err = store.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": actorID},
			EventType: "job.cancel.requested",
			Payload: map[string]interface{}{
				"job_id": job.ID,
				"status": job.Status,
			},
			Producer:  "jobs",
			ProjectID: job.ProjectID,
		})
		return err
	})
	return job, wrap("cancel job", err)
}

func (store PostgresStore) writeStateEvent(
	ctx context.Context,
	tx transaction.Tx,
	job Job,
	workerID string,
) error {
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{
		Actor:     map[string]string{"worker_id": workerID},
		EventType: "job." + strings.ReplaceAll(string(job.Status), "_", "."),
		Payload: map[string]interface{}{
			"attempts": job.Attempts,
			"job_id":   job.ID,
			"job_type": job.JobType,
			"status":   job.Status,
		},
		Producer:  "jobs",
		ProjectID: job.ProjectID,
	})
	return err
}

func recoverExpired(
	ctx context.Context,
	tx transaction.Tx,
	now time.Time,
	jobID string,
) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'cancelled', finished_at = $1,
		    locked_by = NULL, lease_expires_at = NULL, updated_at = $1
		WHERE status = 'running'
		  AND cancel_requested_at IS NOT NULL
		  AND lease_expires_at <= $1
		  AND ($2::TEXT = '' OR job_id = NULLIF($2::TEXT, '')::UUID)
	`, now, jobID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'timed_out', finished_at = $1,
		    locked_by = NULL, lease_expires_at = NULL, updated_at = $1
		WHERE status IN ('queued', 'running') AND timeout_at <= $1
		  AND ($2::TEXT = '' OR job_id = NULLIF($2::TEXT, '')::UUID)
	`, now, jobID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = CASE WHEN attempts < max_attempts THEN 'queued' ELSE 'failed' END,
		    available_at = CASE WHEN attempts < max_attempts THEN $1 ELSE available_at END,
		    error_code = 'LEASE_EXPIRED',
		    error_message = 'Worker lease expired before result submission',
		    finished_at = CASE WHEN attempts < max_attempts THEN NULL ELSE $1 END,
		    locked_by = NULL,
		    lease_expires_at = NULL,
		    updated_at = $1
		WHERE status = 'running' AND lease_expires_at <= $1
		  AND ($2::TEXT = '' OR job_id = NULLIF($2::TEXT, '')::UUID)
	`, now, jobID)
	return err
}

const jobColumns = `
	job_id, project_id, job_type, payload, status, priority,
	COALESCE(idempotency_key, ''), attempts, max_attempts, available_at,
	timeout_seconds, timeout_at, COALESCE(locked_by, ''), lease_expires_at,
	cancel_requested_at, started_at, finished_at, result,
	COALESCE(error_code, ''), COALESCE(error_message, ''),
	created_by, created_at, updated_at`

type scanFunction func(...interface{}) error

func scanJob(scan scanFunction) (Job, error) {
	var job Job
	var payload []byte
	var result []byte
	err := scan(
		&job.ID,
		&job.ProjectID,
		&job.JobType,
		&payload,
		&job.Status,
		&job.Priority,
		&job.IdempotencyKey,
		&job.Attempts,
		&job.MaxAttempts,
		&job.AvailableAt,
		&job.TimeoutSeconds,
		&job.TimeoutAt,
		&job.LockedBy,
		&job.LeaseExpiresAt,
		&job.CancelRequestedAt,
		&job.StartedAt,
		&job.FinishedAt,
		&result,
		&job.ErrorCode,
		&job.ErrorMessage,
		&job.CreatedBy,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return Job{}, err
	}
	if err := json.Unmarshal(payload, &job.Payload); err != nil {
		return Job{}, fmt.Errorf("decode job payload: %w", err)
	}
	if len(result) > 0 {
		if err := json.Unmarshal(result, &job.Result); err != nil {
			return Job{}, fmt.Errorf("decode job result: %w", err)
		}
	}
	return job, nil
}
