package boxcontrol

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgconn"
	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

type PostgresStore struct {
	Audit       transactionalAudit
	DB          *sql.DB
	Generator   identity.Generator
	Outbox      outbox.Writer
	Transaction transaction.Manager
}

type transactionalAudit interface {
	RecordInTransaction(context.Context, transaction.Tx, audit.Event) error
}

type scanner interface{ Scan(...interface{}) error }

func (store PostgresStore) writeEvent(ctx context.Context, tx transaction.Tx, event outbox.Event) error {
	_, err := store.Outbox.Write(ctx, tx, event)
	return err
}

func (store PostgresStore) recordMutationAudit(
	ctx context.Context,
	tx transaction.Tx,
	action, projectID, boxID string,
	metadata map[string]interface{},
	fallbackActor string,
	occurredAt time.Time,
) error {
	if store.Audit == nil {
		return nil
	}
	values := requestctx.TrustedSnapshot(ctx)
	actorID, actorKind := values.ActorID, values.ActorKind
	if actorID == "" {
		actorID, actorKind = fallbackActor, "system"
	}
	if actorKind == "" {
		actorKind = "user"
	}
	return store.Audit.RecordInTransaction(ctx, tx, audit.Event{
		Action: action, ActorID: actorID, ActorKind: actorKind,
		Category: "box", Metadata: metadata, Outcome: "success",
		ProjectID: projectID, ResourceID: boxID, ResourceType: "box",
		Source: "core", OccurredAt: occurredAt,
	})
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func mapConflict(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) &&
		(postgresError.Code == "23505" || postgresError.Code == "23503") {
		return ErrConflict
	}
	return err
}

const boxSelect = `
	SELECT box_id,owner_user_id,name,status,version,capabilities,runtimes,limits,load,
		token_id,last_heartbeat_at,offline_since,drain_requested_at,revoked_at,
		legacy_reauthorization_required,installation_id,created_at,updated_at
	FROM box_nodes
`

const taskSelect = `
	SELECT task_id,experiment_id,project_id,COALESCE(box_id::text,''),
		COALESCE(execution_epoch::text,''),status,attempt,max_attempts,
		run_spec,cancel_requested_at IS NOT NULL,COALESCE(actual_runtime,''),
		COALESCE(runtime_version,''),last_log_sequence,logs_truncated,logs_truncated_at,
		exit_code,COALESCE(error_code,''),COALESCE(error_message,''),
		COALESCE(failure_stage,''),failure_retryable,failure_cleanup_result,
		resource_usage,COALESCE(summary,''),COALESCE(execution_bundle_artifact_id::text,''),
		COALESCE(execution_bundle_version_id::text,''),COALESCE(result_manifest_sha256,''),
		created_at,started_at,finished_at,updated_at
	FROM box_tasks
`

const boxReturnColumns = `
	box_id,owner_user_id,name,status,version,capabilities,runtimes,limits,load,
	token_id,last_heartbeat_at,offline_since,drain_requested_at,revoked_at,
	legacy_reauthorization_required,installation_id,created_at,updated_at
`

const taskReturnColumns = `
	task_id,experiment_id,project_id,COALESCE(box_id::text,''),
	COALESCE(execution_epoch::text,''),status,attempt,max_attempts,
	run_spec,cancel_requested_at IS NOT NULL,COALESCE(actual_runtime,''),
	COALESCE(runtime_version,''),last_log_sequence,logs_truncated,logs_truncated_at,
	exit_code,COALESCE(error_code,''),COALESCE(error_message,''),
	COALESCE(failure_stage,''),failure_retryable,failure_cleanup_result,
	resource_usage,COALESCE(summary,''),COALESCE(execution_bundle_artifact_id::text,''),
	COALESCE(execution_bundle_version_id::text,''),COALESCE(result_manifest_sha256,''),
	created_at,started_at,finished_at,updated_at
`

func scanBox(row scanner) (Box, error) {
	var item Box
	var capabilitiesJSON, runtimesJSON, limitsJSON, loadJSON []byte
	err := row.Scan(
		&item.ID, &item.OwnerUserID, &item.Name, &item.Status, &item.Version,
		&capabilitiesJSON, &runtimesJSON, &limitsJSON, &loadJSON, &item.TokenID,
		&item.LastHeartbeatAt, &item.OfflineSince, &item.DrainRequestedAt,
		&item.RevokedAt, &item.LegacyReauthorizationRequired,
		&item.InstallationID, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Box{}, ErrNotFound
	}
	if err != nil {
		return Box{}, err
	}
	if err := json.Unmarshal(capabilitiesJSON, &item.Capabilities); err != nil {
		return Box{}, err
	}
	if err := json.Unmarshal(runtimesJSON, &item.Runtimes); err != nil {
		return Box{}, err
	}
	if err := json.Unmarshal(limitsJSON, &item.Limits); err != nil {
		return Box{}, err
	}
	if err := json.Unmarshal(loadJSON, &item.Load); err != nil {
		return Box{}, err
	}
	item.ProjectAssignments = []ProjectBinding{}
	return item, nil
}

func scanTask(row scanner) (Task, error) {
	var item Task
	var runSpecJSON, cleanupJSON, usageJSON []byte
	var errorCode, errorMessage, failureStage string
	var retryable bool
	err := row.Scan(
		&item.ID, &item.ExperimentID, &item.ProjectID, &item.BoxID,
		&item.ExecutionEpoch, &item.Status, &item.Attempt, &item.MaxAttempts,
		&runSpecJSON, &item.CancelRequested, &item.ActualRuntime,
		&item.RuntimeVersion, &item.LastLogSequence, &item.LogsTruncated,
		&item.LogsTruncatedAt, &item.ExitCode, &errorCode, &errorMessage,
		&failureStage, &retryable, &cleanupJSON, &usageJSON, &item.Summary,
		&item.ExecutionBundleArtifactID, &item.ExecutionBundleVersionID,
		&item.ResultManifestSHA256, &item.CreatedAt, &item.StartedAt,
		&item.FinishedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	if err := json.Unmarshal(runSpecJSON, &item.RunSpec); err != nil {
		return Task{}, err
	}
	if err := json.Unmarshal(usageJSON, &item.ResourceUsage); err != nil {
		return Task{}, err
	}
	if errorCode != "" {
		failedAt := item.UpdatedAt
		if item.FinishedAt != nil {
			failedAt = *item.FinishedAt
		}
		failure := &Failure{
			Stage: failureStage, Code: errorCode, Message: errorMessage,
			FailedAt: failedAt, BoxID: item.BoxID, Runtime: item.ActualRuntime,
			Attempt: item.Attempt, Retryable: retryable,
		}
		if err := json.Unmarshal(cleanupJSON, &failure.CleanupResult); err != nil {
			return Task{}, err
		}
		item.Failure = failure
	}
	return item, nil
}

func runtimeNames(runtimes []Runtime) []string {
	result := make([]string, 0, len(runtimes))
	for _, runtime := range runtimes {
		result = append(result, runtime.Name)
	}
	return result
}

func terminalTaskStatus(status string) bool {
	return status == TaskSucceeded || status == TaskFailed ||
		status == TaskCanceled || status == TaskTimedOut
}

func validTaskTransition(from, to string) bool {
	switch from {
	case TaskPreparing:
		return to == TaskRunning || to == TaskFailed ||
			to == TaskCanceled || to == TaskTimedOut
	case TaskRunning:
		return to == TaskUploading || to == TaskFailed ||
			to == TaskCanceled || to == TaskTimedOut
	case TaskUploading:
		return to == TaskFailed || to == TaskCanceled || to == TaskTimedOut
	default:
		return false
	}
}

func (store PostgresStore) CreateInTransaction(
	ctx context.Context,
	tx transaction.Tx,
	box Box,
) error {
	capabilitiesJSON, err := json.Marshal(box.Capabilities)
	if err != nil {
		return err
	}
	runtimesJSON, err := json.Marshal(box.Runtimes)
	if err != nil {
		return err
	}
	limitsJSON, err := json.Marshal(box.Limits)
	if err != nil {
		return err
	}
	loadJSON, err := json.Marshal(box.Load)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO box_nodes (
			box_id,legacy_project_id,owner_user_id,installation_id,
			name,status,version,capabilities,runtimes,limits,load,
			token_id,idempotency_key,created_by,created_at,updated_at,
			legacy_reauthorization_required
		) VALUES (
			$1,NULL,$2,$3,$4,$5,$6,$7,$8,$9,$10,
			$11,$3,$2,$12,$12,false
		)
	`, box.ID, box.OwnerUserID, box.InstallationID, box.Name, box.Status,
		box.Version, capabilitiesJSON, runtimesJSON, limitsJSON, loadJSON,
		box.TokenID, box.CreatedAt)
	if err != nil {
		return mapConflict(err)
	}
	if store.Audit != nil {
		if err := store.Audit.RecordInTransaction(ctx, tx, audit.Event{
			Action: "box.registered", ActorID: box.OwnerUserID, ActorKind: "user",
			Category: "box", Metadata: map[string]interface{}{"version": box.Version},
			Outcome: "success", ResourceID: box.ID, ResourceType: "box",
			Source: "core", OccurredAt: box.CreatedAt,
		}); err != nil {
			return err
		}
	}
	return store.writeEvent(ctx, tx, outbox.Event{
		Actor:     map[string]string{"user_id": box.OwnerUserID},
		EventType: "box.registered", Producer: "boxcontrol", OccurredAt: box.CreatedAt,
		Payload: map[string]interface{}{
			"box_id": box.ID, "owner_user_id": box.OwnerUserID, "name": box.Name,
			"status": StatusRegistering, "version": box.Version,
		},
	})
}

func (store PostgresStore) Get(ctx context.Context, boxID string) (Box, error) {
	box, err := scanBox(store.DB.QueryRowContext(
		ctx, boxSelect+" WHERE box_id=$1", boxID,
	))
	if err != nil {
		return Box{}, err
	}
	return store.hydrateBox(ctx, box)
}

func (store PostgresStore) ListOwned(ctx context.Context, ownerUserID string) ([]Box, error) {
	return store.listBoxes(
		ctx,
		boxSelect+" WHERE owner_user_id=$1 ORDER BY updated_at DESC,box_id DESC",
		ownerUserID,
	)
}

func (store PostgresStore) ListProject(ctx context.Context, projectID string) ([]Box, error) {
	return store.listBoxes(ctx, boxSelect+`
		WHERE EXISTS (
			SELECT 1 FROM box_project_bindings binding
			WHERE binding.box_id=box_nodes.box_id
			  AND binding.project_id=$1
			  AND binding.force_unbound_at IS NULL
		)
		ORDER BY updated_at DESC,box_id DESC
	`, projectID)
}

func (store PostgresStore) listBoxes(
	ctx context.Context,
	query string,
	args ...interface{},
) ([]Box, error) {
	rows, err := store.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Box{}
	for rows.Next() {
		item, err := scanBox(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range items {
		items[index], err = store.hydrateBox(ctx, items[index])
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (store PostgresStore) hydrateBox(ctx context.Context, box Box) (Box, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT project_id,box_id,assigned_by,assigned_at,force_unbound_at
		FROM box_project_bindings
		WHERE box_id=$1 AND force_unbound_at IS NULL
		ORDER BY assigned_at,project_id
	`, box.ID)
	if err != nil {
		return Box{}, err
	}
	defer rows.Close()
	box.ProjectAssignments = []ProjectBinding{}
	for rows.Next() {
		var binding ProjectBinding
		if err := rows.Scan(
			&binding.ProjectID, &binding.BoxID, &binding.AssignedBy,
			&binding.AssignedAt, &binding.ForceUnboundAt,
		); err != nil {
			return Box{}, err
		}
		box.ProjectAssignments = append(box.ProjectAssignments, binding)
	}
	return box, rows.Err()
}

func (store PostgresStore) UpdateName(
	ctx context.Context,
	boxID string,
	name string,
	now time.Time,
) (Box, error) {
	result, err := store.DB.ExecContext(ctx, `
		UPDATE box_nodes SET name=$2,updated_at=$3
		WHERE box_id=$1 AND status <> 'revoked'
	`, boxID, name, now)
	if err != nil {
		return Box{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Box{}, err
	}
	if affected == 0 {
		return Box{}, ErrNotFound
	}
	return store.Get(ctx, boxID)
}

func (store PostgresStore) UpdateHeartbeat(
	ctx context.Context,
	boxID string,
	update Box,
	now time.Time,
) (Box, bool, error) {
	capabilitiesJSON, _ := json.Marshal(update.Capabilities)
	runtimesJSON, _ := json.Marshal(update.Runtimes)
	limitsJSON, _ := json.Marshal(update.Limits)
	loadJSON, _ := json.Marshal(update.Load)
	var item Box
	recovered := false
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var previousStatus string
		var offlineSince *time.Time
		if err := tx.QueryRowContext(ctx, `
			SELECT status,offline_since
			FROM box_nodes WHERE box_id=$1 FOR UPDATE
		`, boxID).Scan(&previousStatus, &offlineSince); err != nil {
			return mapNotFound(err)
		}
		if previousStatus == StatusRevoked || previousStatus == StatusDraining {
			return ErrConflict
		}
		query := `
			UPDATE box_nodes
			SET status='online',version=$2,capabilities=$3,runtimes=$4,
				limits=$5,load=$6,last_heartbeat_at=$7,offline_since=NULL,
				updated_at=$7
			WHERE box_id=$1
			RETURNING ` + boxReturnColumns
		var err error
		item, err = scanBox(tx.QueryRowContext(
			ctx, query, boxID, update.Version, capabilitiesJSON,
			runtimesJSON, limitsJSON, loadJSON, now,
		))
		if err != nil {
			return err
		}
		recovered = previousStatus == StatusOffline
		if err := store.writeEvent(ctx, tx, outbox.Event{
			EventType: "box.heartbeat.received", Producer: "boxcontrol",
			OccurredAt: now,
			Payload: map[string]interface{}{
				"box_id": item.ID, "owner_user_id": item.OwnerUserID,
				"status": item.Status, "version": item.Version,
				"running_tasks": item.Load.RunningTasks,
				"runtimes":      runtimeNames(item.Runtimes),
			},
		}); err != nil {
			return err
		}
		if !recovered {
			return nil
		}
		offlineSeconds := int64(0)
		if offlineSince != nil {
			offlineSeconds = int64(now.Sub(*offlineSince).Seconds())
			if offlineSeconds < 0 {
				offlineSeconds = 0
			}
		}
		projectIDs, err := activeBoxProjectIDs(ctx, tx, item.ID)
		if err != nil {
			return err
		}
		return store.writeProjectBoxEvents(ctx, tx, projectIDs, outbox.Event{
			EventType: "box.recovered", Producer: "boxcontrol", OccurredAt: now,
			Payload: map[string]interface{}{
				"box_id": item.ID, "recovered_at": now,
				"offline_seconds": offlineSeconds,
			},
		})
	})
	if err != nil {
		return Box{}, false, err
	}
	item, err = store.hydrateBox(ctx, item)
	return item, recovered, err
}

func (store PostgresStore) MarkOffline(
	ctx context.Context,
	now time.Time,
	heartbeatBefore time.Time,
	limit int,
) ([]Box, error) {
	if limit < 1 {
		return nil, ErrInvalid
	}
	items := []Box{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		query := `
			WITH stale AS (
				SELECT box_id FROM box_nodes
				WHERE status='online' AND last_heartbeat_at < $2
				ORDER BY last_heartbeat_at,box_id
				FOR UPDATE SKIP LOCKED LIMIT $3
			)
			UPDATE box_nodes
			SET status='offline',offline_since=COALESCE(offline_since,$1),
				updated_at=$1
			WHERE box_id IN (SELECT box_id FROM stale)
			RETURNING ` + boxReturnColumns
		rows, err := tx.QueryContext(ctx, query, now, heartbeatBefore, limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			item, err := scanBox(rows)
			if err != nil {
				_ = rows.Close()
				return err
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, item := range items {
			activeIDs, err := activeExperimentIDs(ctx, tx, item.ID)
			if err != nil {
				return err
			}
			projectIDs, err := activeBoxProjectIDs(ctx, tx, item.ID)
			if err != nil {
				return err
			}
			if err := store.writeProjectBoxEvents(ctx, tx, projectIDs, outbox.Event{
				EventType: "box.offline", Producer: "boxcontrol", OccurredAt: now,
				Payload: map[string]interface{}{
					"box_id": item.ID, "offline_since": now,
					"active_experiment_ids": activeIDs,
				},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return items, err
}

func activeBoxProjectIDs(ctx context.Context, tx transaction.Tx, boxID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT project_id
		FROM box_project_bindings
		WHERE box_id=$1 AND force_unbound_at IS NULL
		ORDER BY project_id
	`, boxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projectIDs := []string{}
	for rows.Next() {
		var projectID string
		if err := rows.Scan(&projectID); err != nil {
			return nil, err
		}
		projectIDs = append(projectIDs, projectID)
	}
	return projectIDs, rows.Err()
}

func (store PostgresStore) writeProjectBoxEvents(
	ctx context.Context,
	tx transaction.Tx,
	projectIDs []string,
	event outbox.Event,
) error {
	if len(projectIDs) == 0 {
		return store.writeEvent(ctx, tx, event)
	}
	for _, projectID := range projectIDs {
		projectEvent := event
		projectEvent.ProjectID = projectID
		if err := store.writeEvent(ctx, tx, projectEvent); err != nil {
			return err
		}
	}
	return nil
}

func activeExperimentIDs(
	ctx context.Context,
	tx transaction.Tx,
	boxID string,
) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT experiment_id FROM box_tasks
		WHERE box_id=$1 AND status IN ('preparing','running','uploading')
		ORDER BY experiment_id
	`, boxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func (store PostgresStore) BeginDrain(
	ctx context.Context,
	boxID string,
	now time.Time,
) (Box, int, error) {
	var item Box
	active := 0
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		query := `
			UPDATE box_nodes
			SET status=CASE WHEN status='revoked' THEN status ELSE 'draining' END,
				drain_requested_at=COALESCE(drain_requested_at,$2),updated_at=$2
			WHERE box_id=$1
			RETURNING ` + boxReturnColumns
		var err error
		item, err = scanBox(tx.QueryRowContext(ctx, query, boxID, now))
		if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM box_tasks
			WHERE box_id=$1
			  AND status IN ('preparing','running','uploading','processing_result')
		`, boxID).Scan(&active); err != nil {
			return err
		}
		if err := store.recordMutationAudit(ctx, tx, "box.revoke_requested", "", boxID, map[string]interface{}{
			"mode": "drain", "active_experiment_count": active,
		}, item.OwnerUserID, now); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Box{}, 0, err
	}
	item, err = store.hydrateBox(ctx, item)
	return item, active, err
}

func (store PostgresStore) ForceRevoke(
	ctx context.Context,
	boxID string,
	now time.Time,
) (Box, []Task, error) {
	var item Box
	failed := []Task{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		projectIDs, err := activeBoxProjectIDs(ctx, tx, boxID)
		if err != nil {
			return err
		}
		boxQuery := `
			UPDATE box_nodes
			SET status='revoked',revoked_at=COALESCE(revoked_at,$2),
				offline_since=COALESCE(offline_since,$2),updated_at=$2
			WHERE box_id=$1
			RETURNING ` + boxReturnColumns
		item, err = scanBox(tx.QueryRowContext(ctx, boxQuery, boxID, now))
		if err != nil {
			return err
		}
		taskQuery := `
			UPDATE box_tasks
			SET status='failed',error_code='BOX_FORCE_REVOKED',
				error_message='Box was force-revoked by its owner',
				failure_stage='box_revocation',failure_retryable=false,
				finished_at=$2,lease_expires_at=NULL,updated_at=$2
			WHERE box_id=$1
			  AND status IN ('queued','preparing','running','uploading','processing_result')
			RETURNING ` + taskReturnColumns
		rows, err := tx.QueryContext(ctx, taskQuery, boxID, now)
		if err != nil {
			return err
		}
		for rows.Next() {
			task, err := scanTask(rows)
			if err != nil {
				_ = rows.Close()
				return err
			}
			failed = append(failed, task)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE box_project_bindings
			SET force_unbound_at=COALESCE(force_unbound_at,$2),updated_at=$2
			WHERE box_id=$1 AND force_unbound_at IS NULL
		`, boxID, now); err != nil {
			return err
		}
		for _, projectID := range projectIDs {
			if err := store.recordMutationAudit(ctx, tx, "box.revoked", projectID, boxID, map[string]interface{}{
				"mode": "force", "failed_experiment_count": failedTaskCount(failed, projectID),
			}, item.OwnerUserID, now); err != nil {
				return err
			}
			if err := store.writeEvent(ctx, tx, outbox.Event{
				EventType: "box.unassigned", Producer: "boxcontrol",
				ProjectID: projectID, OccurredAt: now,
				Payload: map[string]interface{}{
					"box_id": boxID, "project_id": projectID, "mode": "force",
					"failed_experiment_count": failedTaskCount(failed, projectID),
				},
			}); err != nil {
				return err
			}
		}
		return store.writeEvent(ctx, tx, outbox.Event{
			EventType: "box.revoked", Producer: "boxcontrol", OccurredAt: now,
			Payload: map[string]interface{}{
				"box_id": item.ID, "owner_user_id": item.OwnerUserID,
				"name": item.Name, "status": StatusRevoked, "mode": "force",
				"failed_experiment_count": len(failed),
			},
		})
	})
	return item, failed, err
}

func (store PostgresStore) FinalizeDrained(
	ctx context.Context,
	boxID string,
	now time.Time,
) (Box, bool, error) {
	var item Box
	changed := false
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var active int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM box_tasks
			WHERE box_id=$1
			  AND status IN ('preparing','running','uploading','processing_result')
		`, boxID).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return nil
		}
		projectIDs, err := activeBoxProjectIDs(ctx, tx, boxID)
		if err != nil {
			return err
		}
		query := `
			UPDATE box_nodes
			SET status='revoked',revoked_at=COALESCE(revoked_at,$2),updated_at=$2
			WHERE box_id=$1 AND status='draining'
			RETURNING ` + boxReturnColumns
		item, err = scanBox(tx.QueryRowContext(ctx, query, boxID, now))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		changed = true
		_, err = tx.ExecContext(ctx, `
			UPDATE box_project_bindings
			SET force_unbound_at=COALESCE(force_unbound_at,$2),updated_at=$2
			WHERE box_id=$1 AND force_unbound_at IS NULL
		`, boxID, now)
		if err != nil {
			return err
		}
		for _, projectID := range projectIDs {
			if err := store.recordMutationAudit(ctx, tx, "box.revoked", projectID, boxID, map[string]interface{}{
				"mode": "drain", "failed_experiment_count": 0,
			}, item.OwnerUserID, now); err != nil {
				return err
			}
			if err := store.writeEvent(ctx, tx, outbox.Event{
				EventType: "box.unassigned", Producer: "boxcontrol",
				ProjectID: projectID, OccurredAt: now,
				Payload: map[string]interface{}{
					"box_id": boxID, "project_id": projectID, "mode": "normal",
					"failed_experiment_count": 0,
				},
			}); err != nil {
				return err
			}
		}
		return store.writeEvent(ctx, tx, outbox.Event{
			EventType: "box.revoked", Producer: "boxcontrol", OccurredAt: now,
			Payload: map[string]interface{}{
				"box_id": item.ID, "owner_user_id": item.OwnerUserID,
				"name": item.Name, "status": StatusRevoked, "mode": "drain",
				"failed_experiment_count": 0,
			},
		})
	})
	return item, changed, err
}

func failedTaskCount(tasks []Task, projectID string) int {
	count := 0
	for _, task := range tasks {
		if task.ProjectID == projectID {
			count++
		}
	}
	return count
}

func (store PostgresStore) Assign(
	ctx context.Context,
	binding ProjectBinding,
) (ProjectBinding, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO box_project_bindings (
				project_id,box_id,created_at,updated_at,
				assigned_by,assigned_at,force_unbound_at
			) VALUES ($1,$2,$3,$3,$4,$3,NULL)
			ON CONFLICT (project_id,box_id) DO UPDATE
			SET assigned_by=EXCLUDED.assigned_by,
				assigned_at=EXCLUDED.assigned_at,
				force_unbound_at=NULL,updated_at=EXCLUDED.updated_at
		`, binding.ProjectID, binding.BoxID, binding.AssignedAt, binding.AssignedBy); err != nil {
			return mapConflict(err)
		}
		if err := store.recordMutationAudit(ctx, tx, "box.assigned", binding.ProjectID, binding.BoxID, map[string]interface{}{}, binding.AssignedBy, binding.AssignedAt); err != nil {
			return err
		}
		return store.writeEvent(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": binding.AssignedBy},
			EventType: "box.assigned", Producer: "boxcontrol",
			ProjectID: binding.ProjectID, OccurredAt: binding.AssignedAt,
			Payload: map[string]interface{}{
				"box_id": binding.BoxID, "project_id": binding.ProjectID,
				"assigned_by": binding.AssignedBy,
				"assigned_at": binding.AssignedAt,
			},
		})
	})
	return binding, err
}

func (store PostgresStore) Unassign(
	ctx context.Context,
	projectID string,
	boxID string,
	force bool,
	now time.Time,
) ([]Task, error) {
	failed := []Task{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var active int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM box_tasks
			WHERE project_id=$1 AND box_id=$2
			  AND status IN ('preparing','running','uploading','processing_result')
		`, projectID, boxID).Scan(&active); err != nil {
			return err
		}
		if active > 0 && !force {
			return ErrConflict
		}
		if force {
			query := `
				UPDATE box_tasks
				SET status='failed',error_code='BOX_PROJECT_FORCE_UNASSIGNED',
					error_message='Box was force-unassigned from the Project',
					failure_stage='box_revocation',failure_retryable=false,
					finished_at=$3,lease_expires_at=NULL,updated_at=$3
				WHERE project_id=$1 AND box_id=$2
				  AND status IN ('queued','preparing','running','uploading','processing_result')
				RETURNING ` + taskReturnColumns
			rows, err := tx.QueryContext(ctx, query, projectID, boxID, now)
			if err != nil {
				return err
			}
			for rows.Next() {
				task, err := scanTask(rows)
				if err != nil {
					_ = rows.Close()
					return err
				}
				failed = append(failed, task)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE box_project_bindings
			SET force_unbound_at=$3,updated_at=$3
			WHERE project_id=$1 AND box_id=$2 AND force_unbound_at IS NULL
		`, projectID, boxID, now)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrNotFound
		}
		mode := "normal"
		if force {
			mode = "force"
		}
		if err := store.recordMutationAudit(ctx, tx, "box.unassigned", projectID, boxID, map[string]interface{}{
			"mode": mode, "failed_experiment_count": len(failed),
		}, "core", now); err != nil {
			return err
		}
		return store.writeEvent(ctx, tx, outbox.Event{
			EventType: "box.unassigned", Producer: "boxcontrol",
			ProjectID: projectID, OccurredAt: now,
			Payload: map[string]interface{}{
				"box_id": boxID, "project_id": projectID, "mode": mode,
				"failed_experiment_count": len(failed),
			},
		})
	})
	return failed, err
}

func (store PostgresStore) FailOfflineTimeouts(
	ctx context.Context,
	now time.Time,
	offlineBefore time.Time,
	limit int,
) ([]Task, error) {
	if limit < 1 {
		return nil, ErrInvalid
	}
	items := []Task{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		query := `
			WITH expired AS (
				SELECT task.task_id AS expired_task_id
				FROM box_tasks task
				JOIN box_nodes box ON box.box_id=task.box_id
				WHERE task.status IN ('preparing','running','uploading')
				  AND box.status='offline'
				  AND box.offline_since IS NOT NULL
				  AND box.offline_since <= $2
				ORDER BY box.offline_since,task.updated_at,task.task_id
				FOR UPDATE OF task SKIP LOCKED
				LIMIT $3
			)
			UPDATE box_tasks task
			SET status='failed',error_code='BOX_OFFLINE_TIMEOUT',
				error_message='Box remained offline for more than 72 hours',
				failure_stage=CASE
					WHEN task.status='uploading' THEN 'result_upload'
					WHEN task.status='running' THEN 'runtime_execution'
					ELSE 'box_preparation'
				END,
				failure_retryable=false,
				failure_cleanup_result='{}'::jsonb,
				finished_at=$1,lease_expires_at=NULL,updated_at=$1
			FROM expired
			WHERE task.task_id=expired.expired_task_id
			RETURNING ` + taskReturnColumns
		rows, err := tx.QueryContext(ctx, query, now, offlineBefore, limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			item, err := scanTask(rows)
			if err != nil {
				_ = rows.Close()
				return err
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		return rows.Close()
	})
	return items, err
}

func (store PostgresStore) CreateTask(ctx context.Context, task Task) error {
	runSpecJSON, err := json.Marshal(task.RunSpec)
	if err != nil {
		return err
	}
	_, err = store.DB.ExecContext(ctx, `
		INSERT INTO box_tasks (
			task_id,experiment_id,project_id,status,attempt,max_attempts,
			run_spec,resource_usage,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,'{}'::jsonb,$8,$8)
	`, task.ID, task.ExperimentID, task.ProjectID, task.Status, task.Attempt,
		task.MaxAttempts, runSpecJSON, task.CreatedAt)
	return mapConflict(err)
}

func (store PostgresStore) GetTask(ctx context.Context, taskID string) (Task, error) {
	return scanTask(store.DB.QueryRowContext(
		ctx, taskSelect+" WHERE task_id=$1", taskID,
	))
}

func (store PostgresStore) ClaimTask(
	ctx context.Context,
	boxID string,
	now time.Time,
) (*Task, error) {
	epoch, err := store.Generator.New()
	if err != nil {
		return nil, err
	}
	var item Task
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `
			SELECT status FROM box_nodes WHERE box_id=$1 FOR UPDATE
		`, boxID).Scan(&status); err != nil {
			return mapNotFound(err)
		}
		if status != StatusOnline {
			return ErrNoTask
		}

		query := `
			WITH selected AS (
				SELECT task.task_id AS selected_task_id,
					chosen.actual_runtime AS selected_actual_runtime,
					chosen.runtime_version AS selected_runtime_version
				FROM box_tasks task
				JOIN LATERAL (
					SELECT candidate.box_id,
						CASE
							WHEN COALESCE(
								task.run_spec->>'runtime_policy',
								task.run_spec->>'runtime','auto'
							) = 'auto' THEN
								CASE
									WHEN EXISTS (
										SELECT 1 FROM jsonb_array_elements(candidate.runtimes) runtime
										WHERE runtime->>'name'='e2b'
									) THEN 'e2b'
									ELSE 'local-docker'
								END
							ELSE COALESCE(
								task.run_spec->>'runtime_policy',
								task.run_spec->>'runtime'
							)
						END AS actual_runtime,
						COALESCE((
							SELECT runtime->>'version'
							FROM jsonb_array_elements(candidate.runtimes) runtime
							WHERE runtime->>'name' = CASE
								WHEN COALESCE(
									task.run_spec->>'runtime_policy',
									task.run_spec->>'runtime','auto'
								) = 'auto' THEN
									CASE
										WHEN EXISTS (
											SELECT 1 FROM jsonb_array_elements(candidate.runtimes) preferred
											WHERE preferred->>'name'='e2b'
										) THEN 'e2b'
										ELSE 'local-docker'
									END
								ELSE COALESCE(
									task.run_spec->>'runtime_policy',
									task.run_spec->>'runtime'
								)
							END
							LIMIT 1
						),'') AS runtime_version,
						CASE
							WHEN COALESCE(
								task.run_spec->>'runtime_policy',
								task.run_spec->>'runtime','auto'
							) = 'auto'
							AND EXISTS (
								SELECT 1 FROM jsonb_array_elements(candidate.runtimes) runtime
								WHERE runtime->>'name'='e2b'
							) THEN 0
							ELSE 1
						END AS runtime_rank,
						(
							SELECT COUNT(*)::numeric
							FROM box_tasks active
							WHERE active.box_id=candidate.box_id
							  AND active.status IN ('preparing','running','uploading')
						) / GREATEST(
							COALESCE((candidate.load->>'capacity')::numeric,1),1
						) AS load_ratio
					FROM box_project_bindings binding
					JOIN box_nodes candidate ON candidate.box_id=binding.box_id
					WHERE binding.project_id=task.project_id
					  AND binding.force_unbound_at IS NULL
					  AND candidate.status='online'
					  AND (
						COALESCE(task.run_spec->>'requested_box_id','')=''
						OR task.run_spec->>'requested_box_id'=candidate.box_id::text
					  )
					  AND (
						SELECT COUNT(*)
						FROM box_tasks active
						WHERE active.box_id=candidate.box_id
						  AND active.status IN ('preparing','running','uploading')
					  ) < GREATEST(COALESCE((candidate.load->>'capacity')::int,1),1)
					  AND COALESCE((candidate.limits->>'cpu_millis')::bigint,0) >=
						COALESCE((task.run_spec->'limits'->>'cpu_millis')::bigint,0)
					  AND COALESCE((candidate.limits->>'memory_bytes')::bigint,0) >=
						COALESCE((task.run_spec->'limits'->>'memory_bytes')::bigint,0)
					  AND COALESCE((candidate.limits->>'timeout_seconds')::int,0) >=
						COALESCE((task.run_spec->'limits'->>'timeout_seconds')::int,0)
					  AND COALESCE((candidate.limits->>'disk_bytes')::bigint,0) >=
						COALESCE((task.run_spec->'limits'->>'disk_bytes')::bigint,0)
					  AND COALESCE((candidate.limits->>'pids')::int,0) >=
						COALESCE((task.run_spec->'limits'->>'pids')::int,0)
					  AND (
						COALESCE(task.run_spec->>'runtime_policy',task.run_spec->>'runtime','auto')='auto'
						AND EXISTS (
							SELECT 1 FROM jsonb_array_elements(candidate.runtimes) runtime
							WHERE runtime->>'name' IN ('e2b','local-docker')
						)
						OR EXISTS (
							SELECT 1 FROM jsonb_array_elements(candidate.runtimes) runtime
							WHERE runtime->>'name'=COALESCE(
								task.run_spec->>'runtime_policy',task.run_spec->>'runtime'
							)
						)
					  )
					ORDER BY runtime_rank,load_ratio,candidate.box_id
					LIMIT 1
				) chosen ON chosen.box_id=$1
				WHERE task.status='queued' AND task.cancel_requested_at IS NULL
				ORDER BY task.created_at,task.task_id
				FOR UPDATE OF task SKIP LOCKED
				LIMIT 1
			)
			UPDATE box_tasks task
			SET status='preparing',box_id=$1,execution_epoch=$2::uuid,
				attempt=task.attempt+1,actual_runtime=selected.selected_actual_runtime,
				runtime_version=selected.selected_runtime_version,claimed_at=$3,
				started_at=COALESCE(task.started_at,$3),last_callback_at=$3,
				lease_expires_at=NULL,updated_at=$3
			FROM selected
			WHERE task.task_id=selected.selected_task_id
			RETURNING ` + taskReturnColumns
		var scanErr error
		item, scanErr = scanTask(tx.QueryRowContext(ctx, query, boxID, epoch, now))
		if errors.Is(scanErr, ErrNotFound) {
			return ErrNoTask
		}
		return scanErr
	})
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (store PostgresStore) ResumeTask(
	ctx context.Context,
	boxID string,
	taskID string,
	request ResumeRequest,
	now time.Time,
) (Resume, error) {
	result := Resume{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		item, err := scanTask(tx.QueryRowContext(
			ctx, taskSelect+" WHERE task_id=$1 FOR UPDATE", taskID,
		))
		if err != nil {
			return err
		}
		if item.BoxID != boxID {
			return ErrNotFound
		}
		result.AcceptedPhase = item.Status
		result.AcceptedThroughSequence = item.LastLogSequence
		if item.ExecutionEpoch != request.ExecutionEpoch {
			result.Action = "cleanup"
			return nil
		}
		switch item.Status {
		case TaskFailed, TaskTimedOut:
			result.Action = "stop_failed"
		case TaskCanceled:
			result.Action = "stop_canceled"
		case TaskSucceeded, TaskProcessingResult:
			result.Action = "cleanup"
		default:
			if item.CancelRequested {
				result.Action = "stop_canceled"
			} else {
				result.Action = "continue"
			}
		}
		resumeJSON, err := json.Marshal(map[string]interface{}{
			"local_phase":            request.LocalPhase,
			"last_local_sequence":    request.LastLocalSequence,
			"bundle_state":           request.BundleState,
			"acknowledged_callbacks": request.AcknowledgedCallbacks,
			"action":                 result.Action,
		})
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE box_tasks
			SET resume_state=$2,last_callback_at=$3,updated_at=$3
			WHERE task_id=$1
		`, taskID, resumeJSON, now)
		return err
	})
	return result, err
}

func (store PostgresStore) CancelTask(
	ctx context.Context,
	taskID string,
	now time.Time,
) (Task, error) {
	query := `
		UPDATE box_tasks
		SET cancel_requested_at=COALESCE(cancel_requested_at,$2),
			status=CASE WHEN status='queued' THEN 'canceled' ELSE status END,
			finished_at=CASE WHEN status='queued' THEN $2 ELSE finished_at END,
			updated_at=$2
		WHERE task_id=$1
		  AND status IN ('queued','preparing','running','uploading')
		RETURNING ` + taskReturnColumns
	item, err := scanTask(store.DB.QueryRowContext(ctx, query, taskID, now))
	if err == nil {
		return item, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Task{}, err
	}
	item, err = store.GetTask(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	if terminalTaskStatus(item.Status) || item.Status == TaskProcessingResult {
		return item, nil
	}
	return Task{}, ErrConflict
}

func (store PostgresStore) AppendLogs(
	ctx context.Context,
	boxID string,
	taskID string,
	batch LogBatch,
	now time.Time,
) (LogAcknowledgement, error) {
	ack := LogAcknowledgement{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		item, err := scanTask(tx.QueryRowContext(
			ctx, taskSelect+" WHERE task_id=$1 FOR UPDATE", taskID,
		))
		if err != nil {
			return err
		}
		if item.BoxID != boxID {
			return ErrNotFound
		}
		if item.ExecutionEpoch != batch.ExecutionEpoch {
			return ErrConflict
		}
		if len(batch.Entries) > 0 && batch.FirstSequence > item.LastLogSequence+1 {
			return ErrConflict
		}
		late := terminalTaskStatus(item.Status)
		for index := range batch.Entries {
			entry := batch.Entries[index]
			if entry.ID == "" {
				entry.ID, err = store.Generator.New()
				if err != nil {
					return err
				}
			}
			fieldsJSON, err := json.Marshal(entry.Fields)
			if err != nil {
				return err
			}
			level := "info"
			if entry.Stream == "stderr" {
				level = "error"
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO box_task_logs (
					log_id,task_id,experiment_id,level,message,fields,occurred_at,
					execution_epoch,sequence,stream,received_at,late_after_failure
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::uuid,$9,$10,$11,$12)
				ON CONFLICT (task_id,execution_epoch,sequence)
				WHERE execution_epoch IS NOT NULL DO NOTHING
			`, entry.ID, taskID, item.ExperimentID, level, entry.Message,
				fieldsJSON, entry.OccurredAt, batch.ExecutionEpoch, entry.Sequence,
				entry.Stream, now, late)
			if err != nil {
				return err
			}
		}
		lastSequence := item.LastLogSequence
		if len(batch.Entries) > 0 {
			end := batch.FirstSequence + int64(len(batch.Entries)) - 1
			if end > lastSequence {
				lastSequence = end
			}
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE box_tasks
			SET last_log_sequence=$2,
				logs_truncated=logs_truncated OR $3,
				logs_truncated_at=CASE
					WHEN $3 THEN COALESCE(logs_truncated_at,$4)
					ELSE logs_truncated_at
				END,
				last_callback_at=$5,updated_at=$5
			WHERE task_id=$1
		`, taskID, lastSequence, batch.LogsTruncated, batch.TruncatedAt, now)
		if err != nil {
			return err
		}
		ack.AcceptedThroughSequence = lastSequence
		return nil
	})
	return ack, err
}

func (store PostgresStore) ReportStatus(
	ctx context.Context,
	boxID string,
	taskID string,
	executionEpoch string,
	status string,
	occurredAt time.Time,
	exitCode *int,
	failure *Failure,
	usage map[string]interface{},
	summary string,
) (Task, error) {
	var result Task
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		current, err := scanTask(tx.QueryRowContext(
			ctx, taskSelect+" WHERE task_id=$1 FOR UPDATE", taskID,
		))
		if err != nil {
			return err
		}
		if current.BoxID != boxID {
			return ErrNotFound
		}
		if current.ExecutionEpoch != executionEpoch {
			return ErrConflict
		}
		if terminalTaskStatus(current.Status) || current.Status == TaskProcessingResult {
			result = current
			return nil
		}
		if current.Status != status && !validTaskTransition(current.Status, status) {
			return ErrConflict
		}
		usageJSON, err := json.Marshal(usage)
		if err != nil {
			return err
		}
		failureStage, failureCode, failureMessage := "", "", ""
		failureRetryable := false
		cleanupJSON := []byte(`{}`)
		if failure != nil {
			failureStage, failureCode, failureMessage = failure.Stage, failure.Code, failure.Message
			failureRetryable = failure.Retryable
			cleanupJSON, err = json.Marshal(failure.CleanupResult)
			if err != nil {
				return err
			}
		}
		query := `
			UPDATE box_tasks
			SET status=$4,exit_code=$5,error_code=NULLIF($6,''),
				error_message=NULLIF($7,''),failure_stage=NULLIF($8,''),
				failure_retryable=$9,failure_cleanup_result=$10,
				resource_usage=$11,summary=NULLIF($12,''),last_callback_at=$13,
				lease_expires_at=CASE
					WHEN $4 IN ('failed','canceled','timed_out') THEN NULL
					ELSE lease_expires_at
				END,
				finished_at=CASE
					WHEN $4 IN ('failed','canceled','timed_out') THEN $13
					ELSE finished_at
				END,
				updated_at=$13
			WHERE task_id=$1 AND box_id=$2 AND execution_epoch=$3::uuid
			RETURNING ` + taskReturnColumns
		result, err = scanTask(tx.QueryRowContext(
			ctx, query, taskID, boxID, executionEpoch, status, exitCode,
			failureCode, failureMessage, failureStage, failureRetryable,
			cleanupJSON, usageJSON, summary, occurredAt,
		))
		return err
	})
	return result, err
}

func (store PostgresStore) SubmitResult(
	ctx context.Context,
	boxID string,
	taskID string,
	result Result,
	now time.Time,
) (Task, error) {
	var item Task
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		current, err := scanTask(tx.QueryRowContext(
			ctx, taskSelect+" WHERE task_id=$1 FOR UPDATE", taskID,
		))
		if err != nil {
			return err
		}
		if current.BoxID != boxID {
			return ErrNotFound
		}
		if current.ExecutionEpoch != result.ExecutionEpoch {
			return ErrConflict
		}
		if current.Status == TaskProcessingResult || current.Status == TaskSucceeded {
			if current.ExecutionBundleArtifactID == result.ExecutionBundle.ArtifactID &&
				current.ExecutionBundleVersionID == result.ExecutionBundle.VersionID &&
				current.ResultManifestSHA256 == result.ManifestSHA256 {
				item = current
				return nil
			}
			return ErrConflict
		}
		if current.Status != TaskUploading {
			return ErrConflict
		}
		artifactJSON, err := json.Marshal(result.ExecutionBundle)
		if err != nil {
			return err
		}
		manifestJSON, err := json.Marshal(map[string]interface{}{
			"sha256": result.ManifestSHA256,
		})
		if err != nil {
			return err
		}
		query := `
			UPDATE box_tasks
			SET status='processing_result',execution_bundle_artifact_id=$4::uuid,
				execution_bundle_version_id=$5::uuid,result_manifest_sha256=$6,
				result_artifact=$7,result_manifest=$8,last_callback_at=$9,
				lease_expires_at=NULL,updated_at=$9
			WHERE task_id=$1 AND box_id=$2 AND execution_epoch=$3::uuid
			RETURNING ` + taskReturnColumns
		item, err = scanTask(tx.QueryRowContext(
			ctx, query, taskID, boxID, result.ExecutionEpoch,
			result.ExecutionBundle.ArtifactID, result.ExecutionBundle.VersionID,
			result.ManifestSHA256, artifactJSON, manifestJSON, now,
		))
		return err
	})
	return item, err
}

func (store PostgresStore) ListLogs(
	ctx context.Context,
	taskID string,
	afterSequence int64,
	limit int,
) ([]Log, bool, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT log_id,task_id,experiment_id,COALESCE(execution_epoch::text,''),
			sequence,COALESCE(stream,'system'),message,fields,occurred_at,
			COALESCE(received_at,occurred_at),late_after_failure
		FROM box_task_logs
		WHERE task_id=$1 AND sequence > $2
		ORDER BY sequence,log_id
		LIMIT $3
	`, taskID, afterSequence, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]Log, 0, limit+1)
	for rows.Next() {
		var item Log
		var fieldsJSON []byte
		if err := rows.Scan(
			&item.ID, &item.TaskID, &item.ExperimentID, &item.ExecutionEpoch,
			&item.Sequence, &item.Stream, &item.Message, &fieldsJSON,
			&item.OccurredAt, &item.ReceivedAt, &item.LateAfterFailure,
		); err != nil {
			return nil, false, err
		}
		if err := json.Unmarshal(fieldsJSON, &item.Fields); err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

// TailLogs returns the newest bounded log window in ascending display order.
// It is intentionally separate from cursor-based ListLogs so live resume keeps
// its forward-only acknowledgement semantics.
func (store PostgresStore) TailLogs(
	ctx context.Context,
	taskID string,
	limit int,
) ([]Log, bool, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT log_id,task_id,experiment_id,execution_epoch,sequence,stream,
		       message,fields,occurred_at,received_at,late_after_failure
		FROM (
			SELECT log_id,task_id,experiment_id,COALESCE(execution_epoch::text,'') AS execution_epoch,
			       sequence,COALESCE(stream,'system') AS stream,message,fields,occurred_at,
			       COALESCE(received_at,occurred_at) AS received_at,late_after_failure
			FROM box_task_logs
			WHERE task_id=$1
			ORDER BY sequence DESC,log_id DESC
			LIMIT $2
		) AS latest
		ORDER BY sequence,log_id
	`, taskID, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]Log, 0, limit+1)
	for rows.Next() {
		var item Log
		var fieldsJSON []byte
		if err := rows.Scan(
			&item.ID, &item.TaskID, &item.ExperimentID, &item.ExecutionEpoch,
			&item.Sequence, &item.Stream, &item.Message, &fieldsJSON,
			&item.OccurredAt, &item.ReceivedAt, &item.LateAfterFailure,
		); err != nil {
			return nil, false, err
		}
		if err := json.Unmarshal(fieldsJSON, &item.Fields); err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[1:]
	}
	return items, hasMore, nil
}
