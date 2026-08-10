package progress

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgconn"
	"github.com/mmdash/mmdash/backend/internal/audit"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func (store PostgresStore) GetState(ctx context.Context, projectID string) (TrackerState, error) {
	var item TrackerState
	var changes, completed, inProgress, blockers, questions []byte
	err := store.DB.QueryRowContext(ctx, `
		SELECT project_id,COALESCE(last_evaluation_id::text,''),detected_stage,
		       effective_stage,stage_overridden,summary,changes_since_last,
		       completed_items,in_progress_items,blockers,pending_questions,
		       last_evaluated_at,updated_at
		FROM progress_tracker_state WHERE project_id=$1
	`, projectID).Scan(&item.ProjectID, &item.LastEvaluationID, &item.DetectedStage,
		&item.EffectiveStage, &item.StageOverridden, &item.Summary, &changes,
		&completed, &inProgress, &blockers, &questions, &item.LastEvaluatedAt,
		&item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return emptyTrackerState(projectID), nil
	}
	if err != nil {
		return TrackerState{}, err
	}
	if err := decodeStringSlices([][]byte{changes, completed, inProgress, blockers, questions},
		[]*[]string{&item.ChangesSinceLast, &item.CompletedItems, &item.InProgressItems, &item.Blockers, &item.PendingQuestions}); err != nil {
		return TrackerState{}, err
	}
	return item, nil
}

func (store PostgresStore) GetLatestEvaluation(ctx context.Context, projectID string) (*Evaluation, error) {
	item, err := store.scanEvaluation(store.DB.QueryRowContext(ctx, evaluationSelect+`
		WHERE evaluation.project_id=$1 ORDER BY evaluation.created_at DESC,evaluation.evaluation_id DESC LIMIT 1
	`, projectID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := store.loadEvaluationDetails(ctx, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (store PostgresStore) ListEvaluations(ctx context.Context, projectID string, page pagination.Request) (EvaluationPage, error) {
	cursorTime, cursorID := "", ""
	if page.Cursor != "" {
		cursor, err := pagination.Decode(page.Cursor)
		if err != nil {
			return EvaluationPage{}, ErrInvalid
		}
		cursorTime, cursorID = cursor.SortValue, cursor.ID
	}
	rows, err := store.DB.QueryContext(ctx, evaluationSelect+`
		WHERE evaluation.project_id=$1
		  AND ($2='' OR (evaluation.created_at,evaluation.evaluation_id) < (NULLIF($2,'')::timestamptz,NULLIF($3,'')::uuid))
		ORDER BY evaluation.created_at DESC,evaluation.evaluation_id DESC LIMIT $4
	`, projectID, cursorTime, cursorID, page.Limit+1)
	if err != nil {
		return EvaluationPage{}, err
	}
	defer rows.Close()
	items := make([]Evaluation, 0, page.Limit)
	for rows.Next() {
		item, scanErr := store.scanEvaluation(rows.Scan)
		if scanErr != nil {
			return EvaluationPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return EvaluationPage{}, err
	}
	hasMore := len(items) > page.Limit
	if hasMore {
		items = items[:page.Limit]
	}
	for index := range items {
		if err := store.loadEvaluationDetails(ctx, &items[index]); err != nil {
			return EvaluationPage{}, err
		}
	}
	next := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		next, err = pagination.Encode(pagination.Cursor{ID: last.ID, SortValue: last.CreatedAt.Format(time.RFC3339Nano)})
		if err != nil {
			return EvaluationPage{}, err
		}
	}
	return EvaluationPage{Items: items, HasMore: hasMore, NextCursor: next}, nil
}

func (store PostgresStore) GetEvaluation(ctx context.Context, projectID, evaluationID string) (Evaluation, error) {
	item, err := store.scanEvaluation(store.DB.QueryRowContext(ctx, evaluationSelect+`
		WHERE evaluation.project_id=$1 AND evaluation.evaluation_id=$2
	`, projectID, evaluationID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Evaluation{}, ErrNotFound
	}
	if err != nil {
		return Evaluation{}, err
	}
	return item, store.loadEvaluationDetails(ctx, &item)
}

func (store PostgresStore) GetEvaluationByJob(ctx context.Context, jobID string) (Evaluation, error) {
	item, err := store.scanEvaluation(store.DB.QueryRowContext(ctx, evaluationSelect+`
		WHERE evaluation.job_id=$1
	`, jobID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Evaluation{}, ErrNotFound
	}
	if err != nil {
		return Evaluation{}, err
	}
	return item, store.loadEvaluationDetails(ctx, &item)
}

func (store PostgresStore) GetRisk(ctx context.Context, projectID, riskID string) (Risk, error) {
	var item Risk
	err := store.DB.QueryRowContext(ctx, `
		SELECT risk_id,evaluation_id,project_id,risk_key,title,severity,status,detail,created_at
		FROM progress_evaluation_risks WHERE project_id=$1 AND risk_id=$2
	`, projectID, riskID).Scan(&item.ID, &item.EvaluationID, &item.ProjectID, &item.Key,
		&item.Title, &item.Severity, &item.Status, &item.Detail, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Risk{}, ErrNotFound
	}
	return item, err
}

func (store PostgresStore) GetActiveOverride(ctx context.Context, projectID string) (*StageOverride, error) {
	item, err := scanStageOverride(store.DB.QueryRowContext(ctx, stageOverrideSelect+`
		WHERE project_id=$1 AND active=true
	`, projectID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

func (store PostgresStore) SetStageOverride(ctx context.Context, projectID, actorID, stage, summary, note string) (StageOverride, error) {
	id, err := store.Generator.New()
	if err != nil {
		return StageOverride{}, err
	}
	now := store.now()
	item := StageOverride{ID: id, ProjectID: projectID, Stage: stage, Summary: summary, Note: note, Active: true, CreatedBy: actorID, CreatedAt: now}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE progress_stage_overrides SET active=false,cleared_by=$2,cleared_at=$3 WHERE project_id=$1 AND active=true`, projectID, actorID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_stage_overrides(override_id,project_id,stage,summary,note,active,created_by,created_at) VALUES($1,$2,$3,$4,$5,true,$6,$7)`, id, projectID, stage, summary, note, actorID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO progress_tracker_state(project_id,effective_stage,stage_overridden,summary,updated_at)
			VALUES($1,$2,true,$3,$4)
			ON CONFLICT(project_id) DO UPDATE SET effective_stage=EXCLUDED.effective_stage,
				stage_overridden=true,summary=CASE WHEN EXCLUDED.summary='' THEN progress_tracker_state.summary ELSE EXCLUDED.summary END,updated_at=EXCLUDED.updated_at
		`, projectID, stage, summary, now); err != nil {
			return err
		}
		if err := store.trackingEvent(ctx, tx, actorID, "session", "progress.stage.overridden", projectID, id, map[string]interface{}{
			"resource_type": "progress_stage_override", "stage": stage, "summary": summary,
		}); err != nil {
			return err
		}
		return store.trackingAudit(ctx, tx, actorID, "session", projectID, "progress.stage.overridden", "progress-stage-override", id, "success", "", map[string]interface{}{"stage": stage})
	})
	return item, err
}

func (store PostgresStore) ClearStageOverride(ctx context.Context, projectID, actorID string) (StageOverride, error) {
	now := store.now()
	var item StageOverride
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var err error
		item, err = scanStageOverride(tx.QueryRowContext(ctx, stageOverrideSelect+` WHERE project_id=$1 AND active=true FOR UPDATE`, projectID).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE progress_stage_overrides SET active=false,cleared_by=$2,cleared_at=$3 WHERE override_id=$1`, item.ID, actorID, now); err != nil {
			return err
		}
		item.Active, item.ClearedBy, item.ClearedAt = false, actorID, &now
		if _, err := tx.ExecContext(ctx, `
			UPDATE progress_tracker_state
			SET effective_stage=detected_stage,
				stage_overridden=false,
				summary=COALESCE((
					SELECT evaluation.summary
					FROM progress_evaluations AS evaluation
					WHERE evaluation.evaluation_id=progress_tracker_state.last_evaluation_id
				),summary),
				updated_at=$2
			WHERE project_id=$1
		`, projectID, now); err != nil {
			return err
		}
		if err := store.trackingEvent(ctx, tx, actorID, "session", "progress.stage.override_cleared", projectID, item.ID, map[string]interface{}{
			"resource_type": "progress_stage_override", "stage": item.Stage,
		}); err != nil {
			return err
		}
		return store.trackingAudit(ctx, tx, actorID, "session", projectID, "progress.stage.override.cleared", "progress-stage-override", item.ID, "success", "", map[string]interface{}{})
	})
	return item, err
}

func (store PostgresStore) UpdateTrackingSettings(ctx context.Context, projectID, actorID string, input UpdateTrackingSettingsInput) (Settings, error) {
	now := store.now()
	var item Settings
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if input.AgentInstanceID != "" {
			var valid bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_instances AS instance JOIN agent_project_grants AS grant_row USING(agent_instance_id) WHERE instance.agent_instance_id=$1 AND grant_row.project_id=$2 AND instance.status='active' AND grant_row.status='active')`, input.AgentInstanceID, projectID).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return ErrReferenceInvalid
			}
		}
		cronStatus := "pending"
		if !input.AutoTrackingEnabled && !input.CronEnabled {
			cronStatus = "disabled"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO progress_settings(project_id,auto_task_changes,auto_tracking_enabled,event_triggers_enabled,cron_enabled,cron_schedule,debounce_seconds,min_interval_seconds,agent_instance_id,cron_sync_status,cron_retry_at,updated_by,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid,$10,$11,$12,$11)
			ON CONFLICT(project_id) DO UPDATE SET auto_task_changes=EXCLUDED.auto_task_changes,
				auto_tracking_enabled=EXCLUDED.auto_tracking_enabled,event_triggers_enabled=EXCLUDED.event_triggers_enabled,
				cron_enabled=EXCLUDED.cron_enabled,cron_schedule=EXCLUDED.cron_schedule,
				debounce_seconds=EXCLUDED.debounce_seconds,min_interval_seconds=EXCLUDED.min_interval_seconds,
				agent_instance_id=EXCLUDED.agent_instance_id,cron_sync_status=EXCLUDED.cron_sync_status,
				cron_error_code='',cron_retry_at=EXCLUDED.cron_retry_at,cron_lease_owner='',cron_lease_expires_at=NULL,
				updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at
		`, projectID, input.AutoTaskChanges, input.AutoTrackingEnabled, input.EventTriggersEnabled,
			input.CronEnabled, input.CronSchedule, input.DebounceSeconds, input.MinIntervalSeconds,
			input.AgentInstanceID, cronStatus, now, actorID); err != nil {
			return err
		}
		var err error
		item, err = store.getSettingsTx(ctx, tx, projectID)
		if err != nil {
			return err
		}
		settingsPayload := map[string]interface{}{
			"resource_type": "progress_settings", "auto_task_changes": input.AutoTaskChanges,
			"auto_tracking_enabled": input.AutoTrackingEnabled, "event_triggers_enabled": input.EventTriggersEnabled,
			"cron_enabled": input.CronEnabled, "cron_schedule": input.CronSchedule,
			"debounce_seconds": input.DebounceSeconds, "min_interval_seconds": input.MinIntervalSeconds,
		}
		if input.AgentInstanceID != "" {
			settingsPayload["agent_instance_id"] = input.AgentInstanceID
		}
		if err := store.trackingEvent(ctx, tx, actorID, "session", "progress.settings.updated", projectID, projectID, settingsPayload); err != nil {
			return err
		}
		return store.trackingAudit(ctx, tx, actorID, "session", projectID, "progress.settings.updated", "progress-settings", projectID, "success", "", map[string]interface{}{"auto_tracking_enabled": input.AutoTrackingEnabled})
	})
	return item, err
}

func (store PostgresStore) ScheduleRequest(ctx context.Context, projectID, actorID, actorKind, triggerKind string, force bool, trigger EvaluationTrigger) (RecalculateResult, error) {
	var result RecalculateResult
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, projectID); err != nil {
			return err
		}
		if trigger.SourceEventID != "" {
			var requestID string
			var scheduled time.Time
			err := tx.QueryRowContext(ctx, `
				SELECT request.request_id,request.scheduled_for
				FROM progress_evaluation_triggers AS trigger
				JOIN progress_evaluation_requests AS request USING(request_id)
				WHERE trigger.source_event_id=$1
			`, trigger.SourceEventID).Scan(&requestID, &scheduled)
			if err == nil {
				result = RecalculateResult{RequestID: requestID, Status: "pending", ScheduledFor: scheduled, Merged: true}
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		settings, err := store.getSettingsTx(ctx, tx, projectID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		actorID, err = store.resolveTrackingActor(ctx, tx, projectID, actorID, settings.AgentInstanceID)
		if err != nil {
			return err
		}
		now := store.now()
		scheduled := now
		if triggerKind == "event" {
			scheduled = now.Add(time.Duration(settings.DebounceSeconds) * time.Second)
		}
		if !force && settings.MinIntervalSeconds > 0 {
			var last sql.NullTime
			if err := tx.QueryRowContext(ctx, `SELECT last_evaluated_at FROM progress_tracker_state WHERE project_id=$1`, projectID).Scan(&last); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if last.Valid {
				minimum := last.Time.Add(time.Duration(settings.MinIntervalSeconds) * time.Second)
				if minimum.After(scheduled) {
					scheduled = minimum
				}
			}
		}
		var requestID, status string
		var existingScheduled time.Time
		err = tx.QueryRowContext(ctx, `SELECT request_id,status,scheduled_for FROM progress_evaluation_requests WHERE project_id=$1 AND status IN ('pending','assembling') FOR UPDATE`, projectID).Scan(&requestID, &status, &existingScheduled)
		merged := err == nil
		if errors.Is(err, sql.ErrNoRows) {
			requestID, err = store.Generator.New()
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO progress_evaluation_requests(request_id,project_id,trigger_kind,status,scheduled_for,actor_id,requested_by_kind,force,created_at,updated_at) VALUES($1,$2,$3,'pending',$4,$5,$6,$7,$8,$8)`, requestID, projectID, triggerKind, scheduled, actorID, normalizedActorKind(actorKind), force, now); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if triggerKind == "manual" || triggerKind == "retry" || triggerKind == "cron" {
				if scheduled.Before(existingScheduled) {
					existingScheduled = scheduled
				}
			} else if scheduled.After(existingScheduled) {
				existingScheduled = scheduled
			}
			if _, err := tx.ExecContext(ctx, `UPDATE progress_evaluation_requests SET scheduled_for=$2,trigger_kind=CASE WHEN $3 IN ('manual','retry') THEN $3 ELSE trigger_kind END,force=force OR $4,actor_id=$5,requested_by_kind=$6,updated_at=$7 WHERE request_id=$1`, requestID, existingScheduled, triggerKind, force, actorID, normalizedActorKind(actorKind), now); err != nil {
				return err
			}
			scheduled = existingScheduled
		}
		triggerID := trigger.ID
		if triggerID == "" {
			triggerID, err = store.Generator.New()
			if err != nil {
				return err
			}
		}
		payload, _ := json.Marshal(nonNilMap(trigger.Payload))
		var sourceEvent interface{}
		if trigger.SourceEventID != "" {
			sourceEvent = trigger.SourceEventID
		}
		if trigger.OccurredAt.IsZero() {
			trigger.OccurredAt = now
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_evaluation_triggers(trigger_id,request_id,project_id,trigger_type,source_event_id,source_event_type,source_resource_id,source_version,payload,occurred_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(source_event_id) WHERE source_event_id IS NOT NULL DO NOTHING`, triggerID, requestID, projectID, trigger.TriggerType, sourceEvent, trigger.SourceEventType, trigger.SourceResourceID, trigger.SourceVersion, payload, trigger.OccurredAt, now); err != nil {
			return err
		}
		if !merged {
			if err := store.trackingEvent(ctx, tx, actorID, normalizedActorKind(actorKind), "progress.evaluation.requested", projectID, requestID, map[string]interface{}{"trigger_kind": triggerKind, "scheduled_for": scheduled.Format(time.RFC3339Nano)}); err != nil {
				return err
			}
		}
		result = RecalculateResult{RequestID: requestID, Status: "pending", ScheduledFor: scheduled, Merged: merged}
		return nil
	})
	return result, err
}

func (store PostgresStore) ScheduleEvent(ctx context.Context, event contract.EventEnvelope, actorID string) error {
	if event.ProjectID == nil {
		return ErrInvalid
	}
	settings, err := store.GetSettings(ctx, *event.ProjectID)
	if err != nil {
		return err
	}
	if !settings.AutoTrackingEnabled || !settings.EventTriggersEnabled {
		return nil
	}
	resourceID := stringMapValue(event.Payload, "resource_id")
	version := firstNonEmptyTracking(stringMapValue(event.Payload, "version"), stringMapValue(event.Payload, "content_hash"), stringMapValue(event.Payload, "commit_sha"))
	_, err = store.ScheduleRequest(ctx, *event.ProjectID, actorID, "system", "event", false, EvaluationTrigger{
		TriggerType: event.EventType, SourceEventID: event.EventID, SourceEventType: event.EventType,
		SourceResourceID: resourceID, SourceVersion: version, Payload: event.Payload, OccurredAt: event.OccurredAt,
	})
	return err
}

func (store PostgresStore) ClaimRequest(ctx context.Context, owner string, lease time.Duration) (*RequestClaim, error) {
	now := store.now()
	var claim *RequestClaim
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE progress_evaluation_requests SET status='pending',lease_owner='',lease_expires_at=NULL,scheduled_for=$1,error_code='ASSEMBLY_LEASE_EXPIRED',updated_at=$1 WHERE status='assembling' AND lease_expires_at <= $1`, now); err != nil {
			return err
		}
		var item RequestClaim
		err := tx.QueryRowContext(ctx, `
			SELECT request_id,project_id,actor_id,requested_by_kind,trigger_kind,force,scheduled_for
			FROM progress_evaluation_requests WHERE status='pending' AND scheduled_for <= $1
			ORDER BY scheduled_for,created_at,request_id FOR UPDATE SKIP LOCKED LIMIT 1
		`, now).Scan(&item.ID, &item.ProjectID, &item.ActorID, &item.RequestedByKind, &item.TriggerKind, &item.Force, &item.ScheduledFor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE progress_evaluation_requests SET status='assembling',attempts=attempts+1,lease_owner=$2,lease_expires_at=$3,updated_at=$4 WHERE request_id=$1`, item.ID, owner, now.Add(lease), now); err != nil {
			return err
		}
		item.LeaseOwner = owner
		claim = &item
		return nil
	})
	return claim, err
}

func (store PostgresStore) ReleaseRequest(ctx context.Context, requestID, owner, errorCode string, retry time.Duration) error {
	now := store.now()
	result, err := store.DB.ExecContext(ctx, `UPDATE progress_evaluation_requests SET status='pending',scheduled_for=$4,lease_owner='',lease_expires_at=NULL,error_code=$3,error_message='',updated_at=$5 WHERE request_id=$1 AND status='assembling' AND lease_owner=$2`, requestID, owner, errorCode, now.Add(retry), now)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrConflict
	}
	return nil
}

func (store PostgresStore) EvaluationContext(ctx context.Context, projectID string) (map[string]interface{}, error) {
	settings, err := store.GetSettings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	milestones, err := store.ListMilestones(ctx, projectID)
	if err != nil {
		return nil, err
	}
	tasks, err := store.ListTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	state, err := store.GetState(ctx, projectID)
	if err != nil {
		return nil, err
	}
	last, err := store.GetLatestEvaluation(ctx, projectID)
	if err != nil {
		return nil, err
	}
	override, err := store.GetActiveOverride(ctx, projectID)
	if err != nil {
		return nil, err
	}
	stableMilestones := make([]map[string]interface{}, 0, len(milestones))
	for _, item := range milestones {
		stableMilestones = append(stableMilestones, map[string]interface{}{
			"milestone_id": item.ID, "title": item.Title, "description": item.Description,
			"status": item.Status, "critical": item.Critical, "start_at": item.StartAt,
			"target_at": item.TargetAt, "completed_at": item.CompletedAt,
			"source": item.Source,
		})
	}
	stableTasks := make([]map[string]interface{}, 0, len(tasks))
	for _, item := range tasks {
		stableTasks = append(stableTasks, map[string]interface{}{
			"task_id": item.ID, "milestone_id": item.MilestoneID, "title": item.Title,
			"description": item.Description, "status": item.Status, "assignee_id": item.AssigneeID,
			"start_at": item.StartAt, "due_at": item.DueAt, "completed_at": item.CompletedAt,
			"source":                 item.Source,
			"manual_override_fields": nonNilStrings(item.ManualOverrideFields),
			"related_object_ids":     nonNilStrings(item.RelatedObjectIDs),
		})
	}
	stableState := map[string]interface{}{
		"detected_stage": state.DetectedStage, "effective_stage": state.EffectiveStage,
		"stage_overridden": state.StageOverridden, "summary": state.Summary,
		"changes_since_last": nonNilStrings(state.ChangesSinceLast),
		"completed_items":    nonNilStrings(state.CompletedItems),
		"in_progress_items":  nonNilStrings(state.InProgressItems),
		"blockers":           nonNilStrings(state.Blockers),
		"pending_questions":  nonNilStrings(state.PendingQuestions),
	}
	stableSettings := map[string]interface{}{
		"auto_task_changes":      settings.AutoTaskChanges,
		"auto_tracking_enabled":  settings.AutoTrackingEnabled,
		"event_triggers_enabled": settings.EventTriggersEnabled,
		"cron_enabled":           settings.CronEnabled, "cron_schedule": settings.CronSchedule,
		"debounce_seconds":     settings.DebounceSeconds,
		"min_interval_seconds": settings.MinIntervalSeconds,
		"agent_instance_id":    settings.AgentInstanceID,
		"evaluator_mode":       settings.EvaluatorMode,
	}
	var stableOverride interface{}
	if override != nil {
		stableOverride = map[string]interface{}{
			"stage": override.Stage, "summary": override.Summary, "note": override.Note,
		}
	}
	var previousOutput interface{}
	if last != nil && last.Output != nil {
		previousOutput = last.Output
	}
	return map[string]interface{}{
		"progress": map[string]interface{}{
			"milestones": stableMilestones, "tasks": stableTasks, "tracker_state": stableState,
			"settings": stableSettings, "active_stage_override": stableOverride,
			"previous_evaluation_output": previousOutput,
		},
	}, nil
}

func (store PostgresStore) FinalizeRequest(ctx context.Context, claim RequestClaim, input map[string]interface{}, inputVersion string) (*Evaluation, error) {
	var result *Evaluation
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status, leaseOwner string
		if err := tx.QueryRowContext(ctx, `SELECT status,lease_owner FROM progress_evaluation_requests WHERE request_id=$1 FOR UPDATE`, claim.ID).Scan(&status, &leaseOwner); err != nil {
			return err
		}
		if status != "assembling" || leaseOwner != claim.LeaseOwner {
			return ErrConflict
		}
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT evaluation_id FROM progress_evaluations WHERE project_id=$1 AND input_version=$2 AND status IN ('queued','running','succeeded') ORDER BY created_at DESC LIMIT 1`, claim.ProjectID, inputVersion).Scan(&existingID)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE progress_evaluation_requests SET status='merged',merged_into_evaluation_id=$2,lease_owner='',lease_expires_at=NULL,error_code='',updated_at=$3 WHERE request_id=$1`, claim.ID, existingID, store.now())
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		evaluationID, err := store.Generator.New()
		if err != nil {
			return err
		}
		now := store.now()
		encodedInput, err := json.Marshal(input)
		if err != nil {
			return ErrInvalid
		}
		sourceIDs, err := store.requestSourceEventIDs(ctx, tx, claim.ID)
		if err != nil {
			return err
		}
		encodedSources, _ := json.Marshal(sourceIDs)
		settings, err := store.getSettingsTx(ctx, tx, claim.ProjectID)
		if err != nil {
			return err
		}
		insert, err := tx.ExecContext(ctx, `
			INSERT INTO progress_evaluations(evaluation_id,request_id,project_id,status,input_version,input_snapshot,source_event_ids,trigger_kind,agent_instance_id,evaluator_mode,requested_by,created_at,updated_at)
			VALUES($1,$2,$3,'queued',$4,$5,$6,$7,NULLIF($8,'')::uuid,$9,$10,$11,$11)
			ON CONFLICT(project_id,input_version) WHERE status IN ('queued','running','succeeded') DO NOTHING
		`, evaluationID, claim.ID, claim.ProjectID, inputVersion, encodedInput, encodedSources, claim.TriggerKind, settings.AgentInstanceID, store.evaluatorMode(), claim.ActorID, now)
		if err != nil {
			return err
		}
		inserted, _ := insert.RowsAffected()
		if inserted == 0 {
			if err := tx.QueryRowContext(ctx, `SELECT evaluation_id FROM progress_evaluations WHERE project_id=$1 AND input_version=$2 AND status IN ('queued','running','succeeded') ORDER BY created_at DESC LIMIT 1`, claim.ProjectID, inputVersion).Scan(&existingID); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE progress_evaluation_requests SET status='merged',merged_into_evaluation_id=$2,lease_owner='',lease_expires_at=NULL,updated_at=$3 WHERE request_id=$1`, claim.ID, existingID, now)
			return err
		}
		job, _, err := store.Jobs.CreateInTransaction(ctx, tx, claim.ActorID, jobs.CreateInput{
			ProjectID: claim.ProjectID, JobType: EvaluationJobType,
			Payload: map[string]interface{}{"evaluation_id": evaluationID}, Priority: 10,
			IdempotencyKey: "progress-evaluation:" + evaluationID, MaxAttempts: 3, TimeoutSeconds: 900,
		})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE progress_evaluations SET job_id=$2 WHERE evaluation_id=$1`, evaluationID, job.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE progress_evaluation_requests SET status='queued',lease_owner='',lease_expires_at=NULL,error_code='',error_message='',updated_at=$2 WHERE request_id=$1`, claim.ID, now); err != nil {
			return err
		}
		if err := store.trackingEvent(ctx, tx, claim.ActorID, claim.RequestedByKind, "progress.evaluation.queued", claim.ProjectID, evaluationID, map[string]interface{}{"job_id": job.ID, "input_version": inputVersion, "trigger_kind": claim.TriggerKind}); err != nil {
			return err
		}
		result = &Evaluation{ID: evaluationID, RequestID: claim.ID, ProjectID: claim.ProjectID, JobID: job.ID, Status: "queued", InputVersion: inputVersion, InputSnapshot: input, SourceEventIDs: sourceIDs, TriggerKind: claim.TriggerKind, AgentInstanceID: settings.AgentInstanceID, EvaluatorMode: store.evaluatorMode(), RequestedBy: claim.ActorID, CreatedAt: now, UpdatedAt: now, Triggers: []EvaluationTrigger{}, Risks: []Risk{}}
		return nil
	})
	return result, err
}

func (store PostgresStore) ClaimCron(ctx context.Context, owner string, lease time.Duration) (*CronClaim, error) {
	now := store.now()
	var claim *CronClaim
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE progress_settings SET cron_sync_status='failed',cron_error_code='CRON_LEASE_EXPIRED',cron_retry_at=$1,cron_lease_owner='',cron_lease_expires_at=NULL WHERE cron_sync_status='syncing' AND cron_lease_expires_at <= $1`, now); err != nil {
			return err
		}
		var item CronClaim
		err := tx.QueryRowContext(ctx, `
			SELECT project_id,COALESCE(agent_instance_id::text,''),cron_remote_job_id,cron_schedule,(auto_tracking_enabled AND cron_enabled)
			FROM progress_settings
			WHERE (cron_sync_status='pending' OR (cron_sync_status='failed' AND COALESCE(cron_retry_at,$1) <= $1))
			  AND (agent_instance_id IS NOT NULL OR cron_remote_job_id <> '')
			ORDER BY updated_at,project_id FOR UPDATE SKIP LOCKED LIMIT 1
		`, now).Scan(&item.ProjectID, &item.AgentInstanceID, &item.RemoteJobID, &item.Schedule, &item.Enabled)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE progress_settings SET cron_sync_status='syncing',cron_lease_owner=$2,cron_lease_expires_at=$3 WHERE project_id=$1`, item.ProjectID, owner, now.Add(lease)); err != nil {
			return err
		}
		claim = &item
		return nil
	})
	return claim, err
}

func (store PostgresStore) CompleteCron(ctx context.Context, projectID, owner, remoteJobID string) error {
	now := store.now()
	status := "ready"
	if remoteJobID == "" {
		status = "disabled"
	}
	result, err := store.DB.ExecContext(ctx, `UPDATE progress_settings SET cron_remote_job_id=$3,cron_sync_status=$4,cron_error_code='',cron_synced_at=$5,cron_retry_at=NULL,cron_lease_owner='',cron_lease_expires_at=NULL WHERE project_id=$1 AND cron_sync_status='syncing' AND cron_lease_owner=$2`, projectID, owner, remoteJobID, status, now)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrConflict
	}
	return nil
}

func (store PostgresStore) FailCron(ctx context.Context, projectID, owner, code string, retry time.Duration) error {
	now := store.now()
	result, err := store.DB.ExecContext(ctx, `UPDATE progress_settings SET cron_sync_status='failed',cron_error_code=$3,cron_retry_at=$4,cron_lease_owner='',cron_lease_expires_at=NULL WHERE project_id=$1 AND cron_sync_status='syncing' AND cron_lease_owner=$2`, projectID, owner, code, now.Add(retry))
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrConflict
	}
	return nil
}

func (store PostgresStore) MarkEvaluationRunning(ctx context.Context, tx transaction.Tx, job jobs.Job) error {
	now := store.now()
	var evaluationID, projectID, actorID string
	err := tx.QueryRowContext(ctx, `UPDATE progress_evaluations SET status='running',attempts=$2,started_at=COALESCE(started_at,$3),error_code='',error_message='',updated_at=$3 WHERE job_id=$1 AND status IN ('queued','running') RETURNING evaluation_id,project_id,requested_by`, job.ID, job.Attempts, now).Scan(&evaluationID, &projectID, &actorID)
	if err != nil {
		return err
	}
	if err := store.trackingEvent(ctx, tx, actorID, "system", "progress.evaluation.started", projectID, evaluationID, map[string]interface{}{"job_id": job.ID, "attempt": job.Attempts}); err != nil {
		return err
	}
	return store.trackingAudit(ctx, tx, actorID, "system", projectID, "progress.evaluation.started", "progress-evaluation", evaluationID, "success", "", map[string]interface{}{"attempt": job.Attempts})
}

func (store PostgresStore) CompleteEvaluation(ctx context.Context, tx transaction.Tx, job jobs.Job, result map[string]interface{}) error {
	output, err := decodeEvaluationResult(result)
	if err != nil {
		return err
	}
	var evaluationID, projectID, actorID, agentInstanceID string
	if err := tx.QueryRowContext(ctx, `SELECT evaluation_id,project_id,requested_by,COALESCE(agent_instance_id::text,'') FROM progress_evaluations WHERE job_id=$1 FOR UPDATE`, job.ID).Scan(&evaluationID, &projectID, &actorID, &agentInstanceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, projectID); err != nil {
		return err
	}
	mode := firstNonEmptyTracking(stringMapValue(result, "evaluator_mode"), "core_agent")
	agentSessionID, agentRunID := stringMapValue(result, "agent_session_id"), stringMapValue(result, "agent_run_id")
	if value := stringMapValue(result, "agent_instance_id"); value != "" {
		agentInstanceID = value
	}
	now := store.now()
	settings, err := store.getSettingsTx(ctx, tx, projectID)
	if err != nil {
		return err
	}
	for _, suggestion := range output.Suggestions {
		if err := store.applyEvaluationSuggestion(ctx, tx, projectID, evaluationID, actorID, agentRunID, settings.AutoTaskChanges, suggestion, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM progress_evaluation_risks WHERE evaluation_id=$1`, evaluationID); err != nil {
		return err
	}
	for _, candidate := range output.Risks {
		riskID, err := store.Generator.New()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_evaluation_risks(risk_id,evaluation_id,project_id,risk_key,title,severity,status,detail,created_at) VALUES($1,$2,$3,$4,$5,$6,'open',$7,$8)`, riskID, evaluationID, projectID, candidate.Key, candidate.Title, candidate.Severity, candidate.Detail, now); err != nil {
			return err
		}
		if err := store.trackingEvent(ctx, tx, actorID, "system", "progress.risk.detected", projectID, riskID, map[string]interface{}{
			"resource_type": "progress_risk", "title": candidate.Title, "status": "open",
			"evaluation_id": evaluationID, "risk_key": candidate.Key, "severity": candidate.Severity,
		}); err != nil {
			return err
		}
		if err := store.trackingAudit(ctx, tx, actorID, "system", projectID, "progress.risk.detected", "progress-risk", riskID, "success", "", map[string]interface{}{"evaluation_id": evaluationID, "severity": candidate.Severity}); err != nil {
			return err
		}
	}
	encodedOutput, _ := json.Marshal(output)
	changes, _ := json.Marshal(output.ChangesSinceLast)
	completed, _ := json.Marshal(output.CompletedItems)
	inProgress, _ := json.Marshal(output.InProgressItems)
	blockers, _ := json.Marshal(output.Blockers)
	questions, _ := json.Marshal(output.PendingQuestions)
	if _, err := tx.ExecContext(ctx, `UPDATE progress_evaluations SET status='succeeded',output_snapshot=$2,detected_stage=$3,summary=$4,changes_since_last=$5,completed_items=$6,in_progress_items=$7,blockers=$8,pending_questions=$9,agent_instance_id=NULLIF($10,'')::uuid,agent_session_id=NULLIF($11,'')::uuid,agent_run_id=NULLIF($12,'')::uuid,evaluator_mode=$13,error_code='',error_message='',completed_at=$14,updated_at=$14 WHERE evaluation_id=$1`, evaluationID, encodedOutput, output.Stage, output.Summary, changes, completed, inProgress, blockers, questions, agentInstanceID, agentSessionID, agentRunID, mode, now); err != nil {
		return err
	}
	var overrideStage, overrideSummary string
	_ = tx.QueryRowContext(ctx, `SELECT stage,summary FROM progress_stage_overrides WHERE project_id=$1 AND active=true`, projectID).Scan(&overrideStage, &overrideSummary)
	effective, summary, overridden := output.Stage, output.Summary, false
	if overrideStage != "" {
		effective, overridden = overrideStage, true
		if overrideSummary != "" {
			summary = overrideSummary
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO progress_tracker_state(project_id,last_evaluation_id,detected_stage,effective_stage,stage_overridden,summary,changes_since_last,completed_items,in_progress_items,blockers,pending_questions,last_evaluated_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12) ON CONFLICT(project_id) DO UPDATE SET last_evaluation_id=EXCLUDED.last_evaluation_id,detected_stage=EXCLUDED.detected_stage,effective_stage=EXCLUDED.effective_stage,stage_overridden=EXCLUDED.stage_overridden,summary=EXCLUDED.summary,changes_since_last=EXCLUDED.changes_since_last,completed_items=EXCLUDED.completed_items,in_progress_items=EXCLUDED.in_progress_items,blockers=EXCLUDED.blockers,pending_questions=EXCLUDED.pending_questions,last_evaluated_at=EXCLUDED.last_evaluated_at,updated_at=EXCLUDED.updated_at`, projectID, evaluationID, output.Stage, effective, overridden, summary, changes, completed, inProgress, blockers, questions, now); err != nil {
		return err
	}
	if err := store.trackingEvent(ctx, tx, actorID, "system", "progress.evaluation.completed", projectID, evaluationID, map[string]interface{}{"title": "Progress evaluation", "status": "succeeded", "stage": output.Stage, "effective_stage": effective, "summary": summary, "risk_count": len(output.Risks), "suggestion_count": len(output.Suggestions)}); err != nil {
		return err
	}
	return store.trackingAudit(ctx, tx, actorID, "system", projectID, "progress.evaluation.completed", "progress-evaluation", evaluationID, "success", "", map[string]interface{}{"stage": effective, "evaluator_mode": mode})
}

func (store PostgresStore) FailEvaluation(ctx context.Context, tx transaction.Tx, job jobs.Job, failure jobs.Failure) error {
	now := store.now()
	status := "failed"
	if job.Status == jobs.StatusQueued {
		status = "queued"
	}
	message := safeTrackingMessage(failure.Message, 1000)
	var evaluationID, projectID, actorID string
	err := tx.QueryRowContext(ctx, `UPDATE progress_evaluations SET status=$2,attempts=$3,error_code=$4,error_message=$5,completed_at=CASE WHEN $2='failed' THEN $6::timestamptz ELSE NULL::timestamptz END,updated_at=$6 WHERE job_id=$1 RETURNING evaluation_id,project_id,requested_by`, job.ID, status, job.Attempts, failure.Code, message, now).Scan(&evaluationID, &projectID, &actorID)
	if err != nil || status != "failed" {
		return err
	}
	if err := store.trackingEvent(ctx, tx, actorID, "system", "progress.evaluation.failed", projectID, evaluationID, map[string]interface{}{"title": "Progress evaluation", "status": "failed", "error_code": failure.Code, "attempts": job.Attempts}); err != nil {
		return err
	}
	return store.trackingAudit(ctx, tx, actorID, "system", projectID, "progress.evaluation.failed", "progress-evaluation", evaluationID, "error", failure.Code, map[string]interface{}{"attempts": job.Attempts})
}

func (store PostgresStore) applyEvaluationSuggestion(ctx context.Context, tx transaction.Tx, projectID, evaluationID, actorID, runID string, autoTasks bool, suggestion EvaluationSuggestion, now time.Time) error {
	if strings.HasPrefix(suggestion.ProposalType, "milestone.") || !autoTasks {
		return store.createEvaluationProposal(ctx, tx, projectID, evaluationID, actorID, runID, suggestion, now)
	}
	switch suggestion.ProposalType {
	case "task.create":
		targetID, changes, err := store.validateProposalReferences(ctx, tx, projectID, suggestion.ProposalType, suggestion.TargetID, suggestion.Changes)
		if err != nil {
			return err
		}
		suggestion.TargetID, suggestion.Changes = targetID, changes
		return store.createEvaluationTask(ctx, tx, projectID, evaluationID, actorID, runID, suggestion, now)
	case "task.update":
		targetID, changes, err := store.validateProposalReferences(ctx, tx, projectID, suggestion.ProposalType, suggestion.TargetID, suggestion.Changes)
		if err != nil {
			return err
		}
		suggestion.TargetID, suggestion.Changes = targetID, changes
		return store.updateEvaluationTask(ctx, tx, projectID, evaluationID, actorID, runID, suggestion, now)
	default:
		return store.createEvaluationProposal(ctx, tx, projectID, evaluationID, actorID, runID, suggestion, now)
	}
}

func (store PostgresStore) createEvaluationProposal(ctx context.Context, tx transaction.Tx, projectID, evaluationID, actorID, runID string, suggestion EvaluationSuggestion, now time.Time) error {
	targetID, changes, err := store.validateProposalReferences(ctx, tx, projectID, suggestion.ProposalType, suggestion.TargetID, suggestion.Changes)
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(changes)
	if suggestion.Key != "" {
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT proposal_id FROM progress_proposals WHERE project_id=$1 AND source_key=$2 AND status='pending' AND proposal_type=$3 AND COALESCE(target_id::text,'')=$4 AND title=$5 AND rationale=$6 AND changes=$7::jsonb LIMIT 1`, projectID, suggestion.Key, suggestion.ProposalType, targetID, suggestion.Title, suggestion.Rationale, encoded).Scan(&existingID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	proposalID, err := store.Generator.New()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO progress_proposals(proposal_id,project_id,proposal_type,target_id,title,rationale,changes,source,source_run_id,source_evaluation_id,source_key,proposed_by,status,review_note,created_at,updated_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,'system',$8,$9,$10,$11,'pending','',$12,$12) ON CONFLICT(source_evaluation_id,source_key) WHERE source_evaluation_id IS NOT NULL AND source_key<>'' DO NOTHING`, proposalID, projectID, suggestion.ProposalType, targetID, suggestion.Title, suggestion.Rationale, encoded, firstNonEmptyTracking(runID, evaluationID), evaluationID, suggestion.Key, actorID, now); err != nil {
		return err
	}
	if err := store.progressEvent(ctx, tx, "progress.proposal.created", projectID, actorID, proposalID, "progress_proposal", suggestion.Title, "pending", map[string]interface{}{"proposal_type": suggestion.ProposalType, "source": "system", "source_run_id": firstNonEmptyTracking(runID, evaluationID), "source_evaluation_id": evaluationID}); err != nil {
		return err
	}
	return store.trackingAudit(ctx, tx, actorID, "system", projectID, "progress.proposal.created", "progress-proposal", proposalID, "success", "", map[string]interface{}{"evaluation_id": evaluationID, "proposal_type": suggestion.ProposalType})
}

func (store PostgresStore) createEvaluationTask(ctx context.Context, tx transaction.Tx, projectID, evaluationID, actorID, runID string, suggestion EvaluationSuggestion, now time.Time) error {
	if suggestion.Key != "" {
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT task_id FROM progress_tasks WHERE project_id=$1 AND source_key=$2`, projectID, suggestion.Key).Scan(&existingID)
		if err == nil {
			suggestion.TargetID = existingID
			return store.updateEvaluationTask(ctx, tx, projectID, evaluationID, actorID, runID, suggestion, now)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	changes := suggestion.Changes
	input := CreateTaskInput{MilestoneID: stringChange(changes, "milestone_id"), Title: stringChange(changes, "title"), Description: stringChange(changes, "description"), Status: stringChange(changes, "status"), AssigneeID: stringChange(changes, "assignee_id"), StartAt: timeChange(changes, "start_at"), DueAt: timeChange(changes, "due_at")}
	if values, _, err := relatedObjectIDsChange(changes); err != nil {
		return err
	} else {
		input.RelatedObjectIDs = values
	}
	if input.Title == "" {
		input.Title = suggestion.Title
	}
	if input.Status == "" {
		input.Status = TaskTodo
	}
	if input.Title == "" || !validTaskStatus(input.Status) {
		return ErrInvalidEvaluationOutput
	}
	taskID, err := store.Generator.New()
	if err != nil {
		return err
	}
	references, _ := json.Marshal(nonNilStrings(input.RelatedObjectIDs))
	var completed interface{}
	if input.Status == TaskDone {
		completed = now
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO progress_tasks(task_id,project_id,milestone_id,title,description,status,assignee_id,start_at,due_at,completed_at,source,source_run_id,source_evaluation_id,source_key,related_object_ids,manual_override_fields,created_by,updated_by,created_at,updated_at) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,NULLIF($7,'')::uuid,$8,$9,$10,'agent',$11,$12,$13,$14,'[]'::jsonb,$15,$15,$16,$16)`, taskID, projectID, input.MilestoneID, input.Title, input.Description, input.Status, input.AssigneeID, input.StartAt, input.DueAt, completed, firstNonEmptyTracking(runID, evaluationID), evaluationID, suggestion.Key, references, actorID, now); err != nil {
		return mapPostgresMutationError(err)
	}
	if err := store.progressEvent(ctx, tx, "progress.task.created", projectID, actorID, taskID, "task", input.Title, input.Status, map[string]interface{}{"source": "agent", "source_run_id": firstNonEmptyTracking(runID, evaluationID), "source_evaluation_id": evaluationID}); err != nil {
		return err
	}
	return store.trackingAudit(ctx, tx, actorID, "system", projectID, "progress.task.created", "task", taskID, "success", "", map[string]interface{}{"evaluation_id": evaluationID})
}

func (store PostgresStore) updateEvaluationTask(ctx context.Context, tx transaction.Tx, projectID, evaluationID, actorID, runID string, suggestion EvaluationSuggestion, now time.Time) error {
	if suggestion.TargetID == "" {
		return ErrInvalidEvaluationOutput
	}
	current, err := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE project_id=$1 AND task_id=$2 FOR UPDATE`, projectID, suggestion.TargetID).Scan)
	if err != nil {
		return mapNotFound(err)
	}
	previous := current
	changes := map[string]interface{}{}
	overrides := map[string]bool{}
	for _, field := range current.ManualOverrideFields {
		overrides[field] = true
	}
	for key, value := range suggestion.Changes {
		if !overrides[key] {
			changes[key] = value
		}
	}
	if len(changes) == 0 {
		return nil
	}
	if value, ok := changes["title"].(string); ok {
		current.Title = strings.TrimSpace(value)
	}
	if value, ok := changes["description"].(string); ok {
		current.Description = value
	}
	if value, ok := changes["status"].(string); ok {
		current.Status = value
	}
	if value, ok := changes["assignee_id"].(string); ok {
		current.AssigneeID = value
	}
	if value, ok := changes["milestone_id"].(string); ok {
		current.MilestoneID = value
	}
	if values, present, err := relatedObjectIDsChange(changes); err != nil {
		return err
	} else if present {
		current.RelatedObjectIDs = values
	}
	if value := timeChange(changes, "start_at"); value != nil {
		current.StartAt = value
	}
	if value := timeChange(changes, "due_at"); value != nil {
		current.DueAt = value
	}
	if current.Title == "" || !validTaskStatus(current.Status) {
		return ErrInvalidEvaluationOutput
	}
	if trackingTasksEqual(previous, current) {
		return nil
	}
	var completed interface{}
	if current.Status == TaskDone {
		if previous.Status == TaskDone && previous.CompletedAt != nil {
			completed = previous.CompletedAt
		} else {
			completed = now
		}
	}
	references, _ := json.Marshal(nonNilStrings(current.RelatedObjectIDs))
	if _, err := tx.ExecContext(ctx, `UPDATE progress_tasks SET milestone_id=NULLIF($3,'')::uuid,title=$4,description=$5,status=$6,assignee_id=NULLIF($7,'')::uuid,start_at=$8,due_at=$9,completed_at=$10,source='agent',source_run_id=$11,source_evaluation_id=$12,related_object_ids=$13,updated_by=$14,updated_at=$15 WHERE project_id=$1 AND task_id=$2`, projectID, current.ID, current.MilestoneID, current.Title, current.Description, current.Status, current.AssigneeID, current.StartAt, current.DueAt, completed, firstNonEmptyTracking(runID, evaluationID), evaluationID, references, actorID, now); err != nil {
		return mapPostgresMutationError(err)
	}
	if err := store.progressEvent(ctx, tx, "progress.task.updated", projectID, actorID, current.ID, "task", current.Title, current.Status, map[string]interface{}{"source": "agent", "source_run_id": firstNonEmptyTracking(runID, evaluationID), "source_evaluation_id": evaluationID}); err != nil {
		return err
	}
	return store.trackingAudit(ctx, tx, actorID, "system", projectID, "progress.task.updated", "task", current.ID, "success", "", map[string]interface{}{"evaluation_id": evaluationID})
}

func trackingTasksEqual(left, right Task) bool {
	return left.MilestoneID == right.MilestoneID && left.Title == right.Title &&
		left.Description == right.Description && left.Status == right.Status &&
		left.AssigneeID == right.AssigneeID && trackingTimesEqual(left.StartAt, right.StartAt) &&
		trackingTimesEqual(left.DueAt, right.DueAt) && stringSlicesEqual(left.RelatedObjectIDs, right.RelatedObjectIDs)
}

func trackingTimesEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (store PostgresStore) getSettingsTx(ctx context.Context, tx transaction.Tx, projectID string) (Settings, error) {
	var item Settings
	err := tx.QueryRowContext(ctx, settingsSelect+` WHERE project_id=$1`, projectID).Scan(settingsScanTargets(&item)...)
	if errors.Is(err, sql.ErrNoRows) {
		item = defaultSettings(projectID)
		err = nil
	}
	item.EvaluatorMode = store.evaluatorMode()
	return item, err
}

func (store PostgresStore) resolveTrackingActor(ctx context.Context, tx transaction.Tx, projectID, actorID, agentInstanceID string) (string, error) {
	if actorID != "" {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM auth_users WHERE user_id=$1)`, actorID).Scan(&exists); err != nil {
			return "", err
		}
		if exists {
			return actorID, nil
		}
	}
	var resolved string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT created_by::text FROM agent_instances WHERE agent_instance_id=NULLIF($2,'')::uuid),(SELECT created_by::text FROM projects WHERE project_id=$1))`, projectID, agentInstanceID).Scan(&resolved)
	return resolved, err
}

func (store PostgresStore) requestSourceEventIDs(ctx context.Context, tx transaction.Tx, requestID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT source_event_id::text FROM progress_evaluation_triggers WHERE request_id=$1 AND source_event_id IS NOT NULL ORDER BY occurred_at,trigger_id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (store PostgresStore) loadEvaluationDetails(ctx context.Context, evaluation *Evaluation) error {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT trigger_id,trigger_type,COALESCE(source_event_id::text,''),source_event_type,
		       source_resource_id,source_version,payload,occurred_at
		FROM progress_evaluation_triggers
		WHERE request_id=$1 OR request_id IN (SELECT request_id FROM progress_evaluation_requests WHERE merged_into_evaluation_id=$2)
		ORDER BY occurred_at,trigger_id
	`, evaluation.RequestID, evaluation.ID)
	if err != nil {
		return err
	}
	evaluation.Triggers = []EvaluationTrigger{}
	for rows.Next() {
		var trigger EvaluationTrigger
		var payload []byte
		if err := rows.Scan(&trigger.ID, &trigger.TriggerType, &trigger.SourceEventID, &trigger.SourceEventType, &trigger.SourceResourceID, &trigger.SourceVersion, &payload, &trigger.OccurredAt); err != nil {
			_ = rows.Close()
			return err
		}
		if err := json.Unmarshal(payload, &trigger.Payload); err != nil {
			_ = rows.Close()
			return err
		}
		evaluation.Triggers = append(evaluation.Triggers, trigger)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	riskRows, err := store.DB.QueryContext(ctx, `SELECT risk_id,evaluation_id,project_id,risk_key,title,severity,status,detail,created_at FROM progress_evaluation_risks WHERE evaluation_id=$1 ORDER BY created_at,risk_id`, evaluation.ID)
	if err != nil {
		return err
	}
	defer riskRows.Close()
	evaluation.Risks = []Risk{}
	for riskRows.Next() {
		var risk Risk
		if err := riskRows.Scan(&risk.ID, &risk.EvaluationID, &risk.ProjectID, &risk.Key, &risk.Title, &risk.Severity, &risk.Status, &risk.Detail, &risk.CreatedAt); err != nil {
			return err
		}
		evaluation.Risks = append(evaluation.Risks, risk)
	}
	return riskRows.Err()
}

func (store PostgresStore) scanEvaluation(scan scanFunc) (Evaluation, error) {
	var item Evaluation
	var input, output, changes, completed, inProgress, blockers, questions, sources []byte
	err := scan(&item.ID, &item.RequestID, &item.ProjectID, &item.JobID, &item.Status,
		&item.InputVersion, &input, &output, &item.DetectedStage, &item.Summary,
		&changes, &completed, &inProgress, &blockers, &questions, &sources,
		&item.TriggerKind, &item.AgentInstanceID, &item.AgentSessionID, &item.AgentRunID,
		&item.EvaluatorMode, &item.Attempts, &item.ErrorCode, &item.ErrorMessage,
		&item.RequestedBy, &item.CreatedAt, &item.StartedAt, &item.CompletedAt, &item.UpdatedAt)
	if err != nil {
		return Evaluation{}, err
	}
	if err := json.Unmarshal(input, &item.InputSnapshot); err != nil {
		return Evaluation{}, err
	}
	if len(output) > 0 && string(output) != "null" {
		var parsed EvaluationOutput
		if err := json.Unmarshal(output, &parsed); err != nil {
			return Evaluation{}, err
		}
		item.Output = &parsed
	}
	if err := decodeStringSlices([][]byte{changes, completed, inProgress, blockers, questions, sources}, []*[]string{&item.ChangesSinceLast, &item.CompletedItems, &item.InProgressItems, &item.Blockers, &item.PendingQuestions, &item.SourceEventIDs}); err != nil {
		return Evaluation{}, err
	}
	return item, nil
}

func (store PostgresStore) trackingEvent(ctx context.Context, tx transaction.Tx, actorID, actorKind, eventType, projectID, resourceID string, payload map[string]interface{}) error {
	payload = nonNilMap(payload)
	payload["resource_id"] = resourceID
	if _, ok := payload["resource_type"]; !ok {
		payload["resource_type"] = "progress_evaluation"
	}
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"actor_id": actorID, "actor_kind": normalizedActorKind(actorKind)}, EventType: eventType, Payload: payload, Producer: "progress", ProjectID: projectID})
	return err
}

func (store PostgresStore) trackingAudit(ctx context.Context, tx transaction.Tx, actorID, actorKind, projectID, action, resourceType, resourceID, outcome, errorCode string, metadata map[string]interface{}) error {
	if store.Audit.Store == nil {
		return nil
	}
	return store.Audit.RecordInTransaction(ctx, tx, audit.Event{Action: action, ActorID: actorID, ActorKind: normalizedActorKind(actorKind), Category: "progress", ErrorCode: errorCode, Metadata: nonNilMap(metadata), Outcome: outcome, ProjectID: projectID, RequestID: requestctx.RequestID(ctx), ResourceID: resourceID, ResourceType: resourceType, Source: "core"})
}

func defaultSettings(projectID string) Settings {
	return Settings{ProjectID: projectID, AutoTaskChanges: true, EventTriggersEnabled: true, CronSchedule: "0 */6 * * *", DebounceSeconds: 60, MinIntervalSeconds: 300, CronSyncStatus: "pending"}
}

func (store PostgresStore) evaluatorMode() string {
	if store.EvaluatorMode == "mock" {
		return "mock"
	}
	return "core_agent"
}

func emptyTrackerState(projectID string) TrackerState {
	return TrackerState{ProjectID: projectID, ChangesSinceLast: []string{}, CompletedItems: []string{}, InProgressItems: []string{}, Blockers: []string{}, PendingQuestions: []string{}}
}

func normalizedActorKind(value string) string {
	switch value {
	case "session", "agent", "system":
		return value
	default:
		return "system"
	}
}

func nonNilMap(value map[string]interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return value
}

func decodeStringSlices(raw [][]byte, targets []*[]string) error {
	for index := range raw {
		if err := json.Unmarshal(raw[index], targets[index]); err != nil {
			return err
		}
		if *targets[index] == nil {
			*targets[index] = []string{}
		}
	}
	return nil
}

func safeTrackingMessage(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func scanStageOverride(scan scanFunc) (StageOverride, error) {
	var item StageOverride
	err := scan(&item.ID, &item.ProjectID, &item.Stage, &item.Summary, &item.Note, &item.Active, &item.CreatedBy, &item.CreatedAt, &item.ClearedBy, &item.ClearedAt)
	return item, err
}

func settingsScanTargets(item *Settings) []interface{} {
	return []interface{}{&item.ProjectID, &item.AutoTaskChanges, &item.AutoTrackingEnabled,
		&item.EventTriggersEnabled, &item.CronEnabled, &item.CronSchedule,
		&item.DebounceSeconds, &item.MinIntervalSeconds, &item.AgentInstanceID,
		&item.CronRemoteJobID, &item.CronSyncStatus, &item.CronErrorCode,
		&item.CronSyncedAt, &item.UpdatedBy, &item.UpdatedAt}
}

const settingsSelect = `SELECT project_id,auto_task_changes,auto_tracking_enabled,event_triggers_enabled,cron_enabled,cron_schedule,debounce_seconds,min_interval_seconds,COALESCE(agent_instance_id::text,''),cron_remote_job_id,cron_sync_status,cron_error_code,cron_synced_at,updated_by,updated_at FROM progress_settings`
const stageOverrideSelect = `SELECT override_id,project_id,stage,summary,note,active,created_by,created_at,COALESCE(cleared_by::text,''),cleared_at FROM progress_stage_overrides`
const evaluationSelect = `SELECT evaluation.evaluation_id,evaluation.request_id,evaluation.project_id,COALESCE(evaluation.job_id::text,''),evaluation.status,evaluation.input_version,evaluation.input_snapshot,evaluation.output_snapshot,evaluation.detected_stage,evaluation.summary,evaluation.changes_since_last,evaluation.completed_items,evaluation.in_progress_items,evaluation.blockers,evaluation.pending_questions,evaluation.source_event_ids,evaluation.trigger_kind,COALESCE(evaluation.agent_instance_id::text,''),COALESCE(evaluation.agent_session_id::text,''),COALESCE(evaluation.agent_run_id::text,''),evaluation.evaluator_mode,evaluation.attempts,evaluation.error_code,evaluation.error_message,evaluation.requested_by,evaluation.created_at,evaluation.started_at,evaluation.completed_at,evaluation.updated_at FROM progress_evaluations AS evaluation `

func trackingPostgresError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrConflict
	}
	return fmt.Errorf("progress tracking persistence: %w", err)
}
