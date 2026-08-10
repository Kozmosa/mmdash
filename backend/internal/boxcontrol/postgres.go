package boxcontrol

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mmdash/mmdash/backend/internal/audit"
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

func (store PostgresStore) Create(ctx context.Context, box Box, idempotency string) error {
	capabilities, err := json.Marshal(box.Capabilities)
	if err != nil {
		return err
	}
	runtimes, err := json.Marshal(box.Runtimes)
	if err != nil {
		return err
	}
	limits, err := json.Marshal(box.Limits)
	if err != nil {
		return err
	}
	load, err := json.Marshal(box.Load)
	if err != nil {
		return err
	}
	insert := func(executor interface {
		ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	}) error {
		_, err = executor.ExecContext(ctx, `
		INSERT INTO box_nodes (box_id,project_id,name,status,version,capabilities,runtimes,limits,load,token_id,idempotency_key,created_by,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)`,
			box.ID, box.ProjectID, box.Name, box.Status, box.Version, capabilities, runtimes,
			limits, load, box.TokenID, idempotency, box.CreatedBy, box.CreatedAt)
		if err != nil {
			return mapConflict(err)
		}
		_, err = executor.ExecContext(ctx, `INSERT INTO box_project_bindings(project_id,box_id,created_at,updated_at) VALUES($1,$2,$3,$3) ON CONFLICT(project_id) DO UPDATE SET box_id=EXCLUDED.box_id,updated_at=EXCLUDED.updated_at`, box.ProjectID, box.ID, box.CreatedAt)
		return mapConflict(err)
	}
	if store.Transaction.DB != nil {
		return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
			if err := insert(tx); err != nil {
				return err
			}
			if store.Audit != nil {
				if err := store.Audit.RecordInTransaction(ctx, tx, audit.Event{Action: "box.registered", ActorID: box.CreatedBy, ActorKind: "user", Category: "box", Metadata: map[string]interface{}{"version": box.Version}, Outcome: "success", ProjectID: box.ProjectID, ResourceID: box.ID, ResourceType: "box", Source: "core"}); err != nil {
					return err
				}
			}
			_, err := store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"user_id": box.CreatedBy}, EventType: "box.registered", Payload: map[string]interface{}{"box_id": box.ID, "project_id": box.ProjectID, "name": box.Name, "status": StatusRegistering, "version": box.Version}, Producer: "boxcontrol", ProjectID: box.ProjectID})
			return err
		})
	}
	return insert(store.DB)
}

func (store PostgresStore) Get(ctx context.Context, boxID string) (Box, error) {
	return store.scanBox(store.DB.QueryRowContext(ctx, `SELECT box_id,project_id,name,status,version,capabilities,runtimes,limits,load,token_id,last_heartbeat_at,disconnected_at,created_at,updated_at FROM box_nodes WHERE box_id=$1`, boxID))
}

func (store PostgresStore) List(ctx context.Context, projectID string) ([]Box, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT b.box_id,b.project_id,b.name,b.status,b.version,b.capabilities,b.runtimes,b.limits,b.load,b.token_id,b.last_heartbeat_at,b.disconnected_at,b.created_at,b.updated_at FROM box_nodes b JOIN box_project_bindings p ON p.box_id=b.box_id WHERE p.project_id=$1 ORDER BY b.updated_at DESC,b.box_id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Box{}
	for rows.Next() {
		item, scanErr := store.scanBox(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) UpdateHeartbeat(ctx context.Context, boxID string, update Box, now time.Time) (Box, error) {
	capabilities, _ := json.Marshal(update.Capabilities)
	runtimes, _ := json.Marshal(update.Runtimes)
	limits, _ := json.Marshal(update.Limits)
	load, _ := json.Marshal(update.Load)
	if store.Transaction.DB != nil {
		var item Box
		err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
			result := tx.QueryRowContext(ctx, `UPDATE box_nodes SET status='online',version=$2,capabilities=$3,runtimes=$4,limits=$5,load=$6,last_heartbeat_at=$7,disconnected_at=NULL,updated_at=$7 WHERE box_id=$1 AND status <> 'revoked' RETURNING box_id,project_id,name,status,version,capabilities,runtimes,limits,load,token_id,last_heartbeat_at,disconnected_at,created_at,updated_at`, boxID, update.Version, capabilities, runtimes, limits, load, now)
			var err error
			item, err = store.scanBox(result)
			if err != nil {
				return err
			}
			if store.Audit != nil {
				if err := store.Audit.RecordInTransaction(ctx, tx, audit.Event{Action: "box.heartbeat", ActorKind: "box", Category: "box", Metadata: map[string]interface{}{"running_tasks": item.Load.RunningTasks}, Outcome: "success", ProjectID: item.ProjectID, ResourceID: item.ID, ResourceType: "box", Source: "core", OccurredAt: now}); err != nil {
					return err
				}
			}
			_, err = store.Outbox.Write(ctx, tx, outbox.Event{EventType: "box.heartbeat.received", Payload: map[string]interface{}{"box_id": item.ID, "project_id": item.ProjectID, "status": StatusOnline, "version": item.Version, "running_tasks": item.Load.RunningTasks}, Producer: "boxcontrol", ProjectID: item.ProjectID, OccurredAt: now})
			return err
		})
		return item, err
	}
	result := store.DB.QueryRowContext(ctx, `UPDATE box_nodes SET status='online',version=$2,capabilities=$3,runtimes=$4,limits=$5,load=$6,last_heartbeat_at=$7,disconnected_at=NULL,updated_at=$7 WHERE box_id=$1 AND status <> 'revoked' RETURNING box_id,project_id,name,status,version,capabilities,runtimes,limits,load,token_id,last_heartbeat_at,disconnected_at,created_at,updated_at`, boxID, update.Version, capabilities, runtimes, limits, load, now)
	return store.scanBox(result)
}

func (store PostgresStore) MarkOffline(ctx context.Context, now time.Time, heartbeatBefore time.Time, limit int) ([]Box, error) {
	if limit < 1 {
		return nil, ErrInvalid
	}
	if store.Transaction.DB == nil {
		return nil, ErrInvalid
	}
	items := make([]Box, 0, limit)
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		rows, err := tx.QueryContext(ctx, `WITH stale AS (SELECT box_id FROM box_nodes WHERE status='online' AND last_heartbeat_at < $2 ORDER BY last_heartbeat_at,box_id FOR UPDATE SKIP LOCKED LIMIT $3) UPDATE box_nodes AS box SET status='offline',disconnected_at=COALESCE(box.disconnected_at,$1),updated_at=$1 FROM stale WHERE box.box_id=stale.box_id RETURNING box.box_id,box.project_id,box.name,box.status,box.version,box.capabilities,box.runtimes,box.limits,box.load,box.token_id,box.last_heartbeat_at,box.disconnected_at,box.created_at,box.updated_at`, now, heartbeatBefore, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, scanErr := store.scanBox(rows)
			if scanErr != nil {
				return scanErr
			}
			items = append(items, item)
			if store.Audit != nil {
				if err := store.Audit.RecordInTransaction(ctx, tx, audit.Event{Action: "box.offline", ActorKind: "system", Category: "box", Outcome: "error", ProjectID: item.ProjectID, ResourceID: item.ID, ResourceType: "box", Source: "core", OccurredAt: now}); err != nil {
					return err
				}
			}
			if _, err := store.Outbox.Write(ctx, tx, outbox.Event{EventType: "box.heartbeat.received", Payload: map[string]interface{}{"box_id": item.ID, "project_id": item.ProjectID, "status": StatusOffline, "version": item.Version, "running_tasks": item.Load.RunningTasks}, Producer: "boxcontrol", ProjectID: item.ProjectID, OccurredAt: now}); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	return items, err
}

func (store PostgresStore) Bind(ctx context.Context, projectID, boxID string, now time.Time) (Box, error) {
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO box_project_bindings(project_id,box_id,created_at,updated_at) VALUES($1,$2,$3,$3) ON CONFLICT(project_id) DO UPDATE SET box_id=EXCLUDED.box_id,updated_at=EXCLUDED.updated_at`, projectID, boxID, now); err != nil {
		return Box{}, mapConflict(err)
	}
	return store.Get(ctx, boxID)
}

func (store PostgresStore) Unbind(ctx context.Context, projectID string, now time.Time) error {
	_, err := store.DB.ExecContext(ctx, `DELETE FROM box_project_bindings WHERE project_id=$1`, projectID)
	return err
}

func (store PostgresStore) CreateTask(ctx context.Context, task Task) error {
	runSpec, err := json.Marshal(task.RunSpec)
	if err != nil {
		return err
	}
	_, err = store.DB.ExecContext(ctx, `INSERT INTO box_tasks(task_id,experiment_id,project_id,status,attempt,max_attempts,run_spec,resource_usage,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,'{}'::jsonb,$8,$8)`, task.ID, task.ExperimentID, task.ProjectID, task.Status, task.Attempt, task.MaxAttempts, runSpec, task.CreatedAt)
	return mapConflict(err)
}

func (store PostgresStore) GetTask(ctx context.Context, taskID string) (Task, error) {
	return scanTask(store.DB.QueryRowContext(ctx, `SELECT task_id,experiment_id,project_id,COALESCE(box_id::text,''),status,attempt,max_attempts,lease_expires_at,cancel_requested_at,run_spec,exit_code,error_code,error_message,resource_usage,summary,created_at,started_at,finished_at,updated_at FROM box_tasks WHERE task_id=$1`, taskID))
}

func (store PostgresStore) ClaimTask(ctx context.Context, boxID string, now time.Time, lease time.Duration) (*Task, error) {
	expires := now.Add(lease)
	row := store.DB.QueryRowContext(ctx, `
		UPDATE box_tasks SET status='preparing',box_id=$1,attempt=attempt+1,lease_expires_at=$2,started_at=COALESCE(started_at,$3),updated_at=$3
		WHERE task_id=(SELECT t.task_id FROM box_tasks t JOIN box_project_bindings b ON b.project_id=t.project_id JOIN box_nodes n ON n.box_id=b.box_id WHERE b.box_id=$1 AND n.status='online' AND t.status='queued' AND (t.lease_expires_at IS NULL OR t.lease_expires_at <= $3) AND t.cancel_requested_at IS NULL ORDER BY t.created_at,t.task_id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING task_id,experiment_id,project_id,COALESCE(box_id::text,''),status,attempt,max_attempts,lease_expires_at,cancel_requested_at,run_spec,exit_code,error_code,error_message,resource_usage,summary,created_at,started_at,finished_at,updated_at`, boxID, expires, now)
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoTask
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (store PostgresStore) RecoverExpired(ctx context.Context, now time.Time, limit int) ([]Task, error) {
	if limit < 1 {
		return nil, ErrInvalid
	}
	items := make([]Task, 0, limit)
	query := `WITH expired AS (SELECT task_id FROM box_tasks WHERE status IN ('preparing','running') AND lease_expires_at <= $1 ORDER BY lease_expires_at,task_id FOR UPDATE SKIP LOCKED LIMIT $2) UPDATE box_tasks AS task SET status=CASE WHEN task.attempt < task.max_attempts THEN 'queued' ELSE 'timed_out' END,box_id=CASE WHEN task.attempt < task.max_attempts THEN NULL ELSE task.box_id END,lease_expires_at=NULL,error_code=CASE WHEN task.attempt < task.max_attempts THEN NULL ELSE 'LEASE_EXPIRED' END,error_message=CASE WHEN task.attempt < task.max_attempts THEN NULL ELSE 'Box lease expired' END,finished_at=CASE WHEN task.attempt < task.max_attempts THEN NULL ELSE $1 END,updated_at=$1 FROM expired WHERE task.task_id=expired.task_id RETURNING task.task_id,task.experiment_id,task.project_id,COALESCE(task.box_id::text,''),task.status,task.attempt,task.max_attempts,task.lease_expires_at,task.cancel_requested_at,task.run_spec,task.exit_code,task.error_code,task.error_message,task.resource_usage,task.summary,task.created_at,task.started_at,task.finished_at,task.updated_at`
	if store.Transaction.DB == nil {
		return nil, ErrInvalid
	}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		rows, err := tx.QueryContext(ctx, query, now, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			task, scanErr := scanTask(rows)
			if scanErr != nil {
				return scanErr
			}
			items = append(items, task)
		}
		return rows.Err()
	})
	return items, err
}

func (store PostgresStore) RenewTask(ctx context.Context, boxID, taskID string, now time.Time, lease time.Duration) (TaskLease, error) {
	if lease < 10*time.Second || lease > 15*time.Minute {
		return TaskLease{}, ErrInvalid
	}
	expires := now.Add(lease)
	var cancel bool
	var result time.Time
	err := store.DB.QueryRowContext(ctx, `UPDATE box_tasks SET lease_expires_at=$4,updated_at=$3 WHERE task_id=$1 AND box_id=$2 AND status IN ('preparing','running') AND lease_expires_at > $3 RETURNING lease_expires_at,cancel_requested_at IS NOT NULL`, taskID, boxID, now, expires).Scan(&result, &cancel)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskLease{}, ErrLeaseLost
	}
	return TaskLease{TaskID: taskID, LeaseExpiresAt: result, CancelRequested: cancel}, err
}

func (store PostgresStore) CancelTask(ctx context.Context, taskID string, now time.Time) (Task, error) {
	row := store.DB.QueryRowContext(ctx, `UPDATE box_tasks SET cancel_requested_at=$2,status=CASE WHEN status='queued' THEN 'canceled' ELSE status END,finished_at=CASE WHEN status='queued' THEN $2 ELSE finished_at END,updated_at=$2 WHERE task_id=$1 AND status NOT IN ('succeeded','failed','canceled','timed_out') RETURNING task_id,experiment_id,project_id,COALESCE(box_id::text,''),status,attempt,max_attempts,lease_expires_at,cancel_requested_at,run_spec,exit_code,error_code,error_message,resource_usage,summary,created_at,started_at,finished_at,updated_at`, taskID, now)
	return scanTask(row)
}

func (store PostgresStore) AppendLog(ctx context.Context, log Log) (Log, error) {
	if log.ID == "" {
		log.ID, _ = store.Generator.New()
	}
	if log.OccurredAt.IsZero() {
		log.OccurredAt = time.Now().UTC()
	}
	fields, _ := json.Marshal(log.Fields)
	_, err := store.DB.ExecContext(ctx, `INSERT INTO box_task_logs(log_id,task_id,experiment_id,level,message,fields,occurred_at) SELECT $1,$2,experiment_id,$3,$4,$5,$6 FROM box_tasks WHERE task_id=$2`, log.ID, log.TaskID, log.Level, log.Message, fields, log.OccurredAt)
	if err != nil {
		return Log{}, err
	}
	return log, nil
}

func (store PostgresStore) ReportStatus(ctx context.Context, boxID, taskID, status string, exitCode *int, code, message string, usage map[string]interface{}, summary string, now time.Time) (Task, error) {
	usageJSON, _ := json.Marshal(usage)
	row := store.DB.QueryRowContext(ctx, `UPDATE box_tasks SET status=$3,exit_code=$4,error_code=NULLIF($5,''),error_message=NULLIF($6,''),resource_usage=$7,summary=NULLIF($8,''),lease_expires_at=CASE WHEN $3 IN ('succeeded','failed','canceled','timed_out') THEN NULL ELSE lease_expires_at END,finished_at=CASE WHEN $3 IN ('succeeded','failed','canceled','timed_out') THEN $9 ELSE finished_at END,updated_at=$9 WHERE task_id=$1 AND box_id=$2 AND status NOT IN ('succeeded','failed','canceled','timed_out') RETURNING task_id,experiment_id,project_id,COALESCE(box_id::text,''),status,attempt,max_attempts,lease_expires_at,cancel_requested_at,run_spec,exit_code,error_code,error_message,resource_usage,summary,created_at,started_at,finished_at,updated_at`, taskID, boxID, status, exitCode, code, message, usageJSON, summary, now)
	task, err := scanTask(row)
	if err == nil {
		return task, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Task{}, err
	}
	current, currentErr := store.GetTask(ctx, taskID)
	if currentErr != nil {
		return Task{}, currentErr
	}
	if current.BoxID != boxID {
		return Task{}, ErrNotFound
	}
	if current.Status == status {
		return current, nil
	}
	return Task{}, ErrConflict
}

func (store PostgresStore) SubmitResult(ctx context.Context, boxID, taskID string, result Result, now time.Time) (Task, error) {
	manifest, _ := json.Marshal(result.Manifest)
	artifact, _ := json.Marshal(result.Artifact)
	row := store.DB.QueryRowContext(ctx, `UPDATE box_tasks SET status='succeeded',result_manifest=$3,result_artifact=$4,finished_at=$5,lease_expires_at=NULL,updated_at=$5 WHERE task_id=$1 AND box_id=$2 AND status NOT IN ('failed','canceled','timed_out') RETURNING task_id,experiment_id,project_id,COALESCE(box_id::text,''),status,attempt,max_attempts,lease_expires_at,cancel_requested_at,run_spec,exit_code,error_code,error_message,resource_usage,summary,created_at,started_at,finished_at,updated_at`, taskID, boxID, manifest, artifact, now)
	task, err := scanTask(row)
	if err == nil {
		return task, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Task{}, err
	}
	current, currentErr := store.GetTask(ctx, taskID)
	if currentErr != nil {
		return Task{}, currentErr
	}
	if current.BoxID != boxID {
		return Task{}, ErrNotFound
	}
	if current.Status == TaskSucceeded {
		return current, nil
	}
	return Task{}, ErrConflict
}

func (store PostgresStore) ListLogs(ctx context.Context, taskID string, offset, limit int) ([]Log, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT log_id,task_id,experiment_id,level,message,fields,occurred_at FROM box_task_logs WHERE task_id=$1 ORDER BY occurred_at,log_id OFFSET $2 LIMIT $3`, taskID, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Log{}
	for rows.Next() {
		var log Log
		var fields []byte
		if err := rows.Scan(&log.ID, &log.TaskID, &log.ExperimentID, &log.Level, &log.Message, &fields, &log.OccurredAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(fields, &log.Fields); err != nil {
			return nil, err
		}
		items = append(items, log)
	}
	return items, rows.Err()
}

type scanner interface{ Scan(...interface{}) error }

func (store PostgresStore) scanBox(row scanner) (Box, error) {
	var box Box
	var capabilities, runtimes, limits, load []byte
	var heartbeat, disconnected sql.NullTime
	err := row.Scan(&box.ID, &box.ProjectID, &box.Name, &box.Status, &box.Version, &capabilities, &runtimes, &limits, &load, &box.TokenID, &heartbeat, &disconnected, &box.CreatedAt, &box.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Box{}, ErrNotFound
	}
	if err != nil {
		return Box{}, err
	}
	if err := json.Unmarshal(capabilities, &box.Capabilities); err != nil {
		return Box{}, err
	}
	if err := json.Unmarshal(runtimes, &box.Runtimes); err != nil {
		return Box{}, err
	}
	if err := json.Unmarshal(limits, &box.Limits); err != nil {
		return Box{}, err
	}
	if err := json.Unmarshal(load, &box.Load); err != nil {
		return Box{}, err
	}
	if heartbeat.Valid {
		value := heartbeat.Time
		box.LastHeartbeatAt = &value
	}
	if disconnected.Valid {
		value := disconnected.Time
		box.DisconnectedAt = &value
	}
	return box, nil
}

func scanTask(row scanner) (Task, error) {
	var task Task
	var lease, cancel, started, finished sql.NullTime
	var runSpec, usage []byte
	err := row.Scan(&task.ID, &task.ExperimentID, &task.ProjectID, &task.BoxID, &task.Status, &task.Attempt, &task.MaxAttempts, &lease, &cancel, &runSpec, &task.ExitCode, &task.ErrorCode, &task.ErrorMessage, &usage, &task.Summary, &task.CreatedAt, &started, &finished, &task.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	if err := json.Unmarshal(runSpec, &task.RunSpec); err != nil {
		return Task{}, err
	}
	if err := json.Unmarshal(usage, &task.ResourceUsage); err != nil {
		return Task{}, err
	}
	if lease.Valid {
		value := lease.Time
		task.LeaseExpiresAt = &value
	}
	task.CancelRequested = cancel.Valid
	if started.Valid {
		value := started.Time
		task.StartedAt = &value
	}
	if finished.Valid {
		value := finished.Time
		task.FinishedAt = &value
	}
	return task, nil
}

func mapConflict(err error) error {
	if err == nil {
		return nil
	}
	return err
}
