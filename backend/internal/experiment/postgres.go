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
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
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

func (store PostgresStore) Create(ctx context.Context, item Experiment) (Experiment, bool, error) {
	if store.Transaction.DB != nil {
		inserted := false
		err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
			parameters, _ := json.Marshal(item.Parameters)
			environment, _ := json.Marshal(item.Environment)
			inputs, _ := json.Marshal(item.Inputs)
			limits, _ := json.Marshal(item.Limits)
			row := tx.QueryRowContext(ctx, `INSERT INTO experiments(experiment_id,project_id,created_by,name,status,source_commit,entrypoint,parameters,environment,inputs,runtime,limits,idempotency_key,max_attempts,resource_usage,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'{}'::jsonb,$15,$15) ON CONFLICT (project_id,idempotency_key) DO NOTHING RETURNING experiment_id`, item.ID, item.ProjectID, item.CreatedBy, item.Name, item.Status, item.SourceCommit, item.Entrypoint, parameters, environment, inputs, item.Runtime, limits, item.IdempotencyKey, item.MaxAttempts, item.CreatedAt)
			var ignored string
			if err := row.Scan(&ignored); errors.Is(err, sql.ErrNoRows) {
				return nil
			} else if err != nil {
				return err
			}
			inserted = true
			if err := store.recordCreate(ctx, tx, item); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return Experiment{}, false, err
		}
		created, err := store.Get(ctx, item.ProjectID, item.ID)
		if !inserted {
			created, err = store.getByIdempotency(ctx, item.ProjectID, item.IdempotencyKey)
		}
		return created, inserted, err
	}
	if existing, err := store.getByIdempotency(ctx, item.ProjectID, item.IdempotencyKey); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Experiment{}, false, err
	}
	parameters, _ := json.Marshal(item.Parameters)
	environment, _ := json.Marshal(item.Environment)
	inputs, _ := json.Marshal(item.Inputs)
	limits, _ := json.Marshal(item.Limits)
	_, err := store.DB.ExecContext(ctx, `INSERT INTO experiments(experiment_id,project_id,created_by,name,status,source_commit,entrypoint,parameters,environment,inputs,runtime,limits,idempotency_key,max_attempts,resource_usage,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'{}'::jsonb,$15,$15)`, item.ID, item.ProjectID, item.CreatedBy, item.Name, item.Status, item.SourceCommit, item.Entrypoint, parameters, environment, inputs, item.Runtime, limits, item.IdempotencyKey, item.MaxAttempts, item.CreatedAt)
	if err != nil {
		return Experiment{}, false, err
	}
	created, err := store.Get(ctx, item.ProjectID, item.ID)
	return created, true, err
}

func (store PostgresStore) recordCreate(ctx context.Context, tx transaction.Tx, item Experiment) error {
	if store.Audit != nil {
		if err := store.Audit.RecordInTransaction(ctx, tx, audit.Event{
			Action: "experiment.created", ActorID: item.CreatedBy, ActorKind: "user",
			Category: "experiment", Metadata: map[string]interface{}{"runtime": item.Runtime},
			Outcome: "success", ProjectID: item.ProjectID, ResourceID: item.ID,
			ResourceType: "experiment", Source: "core",
		}); err != nil {
			return err
		}
	}
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{
		Actor: map[string]string{"user_id": item.CreatedBy}, EventType: "experiment.created",
		Payload:  map[string]interface{}{"experiment_id": item.ID, "name": item.Name, "source_commit": item.SourceCommit, "entrypoint": item.Entrypoint, "status": StatusCreated},
		Producer: "experiment", ProjectID: item.ProjectID,
	})
	return err
}

func (store PostgresStore) getByIdempotency(ctx context.Context, projectID, key string) (Experiment, error) {
	return store.scan(store.DB.QueryRowContext(ctx, store.selectSQL()+` WHERE project_id=$1 AND idempotency_key=$2`, projectID, key))
}

func (store PostgresStore) Get(ctx context.Context, projectID, id string) (Experiment, error) {
	return store.scan(store.DB.QueryRowContext(ctx, store.selectSQL()+` WHERE project_id=$1 AND experiment_id=$2`, projectID, id))
}

func (store PostgresStore) List(ctx context.Context, projectID, status string, offset, limit int) (Page, error) {
	query := store.selectSQL() + ` WHERE project_id=$1 AND ($2='' OR status=$2) ORDER BY updated_at DESC,experiment_id DESC OFFSET $3 LIMIT $4`
	rows, err := store.DB.QueryContext(ctx, query, projectID, status, offset, limit+1)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	items := []Experiment{}
	for rows.Next() {
		item, scanErr := store.scan(rows)
		if scanErr != nil {
			return Page{}, scanErr
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

func (store PostgresStore) Queue(ctx context.Context, id, taskID string, now time.Time) (Experiment, error) {
	row := store.DB.QueryRowContext(ctx, store.selectSQL()+` WHERE experiment_id=$1 AND status='created'`, id)
	item, err := store.scan(row)
	if errors.Is(err, ErrNotFound) {
		return Experiment{}, ErrConflict
	}
	if err != nil {
		return Experiment{}, err
	}
	_, err = store.DB.ExecContext(ctx, `UPDATE experiments SET status='queued',task_id=$2,updated_at=$3 WHERE experiment_id=$1 AND status='created'`, id, taskID, now)
	if err != nil {
		return Experiment{}, err
	}
	return store.Get(ctx, item.ProjectID, id)
}

// QueueWithTask atomically creates the PostgreSQL-backed Box job and freezes
// the Experiment's task reference. This prevents a crash between two
// independent writes from leaving an experiment queued without a task.
func (store PostgresStore) QueueWithTask(ctx context.Context, item Experiment, task boxcontrol.Task, now time.Time) (Experiment, error) {
	runSpec, err := json.Marshal(task.RunSpec)
	if err != nil {
		return Experiment{}, err
	}
	if store.Transaction.DB == nil {
		if err := store.DB.QueryRowContext(ctx, `SELECT experiment_id FROM experiments WHERE experiment_id=$1 AND status='created'`, item.ID).Scan(new(string)); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Experiment{}, ErrConflict
			}
			return Experiment{}, err
		}
		if _, err := store.DB.ExecContext(ctx, `INSERT INTO box_tasks(task_id,experiment_id,project_id,status,attempt,max_attempts,run_spec,resource_usage,created_at,updated_at) VALUES($1,$2,$3,'queued',0,$4,$5,'{}'::jsonb,$6,$6)`, task.ID, item.ID, item.ProjectID, task.MaxAttempts, runSpec, now); err != nil {
			return Experiment{}, err
		}
		return store.Queue(ctx, item.ID, task.ID, now)
	}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM experiments WHERE experiment_id=$1 FOR UPDATE`, item.ID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != StatusCreated {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO box_tasks(task_id,experiment_id,project_id,status,attempt,max_attempts,run_spec,resource_usage,created_at,updated_at) VALUES($1,$2,$3,'queued',0,$4,$5,'{}'::jsonb,$6,$6)`, task.ID, item.ID, item.ProjectID, task.MaxAttempts, runSpec, now); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE experiments SET status='queued',task_id=$2,updated_at=$3 WHERE experiment_id=$1 AND status='created'`, item.ID, task.ID, now)
		return err
	})
	if err != nil {
		return Experiment{}, err
	}
	return store.Get(ctx, item.ProjectID, item.ID)
}

func (store PostgresStore) Cancel(ctx context.Context, projectID, id string, now time.Time) (Experiment, error) {
	if store.Transaction.DB != nil {
		var previous, taskID string
		err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
			if err := tx.QueryRowContext(ctx, `SELECT status,COALESCE(task_id::text,'') FROM experiments WHERE project_id=$1 AND experiment_id=$2 FOR UPDATE`, projectID, id).Scan(&previous, &taskID); errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
			if previous == StatusArchived || previous == StatusSucceeded || previous == StatusFailed || previous == StatusCanceled {
				return nil
			}
			if _, err := tx.ExecContext(ctx, `UPDATE experiments SET status=CASE WHEN status IN ('created','queued') THEN 'canceled' ELSE status END,finished_at=CASE WHEN status IN ('created','queued') THEN $3 ELSE finished_at END,updated_at=$3 WHERE project_id=$1 AND experiment_id=$2`, projectID, id, now); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE box_tasks SET cancel_requested_at=$2,status=CASE WHEN status='queued' THEN 'canceled' ELSE status END,finished_at=CASE WHEN status='queued' THEN $2 ELSE finished_at END,updated_at=$2 WHERE experiment_id=$1 AND status NOT IN ('succeeded','failed','canceled','timed_out')`, id, now); err != nil {
				return err
			}
			if previous == StatusCreated || previous == StatusQueued {
				if err := store.recordLifecycle(ctx, tx, projectID, id, taskID, "", "", StatusCanceled, "", nil, nil, now); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return Experiment{}, err
		}
	} else {
		row := store.DB.QueryRowContext(ctx, `UPDATE experiments SET status=CASE WHEN status IN ('created','queued') THEN 'canceled' ELSE status END,finished_at=CASE WHEN status IN ('created','queued') THEN $3 ELSE finished_at END,updated_at=$3 WHERE project_id=$1 AND experiment_id=$2 RETURNING experiment_id`, projectID, id, now)
		var ignored string
		if err := row.Scan(&ignored); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Experiment{}, ErrNotFound
			}
			return Experiment{}, err
		}
		_, _ = store.DB.ExecContext(ctx, `UPDATE box_tasks SET cancel_requested_at=$2,status=CASE WHEN status='queued' THEN 'canceled' ELSE status END,finished_at=CASE WHEN status='queued' THEN $2 ELSE finished_at END,updated_at=$2 WHERE experiment_id=$1 AND status NOT IN ('succeeded','failed','canceled','timed_out')`, id, now)
	}
	return store.Get(ctx, projectID, id)
}

func (store PostgresStore) Archive(ctx context.Context, projectID, id string, now time.Time) (Experiment, error) {
	if store.Transaction.DB == nil {
		var status string
		if err := store.DB.QueryRowContext(ctx, `SELECT status FROM experiments WHERE project_id=$1 AND experiment_id=$2`, projectID, id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
			return Experiment{}, ErrNotFound
		} else if err != nil {
			return Experiment{}, err
		}
		if status != StatusSucceeded && status != StatusFailed && status != StatusCanceled {
			return Experiment{}, ErrConflict
		}
		if _, err := store.DB.ExecContext(ctx, `UPDATE experiments SET status='archived',updated_at=$3 WHERE project_id=$1 AND experiment_id=$2`, projectID, id, now); err != nil {
			return Experiment{}, err
		}
		return store.Get(ctx, projectID, id)
	}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status, taskID string
		if err := tx.QueryRowContext(ctx, `SELECT status,COALESCE(task_id::text,'') FROM experiments WHERE project_id=$1 AND experiment_id=$2 FOR UPDATE`, projectID, id).Scan(&status, &taskID); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status == StatusArchived {
			return nil
		}
		if status != StatusSucceeded && status != StatusFailed && status != StatusCanceled {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE experiments SET status='archived',updated_at=$3 WHERE project_id=$1 AND experiment_id=$2`, projectID, id, now); err != nil {
			return err
		}
		if store.Audit != nil {
			if err := store.Audit.RecordInTransaction(ctx, tx, audit.Event{Action: "experiment.archived", ActorKind: "user", Category: "experiment", Outcome: "success", ProjectID: projectID, ResourceID: id, ResourceType: "experiment", Source: "core", OccurredAt: now}); err != nil {
				return err
			}
		}
		_, err := store.Outbox.Write(ctx, tx, outbox.Event{EventType: "experiment.archived", Payload: map[string]interface{}{"experiment_id": id, "status": StatusArchived}, Producer: "experiment", ProjectID: projectID, OccurredAt: now, CausationID: taskID})
		return err
	})
	if err != nil {
		return Experiment{}, err
	}
	return store.Get(ctx, projectID, id)
}

func (store PostgresStore) ApplyTaskStatus(ctx context.Context, task boxcontrol.Task, now time.Time) (Experiment, error) {
	status := taskStatus(task.Status)
	if status == "" {
		return Experiment{}, ErrInvalid
	}
	usage, _ := json.Marshal(task.ResourceUsage)
	if store.Transaction.DB != nil {
		err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
			var previous, runtime string
			if err := tx.QueryRowContext(ctx, `SELECT status,runtime FROM experiments WHERE experiment_id=$1 FOR UPDATE`, task.ExperimentID).Scan(&previous, &runtime); errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
			if previous == status {
				return nil
			}
			if !validTransition(previous, status) {
				return ErrConflict
			}
			if _, err := tx.ExecContext(ctx, `UPDATE experiments SET status=$2,box_id=NULLIF($3,'')::uuid,exit_code=$4,failure_code=NULLIF($5,''),failure_message=NULLIF($6,''),resource_usage=$7,summary=NULLIF($8,''),started_at=CASE WHEN $2 IN ('preparing','running') THEN COALESCE(started_at,$9) ELSE started_at END,finished_at=CASE WHEN $2 IN ('succeeded','failed','canceled') THEN COALESCE(finished_at,$9) ELSE finished_at END,updated_at=$9 WHERE experiment_id=$1`, task.ExperimentID, status, task.BoxID, task.ExitCode, task.ErrorCode, task.ErrorMessage, usage, task.Summary, now); err != nil {
				return err
			}
			if status != StatusSucceeded {
				return store.recordLifecycle(ctx, tx, task.ProjectID, task.ExperimentID, task.ID, task.BoxID, runtime, status, task.ErrorCode, task.ExitCode, nil, now)
			}
			return nil
		})
		if err != nil {
			return Experiment{}, err
		}
	} else {
		row := store.DB.QueryRowContext(ctx, `UPDATE experiments SET status=$2,box_id=NULLIF($3,'')::uuid,exit_code=$4,failure_code=NULLIF($5,''),failure_message=NULLIF($6,''),resource_usage=$7,summary=NULLIF($8,''),started_at=CASE WHEN $2 IN ('preparing','running') THEN COALESCE(started_at,$9) ELSE started_at END,finished_at=CASE WHEN $2 IN ('succeeded','failed','canceled') THEN COALESCE(finished_at,$9) ELSE finished_at END,updated_at=$9 WHERE experiment_id=$1 RETURNING experiment_id`, task.ExperimentID, status, task.BoxID, task.ExitCode, task.ErrorCode, task.ErrorMessage, usage, task.Summary, now)
		var ignored string
		if err := row.Scan(&ignored); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Experiment{}, ErrNotFound
			}
			return Experiment{}, err
		}
	}
	return store.Get(ctx, task.ProjectID, task.ExperimentID)
}

func (store PostgresStore) ApplyResult(ctx context.Context, task boxcontrol.Task, result boxcontrol.Result, now time.Time) (Experiment, error) {
	manifest, _ := json.Marshal(result.Manifest)
	artifact, _ := json.Marshal(result.Artifact)
	if store.Transaction.DB != nil {
		err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
			var status string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM experiments WHERE experiment_id=$1 AND task_id=$2 FOR UPDATE`, task.ExperimentID, task.ID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
				return ErrConflict
			} else if err != nil {
				return err
			}
			if status == StatusSucceeded {
				return nil
			}
			if status == StatusFailed || status == StatusCanceled || status == StatusArchived {
				return ErrConflict
			}
			if _, err := tx.ExecContext(ctx, `UPDATE experiments SET status='succeeded',result_manifest=$3,result_artifact=$4,finished_at=$5,updated_at=$5 WHERE experiment_id=$1 AND task_id=$2 AND result_manifest IS NULL`, task.ExperimentID, task.ID, manifest, artifact, now); err != nil {
				return err
			}
			return store.recordLifecycle(ctx, tx, task.ProjectID, task.ExperimentID, task.ID, task.BoxID, "", StatusSucceeded, "", nil, result.Artifact, now)
		})
		if err != nil {
			return Experiment{}, err
		}
	} else {
		row := store.DB.QueryRowContext(ctx, `UPDATE experiments SET status='succeeded',result_manifest=$3,result_artifact=$4,finished_at=$5,updated_at=$5 WHERE experiment_id=$1 AND task_id=$2 AND result_manifest IS NULL AND status NOT IN ('failed','canceled','archived') RETURNING experiment_id`, task.ExperimentID, task.ID, manifest, artifact, now)
		var ignored string
		if err := row.Scan(&ignored); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Experiment{}, ErrConflict
			}
			return Experiment{}, err
		}
	}
	return store.Get(ctx, task.ProjectID, task.ExperimentID)
}

func (store PostgresStore) recordLifecycle(ctx context.Context, tx transaction.Tx, projectID, experimentID, taskID, boxID, runtime, status, errorCode string, exitCode *int, artifact map[string]interface{}, now time.Time) error {
	if status == StatusArchived {
		return nil
	}
	if store.Audit != nil {
		if err := store.Audit.RecordInTransaction(ctx, tx, audit.Event{Action: "experiment." + status, ActorKind: "box", Category: "experiment", ErrorCode: errorCode, Metadata: map[string]interface{}{"task_id": taskID}, Outcome: lifecycleOutcome(status), ProjectID: projectID, ResourceID: experimentID, ResourceType: "experiment", Source: "core", OccurredAt: now}); err != nil {
			return err
		}
	}
	payload := map[string]interface{}{"experiment_id": experimentID, "task_id": taskID, "status": status}
	if errorCode != "" {
		payload["error_code"] = errorCode
	}
	if exitCode != nil {
		payload["exit_code"] = *exitCode
	}
	eventType := "experiment." + status
	if status == StatusPreparing || status == StatusRunning {
		eventType = "experiment.started"
		payload["box_id"] = boxID
		payload["runtime"] = runtime
	}
	if status == StatusFailed {
		eventType = "experiment.failed"
	}
	if status == StatusSucceeded {
		eventType = "experiment.succeeded"
		payload["artifact_id"], payload["version_id"] = resultIDs(artifact)
	}
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{EventType: eventType, Payload: payload, Producer: "experiment", ProjectID: projectID, OccurredAt: now})
	return err
}

func lifecycleOutcome(status string) string {
	if status == StatusFailed {
		return "error"
	}
	if status == StatusCanceled {
		return "denied"
	}
	return "success"
}

func resultIDs(result map[string]interface{}) (string, string) {
	if result == nil {
		return "", ""
	}
	artifactID, _ := result["artifact_id"].(string)
	versionID, _ := result["version_id"].(string)
	return artifactID, versionID
}

func validTransition(from, to string) bool {
	if from == StatusCreated && to == StatusQueued {
		return true
	}
	if from == StatusQueued && (to == StatusPreparing || to == StatusFailed || to == StatusCanceled) {
		return true
	}
	if from == StatusPreparing && (to == StatusQueued || to == StatusRunning || to == StatusFailed || to == StatusCanceled) {
		return true
	}
	if from == StatusRunning && (to == StatusQueued || to == StatusSucceeded || to == StatusFailed || to == StatusCanceled) {
		return true
	}
	return false
}

func (store PostgresStore) Compare(ctx context.Context, projectID string, ids []string) (Comparison, error) {
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

func (store PostgresStore) selectSQL() string {
	return `SELECT experiment_id,project_id,created_by,name,status,source_commit,entrypoint,parameters,environment,inputs,runtime,limits,idempotency_key,max_attempts,box_id,task_id,exit_code,failure_code,failure_message,resource_usage,summary,result_manifest,result_artifact,created_at,started_at,finished_at,updated_at FROM experiments`
}

type scanner interface{ Scan(...interface{}) error }

func (store PostgresStore) scan(row scanner) (Experiment, error) {
	var item Experiment
	var parameters, environment, inputs, limits, usage, manifest, artifact []byte
	var boxID, taskID, failCode, failMessage, summary sql.NullString
	var started, finished sql.NullTime
	err := row.Scan(&item.ID, &item.ProjectID, &item.CreatedBy, &item.Name, &item.Status, &item.SourceCommit, &item.Entrypoint, &parameters, &environment, &inputs, &item.Runtime, &limits, &item.IdempotencyKey, &item.MaxAttempts, &boxID, &taskID, &item.ExitCode, &failCode, &failMessage, &usage, &summary, &manifest, &artifact, &item.CreatedAt, &started, &finished, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Experiment{}, ErrNotFound
	}
	if err != nil {
		return Experiment{}, err
	}
	if err = json.Unmarshal(parameters, &item.Parameters); err != nil {
		return Experiment{}, err
	}
	if err = json.Unmarshal(environment, &item.Environment); err != nil {
		return Experiment{}, err
	}
	if err = json.Unmarshal(inputs, &item.Inputs); err != nil {
		return Experiment{}, err
	}
	if err = json.Unmarshal(limits, &item.Limits); err != nil {
		return Experiment{}, err
	}
	if err = json.Unmarshal(usage, &item.ResourceUsage); err != nil {
		return Experiment{}, err
	}
	if boxID.Valid {
		item.BoxID = boxID.String
	}
	if taskID.Valid {
		item.TaskID = taskID.String
	}
	if failCode.Valid {
		item.FailureCode = failCode.String
	}
	if failMessage.Valid {
		item.FailureMessage = failMessage.String
	}
	if summary.Valid {
		item.Summary = summary.String
	}
	if len(manifest) > 0 {
		item.Result = &ResultBundle{Manifest: map[string]interface{}{}, Artifact: map[string]interface{}{}}
		if err = json.Unmarshal(manifest, &item.Result.Manifest); err != nil {
			return Experiment{}, err
		}
		if err = json.Unmarshal(artifact, &item.Result.Artifact); err != nil {
			return Experiment{}, err
		}
	}
	if started.Valid {
		value := started.Time
		item.StartedAt = &value
	}
	if finished.Valid {
		value := finished.Time
		item.FinishedAt = &value
	}
	return item, nil
}
func taskStatus(status string) string {
	switch status {
	case boxcontrol.TaskQueued:
		return StatusQueued
	case boxcontrol.TaskPreparing:
		return StatusPreparing
	case boxcontrol.TaskRunning:
		return StatusRunning
	case boxcontrol.TaskSucceeded:
		return StatusSucceeded
	case boxcontrol.TaskFailed, boxcontrol.TaskTimedOut:
		return StatusFailed
	case boxcontrol.TaskCanceled:
		return StatusCanceled
	default:
		return ""
	}
}
