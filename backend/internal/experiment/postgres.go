package experiment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/boxcontrol"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

const defaultLimitsJSON = `{"cpu_millis":1000,"memory_bytes":1073741824,"timeout_seconds":3600,"disk_bytes":10737418240,"pids":256,"network":"disabled"}`

type PostgresStore struct {
	Audit       transactionalAudit
	DB          *sql.DB
	Generator   identity.Generator
	Jobs        jobs.TransactionalWriter
	Outbox      outbox.Writer
	Transaction transaction.Manager
}

type transactionalAudit interface {
	RecordInTransaction(context.Context, transaction.Tx, audit.Event) error
}

type scanner interface{ Scan(...interface{}) error }

func (store PostgresStore) GetSettings(ctx context.Context, projectID string) (Settings, error) {
	if _, err := store.DB.ExecContext(ctx, `
		INSERT INTO experiment_project_settings (
			project_id,timezone,default_runtime_policy,default_limits,
			git_large_file_threshold_bytes,updated_by,updated_at
		)
		SELECT project_id,'UTC','auto',$2::jsonb,52428800,created_by,updated_at
		FROM projects WHERE project_id=$1
		ON CONFLICT (project_id) DO NOTHING
	`, projectID, defaultLimitsJSON); err != nil {
		return Settings{}, err
	}
	return scanSettings(store.DB.QueryRowContext(ctx, `
		SELECT project_id,timezone,default_runtime_policy,default_limits,
			git_large_file_threshold_bytes,updated_by,updated_at
		FROM experiment_project_settings WHERE project_id=$1
	`, projectID))
}

func (store PostgresStore) UpdateSettings(
	ctx context.Context,
	projectID string,
	updatedBy string,
	patch SettingsPatch,
	now time.Time,
) (Settings, error) {
	current, err := store.GetSettings(ctx, projectID)
	if err != nil {
		return Settings{}, err
	}
	if patch.Timezone != nil {
		current.Timezone = *patch.Timezone
	}
	if patch.DefaultRuntimePolicy != nil {
		current.DefaultRuntimePolicy = *patch.DefaultRuntimePolicy
	}
	if patch.DefaultLimits != nil {
		current.DefaultLimits = *patch.DefaultLimits
	}
	if patch.GitLargeFileThresholdBytes != nil {
		current.GitLargeFileThresholdBytes = *patch.GitLargeFileThresholdBytes
	}
	limitsJSON, err := json.Marshal(current.DefaultLimits)
	if err != nil {
		return Settings{}, err
	}
	_, err = store.DB.ExecContext(ctx, `
		UPDATE experiment_project_settings
		SET timezone=$2,default_runtime_policy=$3,default_limits=$4,
			git_large_file_threshold_bytes=$5,updated_by=$6,updated_at=$7
		WHERE project_id=$1
	`, projectID, current.Timezone, current.DefaultRuntimePolicy, limitsJSON,
		current.GitLargeFileThresholdBytes, updatedBy, now)
	if err != nil {
		return Settings{}, err
	}
	return store.GetSettings(ctx, projectID)
}

func scanSettings(row scanner) (Settings, error) {
	var item Settings
	var limitsJSON []byte
	err := row.Scan(
		&item.ProjectID, &item.Timezone, &item.DefaultRuntimePolicy, &limitsJSON,
		&item.GitLargeFileThresholdBytes, &item.UpdatedBy, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{}, ErrNotFound
	}
	if err != nil {
		return Settings{}, err
	}
	if err := json.Unmarshal(limitsJSON, &item.DefaultLimits); err != nil {
		return Settings{}, err
	}
	return item, nil
}

func (store PostgresStore) Create(
	ctx context.Context,
	item Experiment,
) (Experiment, bool, error) {
	if store.Transaction.DB == nil {
		return Experiment{}, false, ErrInvalid
	}
	inserted := false
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var err error
		inserted, err = store.insertExperiment(ctx, tx, item)
		if err != nil || !inserted {
			return err
		}
		return store.recordCreated(ctx, tx, item)
	})
	if err != nil {
		return Experiment{}, false, err
	}
	if !inserted {
		item, err = store.getByIdempotency(ctx, item.ProjectID, item.IdempotencyKey)
	} else {
		item, err = store.Get(ctx, item.ProjectID, item.ID)
	}
	return item, inserted, err
}

func (store PostgresStore) CreateRerun(
	ctx context.Context,
	previous Experiment,
	next Experiment,
	now time.Time,
) (Experiment, bool, error) {
	if store.Transaction.DB == nil {
		return Experiment{}, false, ErrInvalid
	}
	inserted := false
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status, latestID string
		if err := tx.QueryRowContext(ctx, `
			SELECT execution_status,COALESCE(latest_experiment_id::text,experiment_id::text)
			FROM experiments WHERE project_id=$1 AND experiment_id=$2 FOR UPDATE
		`, previous.ProjectID, previous.ID).Scan(&status, &latestID); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if !terminalStatus(status) || status == StatusArchived || latestID != previous.ID {
			return ErrConflict
		}
		var err error
		inserted, err = store.insertExperiment(ctx, tx, next)
		if err != nil || !inserted {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE experiments
			SET latest_experiment_id=$3,updated_at=CASE
				WHEN experiment_id=$2 THEN $4 ELSE updated_at END,
				superseded_by_experiment_id=CASE
				WHEN experiment_id=$2 THEN $3 ELSE superseded_by_experiment_id END
			WHERE project_id=$1 AND root_experiment_id=$5
		`, next.ProjectID, previous.ID, next.ID, now, next.Retry.RootExperimentID); err != nil {
			return err
		}
		if err := store.recordCreated(ctx, tx, next); err != nil {
			return err
		}
		if err := store.recordMutationAudit(ctx, tx, "experiment.rerun_created", next.ProjectID, next.ID, map[string]interface{}{
			"retry_of_experiment_id": previous.ID, "retry_sequence": next.Retry.RetrySequence,
		}, next.CreatedBy, now); err != nil {
			return err
		}
		return store.writeEvent(ctx, tx, outbox.Event{
			EventType: "experiment.rerun_created", Producer: "experiment",
			ProjectID: next.ProjectID, OccurredAt: now,
			Actor: map[string]string{"user_id": next.CreatedBy},
			Payload: map[string]interface{}{
				"experiment_id":          next.ID,
				"retry_of_experiment_id": previous.ID,
				"root_experiment_id":     next.Retry.RootExperimentID,
				"retry_sequence":         next.Retry.RetrySequence,
			},
		})
	})
	if err != nil {
		return Experiment{}, false, err
	}
	if !inserted {
		next, err = store.getByIdempotency(ctx, next.ProjectID, next.IdempotencyKey)
	} else {
		next, err = store.Get(ctx, next.ProjectID, next.ID)
	}
	return next, inserted, err
}

func (store PostgresStore) insertExperiment(
	ctx context.Context,
	tx transaction.Tx,
	item Experiment,
) (bool, error) {
	parametersJSON, err := json.Marshal(item.Parameters)
	if err != nil {
		return false, err
	}
	environmentJSON, err := json.Marshal(item.Environment)
	if err != nil {
		return false, err
	}
	inputsJSON, err := json.Marshal(item.Inputs)
	if err != nil {
		return false, err
	}
	limitsJSON, err := json.Marshal(item.Limits)
	if err != nil {
		return false, err
	}
	row := tx.QueryRowContext(ctx, `
		INSERT INTO experiments (
			experiment_id,project_id,created_by,name,experiment_type,
			execution_status,connectivity_status,source_commit,entrypoint,
			parameters,environment,inputs,requested_runtime_policy,
			requested_box_id,limits,idempotency_key,max_attempts,resource_usage,
			project_timezone,result_directory,root_experiment_id,
			latest_experiment_id,retry_of_experiment_id,retry_sequence,
			cleanup_result,result_processing,created_at,updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,NULL,$7,$8,$9,$10,$11,$12,NULLIF($13,'')::uuid,
			$14,$15,$16,'{}'::jsonb,$17,$18,$19::uuid,$20::uuid,
			NULLIF($21,'')::uuid,$22,'{}'::jsonb,'{}'::jsonb,$23,$23
		)
		ON CONFLICT (project_id,idempotency_key) DO NOTHING
		RETURNING experiment_id
	`, item.ID, item.ProjectID, item.CreatedBy, item.Name, item.Type,
		item.ExecutionStatus, item.SourceCommit, item.Entrypoint, parametersJSON,
		environmentJSON, inputsJSON, item.RequestedRuntimePolicy,
		item.RequestedBoxID, limitsJSON, item.IdempotencyKey, item.MaxAttempts,
		item.ProjectTimezone, item.ResultDirectory, item.Retry.RootExperimentID,
		item.Retry.LatestExperimentID, item.Retry.RetryOfExperimentID,
		item.Retry.RetrySequence, item.CreatedAt)
	var ignored string
	if err := row.Scan(&ignored); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func (store PostgresStore) recordCreated(
	ctx context.Context,
	tx transaction.Tx,
	item Experiment,
) error {
	if store.Audit != nil {
		if err := store.Audit.RecordInTransaction(ctx, tx, audit.Event{
			Action: "experiment.created", ActorID: item.CreatedBy, ActorKind: "user",
			Category: "experiment", Outcome: "success", ProjectID: item.ProjectID,
			ResourceID: item.ID, ResourceType: "experiment", Source: "core",
			OccurredAt: item.CreatedAt,
			Metadata: map[string]interface{}{
				"experiment_type": item.Type,
				"runtime_policy":  item.RequestedRuntimePolicy,
			},
		}); err != nil {
			return err
		}
	}
	return store.writeEvent(ctx, tx, outbox.Event{
		Actor:     map[string]string{"user_id": item.CreatedBy},
		EventType: "experiment.created", Producer: "experiment",
		ProjectID: item.ProjectID, OccurredAt: item.CreatedAt,
		Payload: map[string]interface{}{
			"experiment_id": item.ID, "experiment_type": item.Type,
			"name": item.Name, "source_commit": item.SourceCommit,
			"entrypoint": item.Entrypoint, "execution_status": item.ExecutionStatus,
			"result_directory": item.ResultDirectory,
		},
	})
}

func (store PostgresStore) writeEvent(
	ctx context.Context,
	tx transaction.Tx,
	event outbox.Event,
) error {
	_, err := store.Outbox.Write(ctx, tx, event)
	return err
}

func (store PostgresStore) getByIdempotency(
	ctx context.Context,
	projectID, key string,
) (Experiment, error) {
	return store.scan(store.DB.QueryRowContext(
		ctx, store.selectSQL()+` WHERE experiment.project_id=$1 AND experiment.idempotency_key=$2`,
		projectID, key,
	))
}

func (store PostgresStore) Get(
	ctx context.Context,
	projectID, id string,
) (Experiment, error) {
	return store.scan(store.DB.QueryRowContext(
		ctx, store.selectSQL()+` WHERE experiment.project_id=$1 AND experiment.experiment_id=$2`,
		projectID, id,
	))
}

func (store PostgresStore) List(
	ctx context.Context,
	projectID, status string,
	offset, limit int,
) (Page, error) {
	rows, err := store.DB.QueryContext(ctx, store.selectSQL()+`
		WHERE experiment.project_id=$1
		  AND ($2='' OR experiment.execution_status=$2)
		ORDER BY experiment.updated_at DESC,experiment.experiment_id DESC
		OFFSET $3 LIMIT $4
	`, projectID, status, offset, limit+1)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	items := []Experiment{}
	for rows.Next() {
		item, err := store.scan(rows)
		if err != nil {
			return Page{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	next := ""
	if hasMore {
		next = fmt.Sprintf("%d", offset+limit)
	}
	return Page{Items: items, HasMore: hasMore, NextCursor: next}, nil
}

func (store PostgresStore) QueueWithTask(
	ctx context.Context,
	item Experiment,
	task boxcontrol.Task,
	idempotencyKey string,
	now time.Time,
) (Experiment, error) {
	runSpecJSON, err := json.Marshal(task.RunSpec)
	if err != nil {
		return Experiment{}, err
	}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status string
		var existingKey sql.NullString
		if err := tx.QueryRowContext(ctx, `
			SELECT execution_status,run_idempotency_key
			FROM experiments WHERE project_id=$1 AND experiment_id=$2 FOR UPDATE
		`, item.ProjectID, item.ID).Scan(&status, &existingKey); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != StatusCreated {
			if existingKey.Valid && existingKey.String == idempotencyKey {
				return nil
			}
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO box_tasks (
				task_id,experiment_id,project_id,status,attempt,max_attempts,
				run_spec,resource_usage,created_at,updated_at
			) VALUES ($1,$2,$3,'queued',0,1,$4,'{}'::jsonb,$5,$5)
		`, task.ID, item.ID, item.ProjectID, runSpecJSON, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE experiments
			SET execution_status='queued',task_id=$3,run_idempotency_key=$4,updated_at=$5
			WHERE project_id=$1 AND experiment_id=$2
		`, item.ProjectID, item.ID, task.ID, idempotencyKey, now); err != nil {
			return err
		}
		return store.recordPhase(ctx, tx, item, StatusCreated, StatusQueued, now)
	})
	if err != nil {
		return Experiment{}, err
	}
	return store.Get(ctx, item.ProjectID, item.ID)
}

func (store PostgresStore) Cancel(
	ctx context.Context,
	projectID, id string,
	now time.Time,
) (Experiment, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status, itemType, taskID string
		if err := tx.QueryRowContext(ctx, `
			SELECT execution_status,experiment_type,COALESCE(task_id::text,'')
			FROM experiments WHERE project_id=$1 AND experiment_id=$2 FOR UPDATE
		`, projectID, id).Scan(&status, &itemType, &taskID); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if terminalStatus(status) {
			return nil
		}
		if taskID != "" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE box_tasks
				SET cancel_requested_at=COALESCE(cancel_requested_at,$2),
					status=CASE WHEN status='queued' THEN 'canceled' ELSE status END,
					finished_at=CASE WHEN status='queued' THEN $2 ELSE finished_at END,
					updated_at=$2
				WHERE task_id=$1 AND status IN ('queued','preparing','running','uploading')
			`, taskID, now); err != nil {
				return err
			}
		}
		immediate := status == StatusCreated || status == StatusQueued ||
			status == StatusAwaitingResult || status == StatusVerifyingResult ||
			status == StatusProcessingResult
		if !immediate {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE experiments SET execution_status='canceled',finished_at=$3,updated_at=$3
			WHERE project_id=$1 AND experiment_id=$2
		`, projectID, id, now); err != nil {
			return err
		}
		payload := map[string]interface{}{
			"experiment_id": id, "experiment_type": itemType,
			"execution_status": StatusCanceled,
		}
		if taskID != "" {
			payload["task_id"] = taskID
		}
		return store.writeEvent(ctx, tx, outbox.Event{
			EventType: "experiment.canceled", Producer: "experiment",
			ProjectID: projectID, OccurredAt: now,
			Payload: payload,
		})
	})
	if err != nil {
		return Experiment{}, err
	}
	return store.Get(ctx, projectID, id)
}

func (store PostgresStore) Archive(
	ctx context.Context,
	projectID, id string,
	now time.Time,
) (Experiment, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status, itemType string
		if err := tx.QueryRowContext(ctx, `
			SELECT execution_status,experiment_type
			FROM experiments WHERE project_id=$1 AND experiment_id=$2 FOR UPDATE
		`, projectID, id).Scan(&status, &itemType); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status == StatusArchived {
			return nil
		}
		if !terminalStatus(status) {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE experiments SET execution_status='archived',updated_at=$3
			WHERE project_id=$1 AND experiment_id=$2
		`, projectID, id, now); err != nil {
			return err
		}
		return store.writeEvent(ctx, tx, outbox.Event{
			EventType: "experiment.archived", Producer: "experiment",
			ProjectID: projectID, OccurredAt: now,
			Payload: map[string]interface{}{
				"experiment_id": id, "experiment_type": itemType,
				"previous_status": status, "execution_status": StatusArchived,
			},
		})
	})
	if err != nil {
		return Experiment{}, err
	}
	return store.Get(ctx, projectID, id)
}

func (store PostgresStore) ApplyTaskStatus(
	ctx context.Context,
	task boxcontrol.Task,
	now time.Time,
) (Experiment, error) {
	status := taskStatus(task.Status)
	if status == "" {
		return Experiment{}, ErrInvalid
	}
	usageJSON, err := json.Marshal(task.ResourceUsage)
	if err != nil {
		return Experiment{}, err
	}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var previous, createdBy string
		if err := tx.QueryRowContext(ctx, `
			SELECT execution_status,created_by::text FROM experiments
			WHERE experiment_id=$1 AND task_id=$2 FOR UPDATE
		`, task.ExperimentID, task.ID).Scan(&previous, &createdBy); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		forceInvalidation := previous == StatusProcessingResult && status == StatusFailed && task.Failure != nil &&
			(task.Failure.Code == "BOX_FORCE_REVOKED" || task.Failure.Code == "BOX_PROJECT_FORCE_UNASSIGNED")
		if terminalStatus(previous) || (previous == StatusProcessingResult && !forceInvalidation) {
			return nil
		}
		if previous != status && !validTransition(previous, status) {
			return ErrConflict
		}
		failureStage, failureCode, failureMessage := "", "", ""
		failureRetryable := false
		failureCleanupJSON := []byte(`{}`)
		failedAt := (*time.Time)(nil)
		if task.Failure != nil {
			failureStage, failureCode, failureMessage = task.Failure.Stage, task.Failure.Code, task.Failure.Message
			failureRetryable, failedAt = task.Failure.Retryable, &task.Failure.FailedAt
			failureCleanupJSON, err = json.Marshal(task.Failure.CleanupResult)
			if err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE experiments
			SET execution_status=$3,connectivity_status=CASE
					WHEN $3 IN ('preparing','running','uploading') THEN 'online'
					ELSE connectivity_status END,
				box_id=NULLIF($4,'')::uuid,actual_runtime=NULLIF($5,''),
				runtime_version=NULLIF($6,''),exit_code=$7,
				failure_stage=NULLIF($8,''),failure_code=NULLIF($9,''),
				failure_message=NULLIF($10,''),failed_at=$11,retryable=$12,
				failure_attempt=$13,cleanup_result=$14,resource_usage=$15,
				summary=NULLIF($16,''),
				started_at=CASE WHEN $3 IN ('preparing','running')
					THEN COALESCE(started_at,$17) ELSE started_at END,
				finished_at=CASE WHEN $3 IN ('failed','canceled','timed_out')
					THEN COALESCE(finished_at,$17) ELSE finished_at END,
				updated_at=$17
			WHERE experiment_id=$1 AND task_id=$2
		`, task.ExperimentID, task.ID, status, task.BoxID, task.ActualRuntime,
			task.RuntimeVersion, task.ExitCode, failureStage, failureCode,
			failureMessage, failedAt, failureRetryable, task.Attempt,
			failureCleanupJSON, usageJSON, task.Summary, now); err != nil {
			return err
		}
		current, err := store.scan(tx.QueryRowContext(
			ctx, store.selectSQL()+` WHERE experiment.experiment_id=$1`, task.ExperimentID,
		))
		if err != nil {
			return err
		}
		return store.recordTaskPhase(ctx, tx, current, previous, task, now)
	})
	if err != nil {
		return Experiment{}, err
	}
	return store.Get(ctx, task.ProjectID, task.ExperimentID)
}

func (store PostgresStore) ApplyResult(
	ctx context.Context,
	task boxcontrol.Task,
	result boxcontrol.Result,
	now time.Time,
) (Experiment, error) {
	artifactJSON, err := json.Marshal(result.ExecutionBundle)
	if err != nil {
		return Experiment{}, err
	}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var previous, createdBy string
		if err := tx.QueryRowContext(ctx, `
			SELECT execution_status,created_by::text FROM experiments
			WHERE experiment_id=$1 AND task_id=$2 FOR UPDATE
		`, task.ExperimentID, task.ID).Scan(&previous, &createdBy); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if previous == StatusProcessingResult {
			return nil
		}
		if previous != StatusUploading {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE experiments
			SET execution_status='processing_result',
				execution_bundle_artifact_id=$3::uuid,
				execution_bundle_version_id=$4::uuid,
				result_manifest_sha256=$5,result_artifact=$6,updated_at=$7
			WHERE experiment_id=$1 AND task_id=$2
		`, task.ExperimentID, task.ID, result.ExecutionBundle.ArtifactID,
			result.ExecutionBundle.VersionID, result.ManifestSHA256, artifactJSON, now); err != nil {
			return err
		}
		if store.Jobs != nil {
			_, _, err = store.Jobs.CreateInTransaction(ctx, tx, createdBy, jobs.CreateInput{
				IdempotencyKey: "experiment-result:" + task.ExperimentID,
				JobType:        JobTypeResultProcess,
				MaxAttempts:    3,
				Payload: map[string]interface{}{
					"experiment_id":   task.ExperimentID,
					"artifact_id":     result.ExecutionBundle.ArtifactID,
					"version_id":      result.ExecutionBundle.VersionID,
					"manifest_sha256": result.ManifestSHA256,
				},
				Priority:       50,
				ProjectID:      task.ProjectID,
				TimeoutSeconds: 3600,
			})
			if err != nil {
				return err
			}
		}
		item, err := store.scan(tx.QueryRowContext(
			ctx, store.selectSQL()+` WHERE experiment.experiment_id=$1`, task.ExperimentID,
		))
		if err != nil {
			return err
		}
		return store.recordPhase(ctx, tx, item, previous, StatusProcessingResult, now)
	})
	if err != nil {
		return Experiment{}, err
	}
	return store.Get(ctx, task.ProjectID, task.ExperimentID)
}

func (store PostgresStore) BeginSelfBinding(
	ctx context.Context,
	projectID, experimentID, commitSHA, idempotencyKey string,
	now time.Time,
) (Experiment, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var itemType, status string
		var existingCommit, existingKey sql.NullString
		if err := tx.QueryRowContext(ctx, `
			SELECT experiment_type,execution_status,staging_commit_sha,
				result_bind_idempotency_key
			FROM experiments WHERE project_id=$1 AND experiment_id=$2 FOR UPDATE
		`, projectID, experimentID).Scan(
			&itemType, &status, &existingCommit, &existingKey,
		); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if itemType != TypeSelf {
			return ErrConflict
		}
		if status != StatusAwaitingResult {
			if (status == StatusVerifyingResult || status == StatusSucceeded) &&
				existingCommit.Valid && existingCommit.String == commitSHA &&
				existingKey.Valid && existingKey.String == idempotencyKey {
				return nil
			}
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE experiments
			SET execution_status='verifying_result',staging_commit_sha=$3,
				result_bind_idempotency_key=$4,updated_at=$5
			WHERE project_id=$1 AND experiment_id=$2
		`, projectID, experimentID, commitSHA, idempotencyKey, now); err != nil {
			return err
		}
		item := Experiment{ID: experimentID, ProjectID: projectID}
		return store.recordPhase(ctx, tx, item, StatusAwaitingResult, StatusVerifyingResult, now)
	})
	if err != nil {
		return Experiment{}, err
	}
	return store.Get(ctx, projectID, experimentID)
}

func (store PostgresStore) CompleteResult(
	ctx context.Context,
	projectID, experimentID string,
	result ResultVerification,
	now time.Time,
) (Experiment, error) {
	manifestJSON, err := json.Marshal(result.Manifest)
	if err != nil {
		return Experiment{}, err
	}
	processingJSON, err := json.Marshal(map[string]interface{}{"analysis": result.Analysis})
	if err != nil {
		return Experiment{}, err
	}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status, itemType, resultDirectory, taskID, bundleArtifactID, bundleVersionID string
		var stagingCommit sql.NullString
		if err := tx.QueryRowContext(ctx, `
			SELECT execution_status,experiment_type,result_directory,
				COALESCE(task_id::text,''),COALESCE(staging_commit_sha,''),
				COALESCE(execution_bundle_artifact_id::text,''),
				COALESCE(execution_bundle_version_id::text,'')
			FROM experiments WHERE project_id=$1 AND experiment_id=$2 FOR UPDATE
		`, projectID, experimentID).Scan(
			&status, &itemType, &resultDirectory, &taskID, &stagingCommit,
			&bundleArtifactID, &bundleVersionID,
		); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status == StatusSucceeded {
			return nil
		}
		if status != StatusVerifyingResult && status != StatusProcessingResult {
			return ErrConflict
		}
		if itemType == TypeSelf && (!stagingCommit.Valid || stagingCommit.String != result.CommitSHA) {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM experiment_result_files WHERE experiment_id=$1
		`, experimentID); err != nil {
			return err
		}
		for _, file := range result.Files {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO experiment_result_files (
					experiment_id,project_id,path,storage_kind,sha256,size_bytes,
					media_type,artifact_id,artifact_version_id,repository_path,created_at
				) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,
					NULLIF($9,'')::uuid,NULLIF($10,''),$11)
			`, experimentID, projectID, file.Path, file.StorageKind, file.SHA256,
				file.SizeBytes, file.MediaType, file.ArtifactID,
				file.ArtifactVersionID, file.RepositoryPath, now); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE experiments
			SET execution_status='succeeded',result_commit_sha=$3,
				result_manifest_sha256=$4,result_manifest=$5,summary=NULLIF($6,''),
				result_processing=$7,finished_at=$8,updated_at=$8
			WHERE project_id=$1 AND experiment_id=$2
		`, projectID, experimentID, result.CommitSHA, result.ManifestSHA256,
			manifestJSON, result.Summary, processingJSON, now); err != nil {
			return err
		}
		if taskID != "" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE box_tasks SET status='succeeded',finished_at=$2,updated_at=$2
				WHERE task_id=$1 AND status='processing_result'
			`, taskID, now); err != nil {
				return err
			}
		}
		if err := store.writeEvent(ctx, tx, outbox.Event{
			EventType: "experiment.result_bound", Producer: "experiment",
			ProjectID: projectID, OccurredAt: now,
			Payload: map[string]interface{}{
				"experiment_id": experimentID, "result_commit_sha": result.CommitSHA,
				"result_directory":       resultDirectory,
				"result_manifest_sha256": result.ManifestSHA256,
			},
		}); err != nil {
			return err
		}
		if err := store.recordMutationAudit(ctx, tx, "experiment.result.bound", projectID, experimentID, map[string]interface{}{
			"result_commit_sha": result.CommitSHA,
		}, "core", now); err != nil {
			return err
		}
		payload := map[string]interface{}{
			"experiment_id": experimentID, "experiment_type": itemType,
			"execution_status": StatusSucceeded, "result_commit_sha": result.CommitSHA,
			"result_directory":       resultDirectory,
			"result_manifest_sha256": result.ManifestSHA256,
		}
		if taskID != "" {
			payload["task_id"] = taskID
		}
		if bundleArtifactID != "" {
			payload["execution_bundle_artifact_id"] = bundleArtifactID
			payload["execution_bundle_version_id"] = bundleVersionID
		}
		return store.writeEvent(ctx, tx, outbox.Event{
			EventType: "experiment.succeeded", Producer: "experiment",
			ProjectID: projectID, OccurredAt: now, Payload: payload,
		})
	})
	if err != nil {
		return Experiment{}, err
	}
	return store.Get(ctx, projectID, experimentID)
}

func (store PostgresStore) FailResult(
	ctx context.Context,
	projectID, experimentID string,
	failure Failure,
	now time.Time,
) (Experiment, error) {
	cleanupJSON, err := json.Marshal(failure.CleanupResult)
	if err != nil {
		return Experiment{}, err
	}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status, taskID, boxID string
		var logsTruncated bool
		if err := tx.QueryRowContext(ctx, `
			SELECT experiment.execution_status,COALESCE(experiment.task_id::text,''),
				COALESCE(experiment.box_id::text,''),COALESCE(task.logs_truncated,false)
			FROM experiments experiment
			LEFT JOIN box_tasks task ON task.task_id=experiment.task_id
			WHERE experiment.project_id=$1 AND experiment.experiment_id=$2 FOR UPDATE OF experiment
		`, projectID, experimentID).Scan(
			&status, &taskID, &boxID, &logsTruncated,
		); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status == StatusFailed {
			return nil
		}
		if status != StatusVerifyingResult && status != StatusProcessingResult {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE experiments
			SET execution_status='failed',failure_stage=$3,failure_code=$4,
				failure_message=$5,failed_at=$6,retryable=$7,failure_attempt=$8,
				cleanup_result=$9,finished_at=$10,updated_at=$10
			WHERE project_id=$1 AND experiment_id=$2
		`, projectID, experimentID, failure.Stage, failure.Code, failure.Message,
			failure.FailedAt, failure.Retryable, failure.Attempt, cleanupJSON, now); err != nil {
			return err
		}
		if taskID != "" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE box_tasks
				SET status='failed',error_code=$2,error_message=$3,failure_stage=$4,
					failure_retryable=$5,failure_cleanup_result=$6,
					finished_at=$7,updated_at=$7
				WHERE task_id=$1 AND status='processing_result'
			`, taskID, failure.Code, failure.Message, failure.Stage,
				failure.Retryable, cleanupJSON, now); err != nil {
				return err
			}
		}
		return store.writeEvent(ctx, tx, outbox.Event{
			EventType: "experiment.failed", Producer: "experiment",
			ProjectID: projectID, OccurredAt: now,
			Payload: failurePayload(experimentID, taskID, boxID, StatusFailed, failure, logsTruncated, nil),
		})
	})
	if err != nil {
		return Experiment{}, err
	}
	return store.Get(ctx, projectID, experimentID)
}

func (store PostgresStore) RecordManagedStaging(
	ctx context.Context,
	projectID, experimentID, commitSHA string,
	paths []string,
	now time.Time,
) error {
	pathsJSON, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	result, err := store.DB.ExecContext(ctx, `
		UPDATE experiments
		SET staging_commit_sha=$3,
			result_processing=jsonb_set(result_processing,'{staging_paths}',$4::jsonb,true),
			updated_at=$5
		WHERE project_id=$1 AND experiment_id=$2
		  AND execution_status='processing_result'
	`, projectID, experimentID, commitSHA, pathsJSON, now)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}

func (store PostgresStore) RecordCompensation(
	ctx context.Context,
	projectID, experimentID, stagingSHA, revertSHA string,
	now time.Time,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE experiments
			SET staging_commit_sha=$3,revert_commit_sha=$4,updated_at=$5,
				cleanup_result=cleanup_result || jsonb_build_object(
					'result_staging_commit_sha',$3,'result_revert_commit_sha',$4
				)
			WHERE project_id=$1 AND experiment_id=$2
		`, projectID, experimentID, stagingSHA, revertSHA, now)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrNotFound
		}
		return store.recordMutationAudit(ctx, tx, "experiment.result.compensated", projectID, experimentID, map[string]interface{}{
			"staging_commit_sha": stagingSHA, "revert_commit_sha": revertSHA,
		}, "core", now)
	})
}

func (store PostgresStore) recordMutationAudit(
	ctx context.Context,
	tx transaction.Tx,
	action, projectID, experimentID string,
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
		Category: "experiment", Metadata: metadata, Outcome: "success",
		ProjectID: projectID, ResourceID: experimentID,
		ResourceType: "experiment", Source: "core", OccurredAt: occurredAt,
	})
}

func (store PostgresStore) FailResultInTransaction(
	ctx context.Context,
	tx transaction.Tx,
	experimentID string,
	failure Failure,
	now time.Time,
) error {
	cleanupJSON, err := json.Marshal(failure.CleanupResult)
	if err != nil {
		return err
	}
	var projectID, status, taskID, boxID string
	var logsTruncated bool
	if err := tx.QueryRowContext(ctx, `
		SELECT experiment.project_id::text,experiment.execution_status,
			COALESCE(experiment.task_id::text,''),COALESCE(experiment.box_id::text,''),
			COALESCE(task.logs_truncated,false)
		FROM experiments experiment
		LEFT JOIN box_tasks task ON task.task_id=experiment.task_id
		WHERE experiment.experiment_id=$1 FOR UPDATE OF experiment
	`, experimentID).Scan(
		&projectID, &status, &taskID, &boxID, &logsTruncated,
	); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if status == StatusFailed || status == StatusCanceled || status == StatusTimedOut {
		return nil
	}
	if status != StatusProcessingResult {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE experiments
		SET execution_status='failed',failure_stage=$2,failure_code=$3,
			failure_message=$4,failed_at=$5,retryable=$6,failure_attempt=$7,
			cleanup_result=$8,finished_at=$9,updated_at=$9
		WHERE experiment_id=$1
	`, experimentID, failure.Stage, failure.Code, failure.Message,
		failure.FailedAt, failure.Retryable, failure.Attempt, cleanupJSON, now); err != nil {
		return err
	}
	if taskID != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE box_tasks
			SET status='failed',error_code=$2,error_message=$3,failure_stage=$4,
				failure_retryable=$5,failure_cleanup_result=$6,
				finished_at=$7,updated_at=$7
			WHERE task_id=$1 AND status='processing_result'
		`, taskID, failure.Code, failure.Message, failure.Stage,
			failure.Retryable, cleanupJSON, now); err != nil {
			return err
		}
	}
	return store.writeEvent(ctx, tx, outbox.Event{
		EventType: "experiment.failed", Producer: "experiment",
		ProjectID: projectID, OccurredAt: now,
		Payload: failurePayload(
			experimentID, taskID, boxID, StatusFailed, failure, logsTruncated, nil,
		),
	})
}

func (store PostgresStore) Result(
	ctx context.Context,
	projectID, experimentID string,
) (ResultBundle, error) {
	item, err := store.Get(ctx, projectID, experimentID)
	if err != nil {
		return ResultBundle{}, err
	}
	files, err := store.resultFiles(ctx, projectID, experimentID)
	if err != nil {
		return ResultBundle{}, err
	}
	return ResultBundle{
		ExperimentID: item.ID, ExecutionStatus: item.ExecutionStatus,
		ResultDirectory: item.ResultDirectory, ResultCommitSHA: item.ResultCommitSHA,
		ResultManifestSHA256: item.ResultManifestSHA256,
		Manifest:             item.ResultManifest, ExecutionBundle: item.ExecutionBundle,
		Files: files, Retry: decorate(item).Retry, Summary: item.Summary,
		Analysis: item.ResultAnalysis,
	}, nil
}

func (store PostgresStore) resultFiles(
	ctx context.Context,
	projectID, experimentID string,
) ([]ResultFile, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT path,storage_kind,sha256,size_bytes,media_type,
			COALESCE(artifact_id::text,''),COALESCE(artifact_version_id::text,''),
			COALESCE(repository_path,'')
		FROM experiment_result_files
		WHERE project_id=$1 AND experiment_id=$2 ORDER BY path
	`, projectID, experimentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ResultFile{}
	for rows.Next() {
		var item ResultFile
		if err := rows.Scan(
			&item.Path, &item.StorageKind, &item.SHA256, &item.SizeBytes,
			&item.MediaType, &item.ArtifactID, &item.ArtifactVersionID,
			&item.RepositoryPath,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) Compare(
	ctx context.Context,
	projectID string,
	ids []string,
) (Comparison, error) {
	items := make([]Experiment, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return Comparison{}, ErrInvalid
		}
		seen[id] = true
		item, err := store.Get(ctx, projectID, id)
		if err != nil {
			return Comparison{}, err
		}
		items = append(items, item)
	}
	return Comparison{Items: items}, nil
}

func (store PostgresStore) recordPhase(
	ctx context.Context,
	tx transaction.Tx,
	item Experiment,
	previous, status string,
	now time.Time,
) error {
	return store.writeEvent(ctx, tx, outbox.Event{
		EventType: "experiment.phase_changed", Producer: "experiment",
		ProjectID: item.ProjectID, OccurredAt: now,
		Payload: map[string]interface{}{
			"experiment_id": item.ID, "previous_status": previous,
			"execution_status": status, "progress": progressFor(status),
		},
	})
}

func (store PostgresStore) recordTaskPhase(
	ctx context.Context,
	tx transaction.Tx,
	item Experiment,
	previous string,
	task boxcontrol.Task,
	now time.Time,
) error {
	if previous == item.ExecutionStatus {
		return nil
	}
	if previous != item.ExecutionStatus {
		if err := store.recordPhase(ctx, tx, item, previous, item.ExecutionStatus, now); err != nil {
			return err
		}
	}
	if item.ExecutionStatus == StatusPreparing || item.ExecutionStatus == StatusRunning {
		return store.writeEvent(ctx, tx, outbox.Event{
			EventType: "experiment.started", Producer: "experiment",
			ProjectID: item.ProjectID, OccurredAt: now,
			Payload: map[string]interface{}{
				"experiment_id": item.ID, "task_id": task.ID,
				"box_id": task.BoxID, "execution_epoch": task.ExecutionEpoch,
				"runtime": task.ActualRuntime, "runtime_version": task.RuntimeVersion,
				"execution_status": item.ExecutionStatus,
			},
		})
	}
	if item.ExecutionStatus == StatusFailed || item.ExecutionStatus == StatusTimedOut {
		failure := Failure{
			Stage: "runtime_execution", Code: "RUNTIME_FAILED",
			Message: "Runtime execution failed", FailedAt: now,
			BoxID: task.BoxID, Runtime: task.ActualRuntime, Attempt: task.Attempt,
			CleanupResult: map[string]interface{}{},
		}
		if task.Failure != nil {
			failure = *task.Failure
		}
		return store.writeEvent(ctx, tx, outbox.Event{
			EventType: "experiment.failed", Producer: "experiment",
			ProjectID: item.ProjectID, OccurredAt: now,
			Payload: failurePayload(
				item.ID, task.ID, task.BoxID, item.ExecutionStatus,
				failure, task.LogsTruncated, task.ExitCode,
			),
		})
	}
	if item.ExecutionStatus == StatusCanceled {
		return store.writeEvent(ctx, tx, outbox.Event{
			EventType: "experiment.canceled", Producer: "experiment",
			ProjectID: item.ProjectID, OccurredAt: now,
			Payload: map[string]interface{}{
				"experiment_id": item.ID, "experiment_type": item.Type,
				"task_id": task.ID, "execution_status": StatusCanceled,
			},
		})
	}
	return nil
}

func failurePayload(
	experimentID, taskID, boxID, status string,
	failure Failure,
	logsTruncated bool,
	exitCode *int,
) map[string]interface{} {
	payload := map[string]interface{}{
		"experiment_id": experimentID, "execution_status": status,
		"failure_stage": failure.Stage, "failure_code": failure.Code,
		"failed_at": failure.FailedAt, "retryable": failure.Retryable,
		"logs_truncated": logsTruncated,
	}
	if taskID != "" {
		payload["task_id"] = taskID
	}
	if boxID != "" {
		payload["box_id"] = boxID
	}
	if exitCode != nil {
		payload["exit_code"] = *exitCode
	}
	return payload
}

func validTransition(from, to string) bool {
	switch from {
	case StatusQueued:
		return to == StatusPreparing || to == StatusFailed ||
			to == StatusCanceled || to == StatusTimedOut
	case StatusPreparing:
		return to == StatusRunning || to == StatusFailed ||
			to == StatusCanceled || to == StatusTimedOut
	case StatusRunning:
		return to == StatusUploading || to == StatusFailed ||
			to == StatusCanceled || to == StatusTimedOut
	case StatusUploading:
		return to == StatusFailed || to == StatusCanceled || to == StatusTimedOut
	default:
		return false
	}
}

func taskStatus(status string) string {
	switch status {
	case boxcontrol.TaskQueued:
		return StatusQueued
	case boxcontrol.TaskPreparing:
		return StatusPreparing
	case boxcontrol.TaskRunning:
		return StatusRunning
	case boxcontrol.TaskUploading:
		return StatusUploading
	case boxcontrol.TaskProcessingResult:
		return StatusProcessingResult
	case boxcontrol.TaskSucceeded:
		return StatusSucceeded
	case boxcontrol.TaskFailed:
		return StatusFailed
	case boxcontrol.TaskCanceled:
		return StatusCanceled
	case boxcontrol.TaskTimedOut:
		return StatusTimedOut
	default:
		return ""
	}
}

func (store PostgresStore) selectSQL() string {
	return `
		SELECT experiment.experiment_id,experiment.project_id,experiment.created_by,
			experiment.name,experiment.experiment_type,experiment.execution_status,
			COALESCE(experiment.connectivity_status,''),experiment.source_commit,
			experiment.entrypoint,experiment.parameters,experiment.environment,
			experiment.inputs,experiment.requested_runtime_policy,
			COALESCE(experiment.requested_box_id::text,''),
			COALESCE(experiment.actual_runtime,''),COALESCE(experiment.runtime_version,''),
			experiment.limits,experiment.idempotency_key,experiment.max_attempts,
			COALESCE(experiment.box_id::text,''),COALESCE(experiment.task_id::text,''),
			experiment.exit_code,COALESCE(experiment.failure_stage,''),
			COALESCE(experiment.failure_code,''),COALESCE(experiment.failure_message,''),
			experiment.failed_at,experiment.retryable,experiment.failure_attempt,
			experiment.cleanup_result,
			experiment.resource_usage,COALESCE(experiment.summary,''),
			experiment.project_timezone,experiment.result_directory,
			COALESCE(experiment.result_commit_sha,''),COALESCE(experiment.staging_commit_sha,''),
			COALESCE(experiment.revert_commit_sha,''),
			COALESCE(experiment.execution_bundle_artifact_id::text,''),
			COALESCE(experiment.execution_bundle_version_id::text,''),
			COALESCE(bundle.filename,''),COALESCE(bundle.sha256,''),
			COALESCE(bundle.size_bytes,0),COALESCE(experiment.result_manifest_sha256,''),
			COALESCE(experiment.retry_of_experiment_id::text,''),
			COALESCE(experiment.root_experiment_id::text,''),
			COALESCE(experiment.superseded_by_experiment_id::text,''),
			COALESCE(experiment.latest_experiment_id::text,''),experiment.retry_sequence,
			COALESCE(task.logs_truncated,false),task.logs_truncated_at,
			experiment.result_manifest,experiment.result_processing,
			COALESCE(experiment.run_idempotency_key,''),
			COALESCE(experiment.result_bind_idempotency_key,''),
			COALESCE(settings.git_large_file_threshold_bytes,52428800),
			experiment.created_at,experiment.started_at,experiment.finished_at,
			experiment.updated_at
		FROM experiments experiment
		LEFT JOIN box_tasks task ON task.task_id=experiment.task_id
		LEFT JOIN artifact_versions bundle
			ON bundle.version_id=experiment.execution_bundle_version_id
		LEFT JOIN experiment_project_settings settings
			ON settings.project_id=experiment.project_id
	`
}

func (store PostgresStore) scan(row scanner) (Experiment, error) {
	var item Experiment
	var parametersJSON, environmentJSON, inputsJSON, limitsJSON []byte
	var cleanupJSON, usageJSON, manifestJSON, processingJSON []byte
	var failureStage, failureCode, failureMessage string
	var failedAt *time.Time
	var retryable bool
	var failureAttempt int
	var bundleArtifactID, bundleVersionID, bundleFilename, bundleSHA string
	var bundleSize int64
	err := row.Scan(
		&item.ID, &item.ProjectID, &item.CreatedBy, &item.Name, &item.Type,
		&item.ExecutionStatus, &item.ConnectivityStatus, &item.SourceCommit,
		&item.Entrypoint, &parametersJSON, &environmentJSON, &inputsJSON,
		&item.RequestedRuntimePolicy, &item.RequestedBoxID, &item.ActualRuntime,
		&item.RuntimeVersion, &limitsJSON, &item.IdempotencyKey, &item.MaxAttempts,
		&item.BoxID, &item.TaskID, &item.ExitCode, &failureStage, &failureCode,
		&failureMessage, &failedAt, &retryable, &failureAttempt, &cleanupJSON, &usageJSON,
		&item.Summary, &item.ProjectTimezone, &item.ResultDirectory,
		&item.ResultCommitSHA, &item.StagingCommitSHA, &item.RevertCommitSHA,
		&bundleArtifactID, &bundleVersionID, &bundleFilename, &bundleSHA,
		&bundleSize, &item.ResultManifestSHA256, &item.Retry.RetryOfExperimentID,
		&item.Retry.RootExperimentID, &item.Retry.SupersededByExperimentID,
		&item.Retry.LatestExperimentID, &item.Retry.RetrySequence,
		&item.LogsTruncated, &item.LogsTruncatedAt, &manifestJSON, &processingJSON,
		&item.RunIdempotencyKey, &item.ResultBindIdempotency,
		&item.GitLargeFileThreshold, &item.CreatedAt, &item.StartedAt,
		&item.FinishedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Experiment{}, ErrNotFound
	}
	if err != nil {
		return Experiment{}, err
	}
	for value, destination := range map[*[]byte]interface{}{
		&parametersJSON:  &item.Parameters,
		&environmentJSON: &item.Environment,
		&inputsJSON:      &item.Inputs,
		&limitsJSON:      &item.Limits,
		&usageJSON:       &item.ResourceUsage,
	} {
		if err := json.Unmarshal(*value, destination); err != nil {
			return Experiment{}, err
		}
	}
	if failureCode != "" {
		failure := &Failure{
			Stage: failureStage, Code: failureCode, Message: failureMessage,
			Retryable: retryable, BoxID: item.BoxID, Runtime: item.ActualRuntime,
			CleanupResult: map[string]interface{}{},
		}
		if failedAt != nil {
			failure.FailedAt = *failedAt
		}
		failure.Attempt = failureAttempt
		if err := json.Unmarshal(cleanupJSON, &failure.CleanupResult); err != nil {
			return Experiment{}, err
		}
		item.Failure = failure
	}
	if bundleArtifactID != "" && bundleVersionID != "" {
		item.ExecutionBundle = &ArtifactPointer{
			ArtifactID: bundleArtifactID, VersionID: bundleVersionID,
			Filename: bundleFilename, SHA256: bundleSHA, SizeBytes: bundleSize,
		}
	}
	if len(manifestJSON) > 0 {
		item.ResultManifest = map[string]interface{}{}
		if err := json.Unmarshal(manifestJSON, &item.ResultManifest); err != nil {
			return Experiment{}, err
		}
	}
	if len(processingJSON) > 0 {
		processing := map[string]interface{}{}
		if err := json.Unmarshal(processingJSON, &processing); err != nil {
			return Experiment{}, err
		}
		item.ResultAnalysis, _ = processing["analysis"].(string)
		if values, ok := processing["staging_paths"].([]interface{}); ok {
			item.StagingPaths = make([]string, 0, len(values))
			for _, value := range values {
				if repositoryPath, ok := value.(string); ok {
					item.StagingPaths = append(item.StagingPaths, repositoryPath)
				}
			}
		}
	}
	return item, nil
}
