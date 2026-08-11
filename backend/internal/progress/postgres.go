package progress

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgconn"
	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// ObjectReferenceValidator checks visible Data Hub objects through the
// Data Hub-owned persistence boundary using the caller's transaction.
type ObjectReferenceValidator interface {
	ValidateProgressReferences(context.Context, transaction.Tx, string, []string) (bool, error)
}

type PostgresStore struct {
	Audit              audit.Recorder
	Clock              clock.Clock
	DB                 *sql.DB
	EvaluatorMode      string
	Generator          identity.Generator
	Jobs               jobs.TransactionalWriter
	Outbox             outbox.Writer
	References         ObjectReferenceValidator
	ReminderLease      time.Duration
	ReminderRetryDelay time.Duration
	Transaction        transaction.Manager
}

func (store PostgresStore) ListMilestones(ctx context.Context, projectID string) ([]Milestone, error) {
	rows, err := store.DB.QueryContext(ctx, milestoneSelect+` WHERE project_id=$1 ORDER BY COALESCE(target_at, 'infinity'::timestamptz), milestone_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Milestone{}
	for rows.Next() {
		item, scanErr := scanMilestone(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) GetMilestone(ctx context.Context, projectID, id string) (Milestone, error) {
	item, err := scanMilestone(store.DB.QueryRowContext(ctx, milestoneSelect+` WHERE project_id=$1 AND milestone_id=$2`, projectID, id).Scan)
	return item, mapNotFound(err)
}

func (store PostgresStore) CreateMilestone(ctx context.Context, projectID, actorID string, input CreateMilestoneInput) (Milestone, error) {
	id, err := store.Generator.New()
	if err != nil {
		return Milestone{}, err
	}
	now := store.now()
	item := Milestone{ID: id, ProjectID: projectID, Title: input.Title, Description: input.Description, Status: StatusPlanned, Critical: input.Critical, StartAt: input.StartAt, TargetAt: input.TargetAt, TargetHasTime: input.TargetHasTime, Source: "human", CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_milestones(milestone_id,project_id,title,description,status,critical,start_at,target_at,target_has_time,source,created_by,updated_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11,$12,$12)`, item.ID, item.ProjectID, item.Title, item.Description, item.Status, item.Critical, item.StartAt, item.TargetAt, item.TargetHasTime, item.Source, actorID, now); err != nil {
			return err
		}
		return store.progressEvent(ctx, tx, "progress.milestone.created", projectID, actorID, item.ID, "milestone", item.Title, item.Status, map[string]interface{}{"critical": item.Critical, "source": item.Source})
	})
	return item, err
}

func (store PostgresStore) UpdateMilestone(ctx context.Context, projectID, id, actorID string, input UpdateMilestoneInput) (Milestone, error) {
	var item Milestone
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		current, err := scanMilestone(tx.QueryRowContext(ctx, milestoneSelect+` WHERE project_id=$1 AND milestone_id=$2 FOR UPDATE`, projectID, id).Scan)
		if err != nil {
			return mapNotFound(err)
		}
		if input.Title != nil {
			current.Title = strings.TrimSpace(*input.Title)
		}
		if input.Description != nil {
			current.Description = *input.Description
		}
		if input.Status != nil {
			current.Status = *input.Status
		}
		if input.Critical != nil {
			current.Critical = *input.Critical
		}
		if input.StartAt != nil {
			current.StartAt = *input.StartAt
		}
		if input.TargetAt != nil {
			current.TargetAt = *input.TargetAt
		}
		if input.TargetHasTime != nil {
			current.TargetHasTime = *input.TargetHasTime
		}
		if current.TargetAt == nil {
			current.TargetHasTime = false
		}
		if !validMilestoneStatus(current.Status) || current.Title == "" || current.TargetAt != nil && current.StartAt != nil && current.TargetAt.Before(*current.StartAt) {
			return ErrInvalid
		}
		now := store.now()
		if current.Status == StatusCompleted {
			current.CompletedAt = &now
		} else {
			current.CompletedAt = nil
		}
		current.UpdatedBy, current.UpdatedAt, current.Source = actorID, now, "human"
		if _, err := tx.ExecContext(ctx, `UPDATE progress_milestones SET title=$3,description=$4,status=$5,critical=$6,start_at=$7,target_at=$8,target_has_time=$9,completed_at=$10,source=$11,updated_by=$12,updated_at=$13 WHERE project_id=$1 AND milestone_id=$2`, projectID, id, current.Title, current.Description, current.Status, current.Critical, current.StartAt, current.TargetAt, current.TargetHasTime, current.CompletedAt, current.Source, actorID, now); err != nil {
			return err
		}
		if err := store.progressEvent(ctx, tx, "progress.milestone.updated", projectID, actorID, id, "milestone", current.Title, current.Status, map[string]interface{}{"critical": current.Critical, "source": current.Source}); err != nil {
			return err
		}
		item = current
		return nil
	})
	return item, err
}

func (store PostgresStore) ListTasks(ctx context.Context, projectID string) ([]Task, error) {
	rows, err := store.DB.QueryContext(ctx, taskSelect+` WHERE project_id=$1 ORDER BY COALESCE(due_at, 'infinity'::timestamptz), task_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Task{}
	for rows.Next() {
		item, scanErr := scanTask(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) GetTask(ctx context.Context, projectID, id string) (Task, error) {
	item, err := scanTask(store.DB.QueryRowContext(ctx, taskSelect+` WHERE project_id=$1 AND task_id=$2`, projectID, id).Scan)
	return item, mapNotFound(err)
}

func (store PostgresStore) DeleteMilestone(ctx context.Context, projectID, id, actorID string) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var title string
		if err := tx.QueryRowContext(ctx, `DELETE FROM progress_milestones WHERE project_id=$1 AND milestone_id=$2 RETURNING title`, projectID, id).Scan(&title); err != nil {
			return mapNotFound(err)
		}
		return store.progressEvent(ctx, tx, "progress.milestone.deleted", projectID, actorID, id, "milestone", title, "deleted", map[string]interface{}{"source": "human"})
	})
}

func (store PostgresStore) CreateTask(ctx context.Context, projectID, actorID string, input CreateTaskInput, source string) (Task, error) {
	var err error
	input.MilestoneID, err = normalizeReferenceID(input.MilestoneID)
	if err != nil {
		return Task{}, err
	}
	input.AssigneeID, err = normalizeReferenceID(input.AssigneeID)
	if err != nil {
		return Task{}, err
	}
	input.RelatedObjectIDs, err = normalizeRelatedObjectIDs(input.RelatedObjectIDs)
	if err != nil {
		return Task{}, err
	}
	id, err := store.Generator.New()
	if err != nil {
		return Task{}, err
	}
	now := store.now()
	if input.Status == "" {
		input.Status = TaskTodo
	}
	if !validTaskStatus(input.Status) || input.DueAt != nil && input.StartAt != nil && input.DueAt.Before(*input.StartAt) {
		return Task{}, ErrInvalid
	}
	workState := input.Status
	if workState == TaskDone {
		workState = TaskTodo
	}
	item := Task{ID: id, ProjectID: projectID, MilestoneID: input.MilestoneID, Title: input.Title, Description: input.Description, Status: input.Status, WorkState: workState, AssigneeID: input.AssigneeID, StartAt: input.StartAt, DueAt: input.DueAt, Source: source, SourceRunID: input.SourceRunID, RelatedObjectIDs: nonNilStrings(input.RelatedObjectIDs), CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	if item.Status == TaskDone {
		item.CompletedAt = &now
	}
	metadata, _ := json.Marshal(item.RelatedObjectIDs)
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := store.validateTaskReferences(ctx, tx, projectID, item.MilestoneID, item.AssigneeID, item.RelatedObjectIDs, true, true, true); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_tasks(task_id,project_id,milestone_id,title,description,status,work_state,assignee_id,start_at,due_at,completed_at,source,source_run_id,related_object_ids,created_by,updated_by,created_at,updated_at) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,NULLIF($8,'')::uuid,$9,$10,$11,$12,$13,$14,$15,$15,$16,$16)`, item.ID, projectID, item.MilestoneID, item.Title, item.Description, item.Status, item.WorkState, item.AssigneeID, item.StartAt, item.DueAt, item.CompletedAt, item.Source, item.SourceRunID, metadata, actorID, now); err != nil {
			return mapPostgresMutationError(err)
		}
		return store.progressEvent(ctx, tx, "progress.task.created", projectID, actorID, item.ID, "task", item.Title, item.Status, map[string]interface{}{"source": item.Source, "source_run_id": item.SourceRunID})
	})
	return item, mapPostgresMutationError(err)
}

func (store PostgresStore) UpdateTask(ctx context.Context, projectID, id, actorID string, input UpdateTaskInput, source string) (Task, error) {
	var err error
	if input.MilestoneID != nil {
		value, normalizeErr := normalizeReferenceID(*input.MilestoneID)
		if normalizeErr != nil {
			return Task{}, normalizeErr
		}
		input.MilestoneID = &value
	}
	if input.AssigneeID != nil {
		value, normalizeErr := normalizeReferenceID(*input.AssigneeID)
		if normalizeErr != nil {
			return Task{}, normalizeErr
		}
		input.AssigneeID = &value
	}
	if input.RelatedObjectIDs != nil {
		values, normalizeErr := normalizeRelatedObjectIDs(*input.RelatedObjectIDs)
		if normalizeErr != nil {
			return Task{}, normalizeErr
		}
		input.RelatedObjectIDs = &values
	}
	var item Task
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		current, err := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE project_id=$1 AND task_id=$2 FOR UPDATE`, projectID, id).Scan)
		if err != nil {
			return mapNotFound(err)
		}
		if source == "human" {
			current.ManualOverrideFields = mergeOverrideFields(current.ManualOverrideFields, taskInputFields(input))
		} else {
			input = filterTaskInput(input, current.ManualOverrideFields)
		}
		if input.MilestoneID != nil {
			current.MilestoneID = *input.MilestoneID
		}
		if input.Title != nil {
			current.Title = strings.TrimSpace(*input.Title)
		}
		if input.Description != nil {
			current.Description = *input.Description
		}
		if input.Status != nil {
			current.Status = *input.Status
			if current.Status != TaskDone {
				current.WorkState = current.Status
			}
		}
		if input.AssigneeID != nil {
			current.AssigneeID = *input.AssigneeID
		}
		if input.StartAt != nil {
			current.StartAt = *input.StartAt
		}
		if input.DueAt != nil {
			current.DueAt = *input.DueAt
		}
		if input.RelatedObjectIDs != nil {
			current.RelatedObjectIDs = *input.RelatedObjectIDs
		}
		if input.SourceRunID != nil {
			current.SourceRunID = *input.SourceRunID
		}
		if current.Title == "" || !validTaskStatus(current.Status) || current.DueAt != nil && current.StartAt != nil && current.DueAt.Before(*current.StartAt) {
			return ErrInvalid
		}
		if err := store.validateTaskReferences(ctx, tx, projectID, current.MilestoneID, current.AssigneeID, current.RelatedObjectIDs, input.MilestoneID != nil, input.AssigneeID != nil, input.RelatedObjectIDs != nil); err != nil {
			return err
		}
		now := store.now()
		if current.Status == TaskDone {
			current.CompletedAt = &now
		} else {
			current.CompletedAt = nil
		}
		current.Source, current.UpdatedBy, current.UpdatedAt = source, actorID, now
		metadata, _ := json.Marshal(nonNilStrings(current.RelatedObjectIDs))
		overrides, _ := json.Marshal(nonNilStrings(current.ManualOverrideFields))
		if _, err := tx.ExecContext(ctx, `UPDATE progress_tasks SET milestone_id=NULLIF($3,'')::uuid,title=$4,description=$5,status=$6,work_state=$7,assignee_id=NULLIF($8,'')::uuid,start_at=$9,due_at=$10,completed_at=$11,source=$12,source_run_id=$13,source_evaluation_id=NULL,related_object_ids=$14,manual_override_fields=$15,updated_by=$16,updated_at=$17 WHERE project_id=$1 AND task_id=$2`, projectID, id, current.MilestoneID, current.Title, current.Description, current.Status, current.WorkState, current.AssigneeID, current.StartAt, current.DueAt, current.CompletedAt, current.Source, current.SourceRunID, metadata, overrides, actorID, now); err != nil {
			return mapPostgresMutationError(err)
		}
		if err := store.progressEvent(ctx, tx, "progress.task.updated", projectID, actorID, id, "task", current.Title, current.Status, map[string]interface{}{"source": current.Source, "source_run_id": current.SourceRunID}); err != nil {
			return err
		}
		item = current
		return nil
	})
	return item, mapPostgresMutationError(err)
}

func (store PostgresStore) DeleteTask(ctx context.Context, projectID, id, actorID string) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var title string
		if err := tx.QueryRowContext(ctx, `DELETE FROM progress_tasks WHERE project_id=$1 AND task_id=$2 RETURNING title`, projectID, id).Scan(&title); err != nil {
			return mapNotFound(err)
		}
		return store.progressEvent(ctx, tx, "progress.task.deleted", projectID, actorID, id, "task", title, "deleted", map[string]interface{}{"source": "human"})
	})
}

func (store PostgresStore) ListDependencies(ctx context.Context, projectID string) ([]Dependency, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT dependency_id,project_id,task_id,depends_on_task_id,kind,created_by,created_at FROM progress_dependencies WHERE project_id=$1 ORDER BY created_at,dependency_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Dependency{}
	for rows.Next() {
		var item Dependency
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.TaskID, &item.DependsOnTaskID, &item.Kind, &item.CreatedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) CreateDependency(ctx context.Context, projectID, actorID string, input CreateDependencyInput) (Dependency, error) {
	if input.TaskID == "" || input.DependsOnTaskID == "" || input.TaskID == input.DependsOnTaskID {
		return Dependency{}, ErrInvalid
	}
	var err error
	input.TaskID, err = normalizeReferenceID(input.TaskID)
	if err != nil {
		return Dependency{}, err
	}
	input.DependsOnTaskID, err = normalizeReferenceID(input.DependsOnTaskID)
	if err != nil {
		return Dependency{}, err
	}
	id, err := store.Generator.New()
	if err != nil {
		return Dependency{}, err
	}
	now := store.now()
	item := Dependency{ID: id, ProjectID: projectID, TaskID: input.TaskID, DependsOnTaskID: input.DependsOnTaskID, Kind: input.Kind, CreatedBy: actorID, CreatedAt: now}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := validateTaskReference(ctx, tx, projectID, input.TaskID); err != nil {
			return err
		}
		if err := validateTaskReference(ctx, tx, projectID, input.DependsOnTaskID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_dependencies(dependency_id,project_id,task_id,depends_on_task_id,kind,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, projectID, item.TaskID, item.DependsOnTaskID, item.Kind, actorID, now); err != nil {
			return mapPostgresMutationError(err)
		}
		return store.progressEvent(ctx, tx, "progress.dependency.created", projectID, actorID, id, "dependency", item.TaskID, item.Kind, map[string]interface{}{"task_id": item.TaskID, "depends_on_task_id": item.DependsOnTaskID})
	})
	return item, mapPostgresMutationError(err)
}

func (store PostgresStore) DeleteDependency(ctx context.Context, projectID, id, actorID string) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var taskID string
		if err := tx.QueryRowContext(ctx, `DELETE FROM progress_dependencies WHERE project_id=$1 AND dependency_id=$2 RETURNING task_id`, projectID, id).Scan(&taskID); err != nil {
			return mapNotFound(err)
		}
		return store.progressEvent(ctx, tx, "progress.dependency.deleted", projectID, actorID, id, "dependency", taskID, "deleted", map[string]interface{}{"source": "human"})
	})
}

func (store PostgresStore) ListReminders(ctx context.Context, projectID string) ([]Reminder, error) {
	rows, err := store.DB.QueryContext(ctx, reminderSelect+` WHERE project_id=$1 ORDER BY remind_at,reminder_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Reminder{}
	for rows.Next() {
		item, scanErr := scanReminder(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) CreateReminder(ctx context.Context, projectID, actorID string, input CreateReminderInput) (Reminder, error) {
	var err error
	input.TaskID, err = normalizeReferenceID(input.TaskID)
	if err != nil {
		return Reminder{}, err
	}
	input.MilestoneID, err = normalizeReferenceID(input.MilestoneID)
	if err != nil {
		return Reminder{}, err
	}
	id, err := store.Generator.New()
	if err != nil {
		return Reminder{}, err
	}
	now := store.now()
	item := Reminder{ID: id, ProjectID: projectID, TaskID: input.TaskID, MilestoneID: input.MilestoneID, RemindAt: input.RemindAt, Status: ReminderPending, Note: input.Note, Source: "human", CreatedBy: actorID, CreatedAt: now, UpdatedAt: now, AvailableAt: input.RemindAt, MaxAttempts: 5}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if item.TaskID != "" {
			if err := validateTaskReference(ctx, tx, projectID, item.TaskID); err != nil {
				return err
			}
		} else if err := validateMilestoneReference(ctx, tx, projectID, item.MilestoneID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_reminders(reminder_id,project_id,task_id,milestone_id,remind_at,status,note,source,created_by,available_at,created_at,updated_at) VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$5,$10,$10)`, item.ID, projectID, item.TaskID, item.MilestoneID, item.RemindAt, item.Status, item.Note, item.Source, actorID, now); err != nil {
			return mapPostgresMutationError(err)
		}
		title := strings.TrimSpace(input.Note)
		if title == "" {
			title = "Progress reminder"
		}
		return store.progressEvent(ctx, tx, "progress.reminder.created", projectID, actorID, id, "reminder", title, item.Status, map[string]interface{}{"remind_at": item.RemindAt.Format(time.RFC3339)})
	})
	return item, mapPostgresMutationError(err)
}

func (store PostgresStore) TriggerReminder(ctx context.Context, projectID, id, actorID string) (Reminder, error) {
	ownerID, err := store.Generator.New()
	if err != nil {
		return Reminder{}, err
	}
	item, err := store.claimReminder(ctx, projectID, id, "manual-progress-reminder-"+ownerID, store.reminderLease())
	if err != nil {
		return Reminder{}, err
	}
	completed, err := store.CompleteReminder(ctx, item.ID, item.LockedBy, actorID)
	if err == nil {
		return completed, nil
	}
	_, releaseErr := store.FailReminder(ctx, item.ID, item.LockedBy, "event_write_failed", "Progress reminder event could not be recorded", store.reminderRetryDelay())
	return Reminder{}, errors.Join(err, releaseErr)
}

// ClaimDueReminders leases one deterministic batch of globally due reminders.
func (store PostgresStore) ClaimDueReminders(ctx context.Context, owner string, lease time.Duration, limit int) ([]Reminder, error) {
	return store.claimReminders(ctx, owner, lease, limit, "", "", true)
}

func (store PostgresStore) claimReminder(ctx context.Context, projectID, id, owner string, lease time.Duration) (Reminder, error) {
	items, err := store.claimReminders(ctx, owner, lease, 1, projectID, id, false)
	if err != nil {
		return Reminder{}, err
	}
	if len(items) == 1 {
		return items[0], nil
	}
	var status string
	err = store.DB.QueryRowContext(ctx, `SELECT status FROM progress_reminders WHERE project_id=$1 AND reminder_id=$2`, projectID, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return Reminder{}, ErrNotFound
	}
	if err != nil {
		return Reminder{}, err
	}
	return Reminder{}, ErrConflict
}

func (store PostgresStore) claimReminders(ctx context.Context, owner string, lease time.Duration, limit int, projectID, reminderID string, dueOnly bool) ([]Reminder, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || lease <= 0 || limit < 1 {
		return nil, ErrInvalid
	}
	now := store.now()
	items := []Reminder{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := recoverExpiredReminders(ctx, tx, now, reminderID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `
			WITH candidates AS (
				SELECT reminder_id
				FROM progress_reminders
				WHERE status='pending'
				  AND (NOT $5::boolean OR (remind_at <= $1 AND available_at <= $1))
				  AND ($6 = '' OR project_id = NULLIF($6, '')::uuid)
				  AND ($7 = '' OR reminder_id = NULLIF($7, '')::uuid)
				  AND attempts < max_attempts
				ORDER BY remind_at, reminder_id
				FOR UPDATE SKIP LOCKED
				LIMIT $4
			)
			UPDATE progress_reminders AS reminder
			SET status='processing', attempts=reminder.attempts+1,
			    locked_by=$2, lease_expires_at=$3,
			    last_error_code='', last_error_message='', updated_at=$1
			FROM candidates
			WHERE reminder.reminder_id=candidates.reminder_id
			RETURNING `+claimedReminderColumns,
			now, owner, now.Add(lease), limit, dueOnly, projectID, reminderID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, scanErr := scanReminder(rows.Scan)
			if scanErr != nil {
				return scanErr
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// CompleteReminder atomically commits the triggered state and stable due event.
func (store PostgresStore) CompleteReminder(ctx context.Context, id, owner, actorID string) (Reminder, error) {
	var item Reminder
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		now := store.now()
		updated, err := scanReminder(tx.QueryRowContext(ctx, `
			UPDATE progress_reminders
			SET status='triggered', triggered_at=$3, locked_by=NULL,
			    lease_expires_at=NULL, last_error_code='', last_error_message='',
			    updated_at=$3
			WHERE reminder_id=$1 AND status='processing' AND locked_by=$2
			  AND lease_expires_at > $3
			RETURNING `+reminderColumns, id, owner, now).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReminderLeaseLost
		}
		if err != nil {
			return err
		}
		item = updated
		metadata := map[string]interface{}{"reminder_id": item.ID}
		if item.TaskID != "" {
			metadata["task_id"] = item.TaskID
		}
		if item.MilestoneID != "" {
			metadata["milestone_id"] = item.MilestoneID
		}
		title := strings.TrimSpace(item.Note)
		if title == "" {
			title = "Progress reminder"
		}
		return store.progressEventWithID(ctx, tx, item.ID, "progress.reminder.due", item.ProjectID, actorID, item.ID, "reminder", title, item.Status, metadata)
	})
	return item, err
}

// FailReminder releases an owned lease for retry or terminal failure.
func (store PostgresStore) FailReminder(ctx context.Context, id, owner, code, message string, retryDelay time.Duration) (Reminder, error) {
	now := store.now()
	if retryDelay < 0 {
		retryDelay = 0
	}
	code = safeReminderError(code, 100)
	message = safeReminderError(message, 500)
	item, err := scanReminder(store.DB.QueryRowContext(ctx, `
		UPDATE progress_reminders
		SET status=CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'failed' END,
		    available_at=CASE WHEN attempts < max_attempts THEN $3 ELSE available_at END,
		    locked_by=NULL, lease_expires_at=NULL,
		    last_error_code=$4, last_error_message=$5, updated_at=$2
		WHERE reminder_id=$1 AND status='processing' AND locked_by=$6
		RETURNING `+reminderColumns,
		id, now, now.Add(retryDelay), code, message, owner).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Reminder{}, ErrReminderLeaseLost
	}
	return item, err
}

func recoverExpiredReminders(ctx context.Context, tx transaction.Tx, now time.Time, reminderID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE progress_reminders
		SET status=CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'failed' END,
		    available_at=CASE WHEN attempts < max_attempts THEN $1 ELSE available_at END,
		    locked_by=NULL, lease_expires_at=NULL,
		    last_error_code='lease_expired',
		    last_error_message='Progress reminder processing lease expired',
		    updated_at=$1
		WHERE status='processing' AND lease_expires_at <= $1
		  AND ($2 = '' OR reminder_id = NULLIF($2, '')::uuid)
	`, now, reminderID)
	return err
}

func (store PostgresStore) reminderLease() time.Duration {
	if store.ReminderLease <= 0 {
		return 30 * time.Second
	}
	return store.ReminderLease
}

func (store PostgresStore) reminderRetryDelay() time.Duration {
	if store.ReminderRetryDelay <= 0 {
		return 2 * time.Second
	}
	return store.ReminderRetryDelay
}

func safeReminderError(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}

func (store PostgresStore) ListProposals(ctx context.Context, projectID string) ([]Proposal, error) {
	rows, err := store.DB.QueryContext(ctx, proposalSelect+` WHERE project_id=$1 ORDER BY created_at DESC,proposal_id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Proposal{}
	for rows.Next() {
		item, scanErr := scanProposal(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) GetProposal(ctx context.Context, projectID, id string) (Proposal, error) {
	item, err := scanProposal(store.DB.QueryRowContext(ctx, proposalSelect+` WHERE project_id=$1 AND proposal_id=$2`, projectID, id).Scan)
	return item, mapNotFound(err)
}

func (store PostgresStore) CreateProposal(ctx context.Context, projectID, actorID string, input CreateProposalInput) (Proposal, error) {
	id, err := store.Generator.New()
	if err != nil {
		return Proposal{}, err
	}
	now := store.now()
	item := Proposal{ID: id, ProjectID: projectID, ProposalType: input.ProposalType, TargetID: input.TargetID, Title: input.Title, Rationale: input.Rationale, Changes: input.Changes, Source: "agent", SourceRunID: input.SourceRunID, ProposedBy: actorID, Status: "pending", ReviewNote: "", CreatedAt: now, UpdatedAt: now}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		targetID, changes, validateErr := store.validateProposalReferences(ctx, tx, projectID, item.ProposalType, item.TargetID, item.Changes)
		if validateErr != nil {
			return validateErr
		}
		item.TargetID, item.Changes = targetID, changes
		rawChanges, marshalErr := json.Marshal(item.Changes)
		if marshalErr != nil {
			return ErrInvalid
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_proposals(proposal_id,project_id,proposal_type,target_id,title,rationale,changes,source,source_run_id,proposed_by,status,review_note,created_at,updated_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)`, item.ID, projectID, item.ProposalType, item.TargetID, item.Title, item.Rationale, rawChanges, item.Source, item.SourceRunID, actorID, item.Status, item.ReviewNote, now); err != nil {
			return mapPostgresMutationError(err)
		}
		return store.progressEvent(ctx, tx, "progress.proposal.created", projectID, actorID, id, "progress_proposal", item.Title, item.Status, map[string]interface{}{"proposal_type": item.ProposalType, "source": item.Source, "source_run_id": item.SourceRunID})
	})
	return item, mapPostgresMutationError(err)
}

func (store PostgresStore) ReviewProposal(ctx context.Context, projectID, id, reviewerID string, input ReviewProposalInput) (Proposal, error) {
	var result Proposal
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var err error
		result, err = store.reviewProposalInTx(ctx, tx, projectID, id, reviewerID, input, store.now())
		return err
	})
	return result, mapPostgresMutationError(err)
}

func (store PostgresStore) ReviewProposals(ctx context.Context, projectID, reviewerID string, input BatchReviewProposalInput) ([]Proposal, error) {
	items := make([]Proposal, 0, len(input.ProposalIDs))
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		now := store.now()
		for _, id := range input.ProposalIDs {
			item, err := store.reviewProposalInTx(ctx, tx, projectID, id, reviewerID, ReviewProposalInput{Decision: input.Decision, Note: input.Note}, now)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return nil
	})
	return items, mapPostgresMutationError(err)
}

func (store PostgresStore) reviewProposalInTx(ctx context.Context, tx transaction.Tx, projectID, id, reviewerID string, input ReviewProposalInput, now time.Time) (Proposal, error) {
	proposal, err := scanProposal(tx.QueryRowContext(ctx, proposalSelect+` WHERE project_id=$1 AND proposal_id=$2 FOR UPDATE`, projectID, id).Scan)
	if err != nil {
		return Proposal{}, mapNotFound(err)
	}
	if proposal.Status != "pending" {
		return Proposal{}, ErrConflict
	}
	if input.Decision == "accepted" {
		targetID, changes, validateErr := store.validateProposalReferences(ctx, tx, projectID, proposal.ProposalType, proposal.TargetID, proposal.Changes)
		if validateErr != nil {
			return Proposal{}, validateErr
		}
		proposal.TargetID, proposal.Changes = targetID, changes
		if err := store.applyProposal(ctx, tx, proposal, reviewerID, now); err != nil {
			return Proposal{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE progress_proposals SET status=$3,reviewed_by=$4,reviewed_at=$5,review_note=$6,updated_at=$5 WHERE project_id=$1 AND proposal_id=$2`, projectID, id, input.Decision, reviewerID, now, input.Note); err != nil {
		return Proposal{}, err
	}
	proposal.Status, proposal.ReviewedBy, proposal.ReviewedAt, proposal.ReviewNote, proposal.UpdatedAt = input.Decision, reviewerID, &now, input.Note, now
	if err := store.progressEvent(ctx, tx, "progress.proposal.reviewed", projectID, reviewerID, id, "progress_proposal", proposal.Title, proposal.Status, map[string]interface{}{"proposal_type": proposal.ProposalType, "decision": input.Decision}); err != nil {
		return Proposal{}, err
	}
	return proposal, nil
}

func (store PostgresStore) applyProposal(ctx context.Context, tx transaction.Tx, proposal Proposal, actorID string, now time.Time) error {
	changes := proposal.Changes
	switch proposal.ProposalType {
	case "milestone.create":
		input := CreateMilestoneInput{Title: stringChange(changes, "title"), Description: stringChange(changes, "description"), Critical: boolChange(changes, "critical"), StartAt: timeChange(changes, "start_at"), TargetAt: timeChange(changes, "target_at"), TargetHasTime: boolChange(changes, "target_has_time")}
		if input.Title == "" {
			return ErrInvalid
		}
		id, err := store.Generator.New()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_milestones(milestone_id,project_id,title,description,status,critical,start_at,target_at,target_has_time,source,source_run_id,created_by,updated_by,created_at,updated_at) VALUES($1,$2,$3,$4,'planned',$5,$6,$7,$8,'proposal',$9,$10,$10,$11,$11)`, id, proposal.ProjectID, input.Title, input.Description, input.Critical, input.StartAt, input.TargetAt, input.TargetHasTime, proposal.SourceRunID, actorID, now); err != nil {
			return err
		}
		return store.progressEvent(ctx, tx, "progress.milestone.created", proposal.ProjectID, actorID, id, "milestone", input.Title, StatusPlanned, map[string]interface{}{"source": "proposal", "proposal_id": proposal.ID})
	case "milestone.update":
		if proposal.TargetID == "" {
			return ErrInvalid
		}
		current, err := scanMilestone(tx.QueryRowContext(ctx, milestoneSelect+` WHERE project_id=$1 AND milestone_id=$2 FOR UPDATE`, proposal.ProjectID, proposal.TargetID).Scan)
		if err != nil {
			return mapNotFound(err)
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
		if value, ok := changes["critical"].(bool); ok {
			current.Critical = value
		}
		if value := timeChange(changes, "start_at"); value != nil {
			current.StartAt = value
		}
		if value := timeChange(changes, "target_at"); value != nil {
			current.TargetAt = value
		}
		if value, ok := changes["target_has_time"].(bool); ok {
			current.TargetHasTime = value
		}
		if current.TargetAt == nil {
			current.TargetHasTime = false
		}
		if !validMilestoneStatus(current.Status) || current.Title == "" {
			return ErrInvalid
		}
		completed := interface{}(nil)
		if current.Status == StatusCompleted {
			completed = now
		}
		if _, err := tx.ExecContext(ctx, `UPDATE progress_milestones SET title=$3,description=$4,status=$5,critical=$6,start_at=$7,target_at=$8,target_has_time=$9,completed_at=$10,source='proposal',source_run_id=$11,updated_by=$12,updated_at=$13 WHERE project_id=$1 AND milestone_id=$2`, proposal.ProjectID, proposal.TargetID, current.Title, current.Description, current.Status, current.Critical, current.StartAt, current.TargetAt, current.TargetHasTime, completed, proposal.SourceRunID, actorID, now); err != nil {
			return err
		}
		return store.progressEvent(ctx, tx, "progress.milestone.updated", proposal.ProjectID, actorID, proposal.TargetID, "milestone", current.Title, current.Status, map[string]interface{}{"source": "proposal", "proposal_id": proposal.ID})
	case "milestone.complete":
		if proposal.TargetID == "" {
			return ErrInvalid
		}
		current, err := scanMilestone(tx.QueryRowContext(ctx, milestoneSelect+` WHERE project_id=$1 AND milestone_id=$2 FOR UPDATE`, proposal.ProjectID, proposal.TargetID).Scan)
		if err != nil {
			return mapNotFound(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE progress_milestones SET status='completed',completed_at=$3,source='proposal',source_run_id=$4,updated_by=$5,updated_at=$3 WHERE project_id=$1 AND milestone_id=$2`, proposal.ProjectID, proposal.TargetID, now, proposal.SourceRunID, actorID); err != nil {
			return err
		}
		return store.progressEvent(ctx, tx, "progress.milestone.updated", proposal.ProjectID, actorID, proposal.TargetID, "milestone", current.Title, StatusCompleted, map[string]interface{}{"source": "proposal", "proposal_id": proposal.ID, "completion_confirmed": true})
	case "task.create":
		relatedObjectIDs, _, err := relatedObjectIDsChange(changes)
		if err != nil {
			return err
		}
		input := CreateTaskInput{MilestoneID: stringChange(changes, "milestone_id"), Title: stringChange(changes, "title"), Description: stringChange(changes, "description"), Status: stringChange(changes, "status"), AssigneeID: stringChange(changes, "assignee_id"), StartAt: timeChange(changes, "start_at"), DueAt: timeChange(changes, "due_at"), RelatedObjectIDs: relatedObjectIDs, SourceRunID: proposal.SourceRunID}
		if input.Title == "" {
			return ErrInvalid
		}
		if input.Status == "" {
			input.Status = TaskTodo
		}
		if !validTaskStatus(input.Status) || input.DueAt != nil && input.StartAt != nil && input.DueAt.Before(*input.StartAt) {
			return ErrInvalid
		}
		id, err := store.Generator.New()
		if err != nil {
			return err
		}
		metadata, _ := json.Marshal(nonNilStrings(input.RelatedObjectIDs))
		var completed interface{}
		if input.Status == TaskDone {
			completed = now
		}
		workState := input.Status
		if workState == TaskDone {
			workState = TaskTodo
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_tasks(task_id,project_id,milestone_id,title,description,status,work_state,assignee_id,start_at,due_at,completed_at,source,source_run_id,related_object_ids,created_by,updated_by,created_at,updated_at) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,NULLIF($8,'')::uuid,$9,$10,$11,'proposal',$12,$13,$14,$14,$15,$15)`, id, proposal.ProjectID, input.MilestoneID, input.Title, input.Description, input.Status, workState, input.AssigneeID, input.StartAt, input.DueAt, completed, proposal.SourceRunID, metadata, actorID, now); err != nil {
			return err
		}
		return store.progressEvent(ctx, tx, "progress.task.created", proposal.ProjectID, actorID, id, "task", input.Title, input.Status, map[string]interface{}{"source": "proposal", "proposal_id": proposal.ID})
	case "task.update":
		if proposal.TargetID == "" {
			return ErrInvalid
		}
		current, err := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE project_id=$1 AND task_id=$2 FOR UPDATE`, proposal.ProjectID, proposal.TargetID).Scan)
		if err != nil {
			return mapNotFound(err)
		}
		if value, ok := changes["title"].(string); ok {
			current.Title = strings.TrimSpace(value)
		}
		if value, ok := changes["description"].(string); ok {
			current.Description = value
		}
		if value, ok := changes["status"].(string); ok {
			current.Status = value
			if current.Status != TaskDone {
				current.WorkState = current.Status
			}
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
		if current.Title == "" || !validTaskStatus(current.Status) || current.DueAt != nil && current.StartAt != nil && current.DueAt.Before(*current.StartAt) {
			return ErrInvalid
		}
		var completed interface{}
		if current.Status == TaskDone {
			completed = now
		}
		metadata, _ := json.Marshal(nonNilStrings(current.RelatedObjectIDs))
		if _, err := tx.ExecContext(ctx, `UPDATE progress_tasks SET milestone_id=NULLIF($3,'')::uuid,title=$4,description=$5,status=$6,work_state=$7,assignee_id=NULLIF($8,'')::uuid,start_at=$9,due_at=$10,completed_at=$11,source='proposal',source_run_id=$12,related_object_ids=$13,updated_by=$14,updated_at=$15 WHERE project_id=$1 AND task_id=$2`, proposal.ProjectID, proposal.TargetID, current.MilestoneID, current.Title, current.Description, current.Status, current.WorkState, current.AssigneeID, current.StartAt, current.DueAt, completed, proposal.SourceRunID, metadata, actorID, now); err != nil {
			return err
		}
		return store.progressEvent(ctx, tx, "progress.task.updated", proposal.ProjectID, actorID, proposal.TargetID, "task", current.Title, current.Status, map[string]interface{}{"source": "proposal", "proposal_id": proposal.ID})
	case "task.complete":
		if proposal.TargetID == "" {
			return ErrInvalid
		}
		current, err := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE project_id=$1 AND task_id=$2 FOR UPDATE`, proposal.ProjectID, proposal.TargetID).Scan)
		if err != nil {
			return mapNotFound(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE progress_tasks SET status='done',completed_at=$3,source='proposal',source_run_id=$4,source_evaluation_id=NULLIF($5,'')::uuid,updated_by=$6,updated_at=$3 WHERE project_id=$1 AND task_id=$2`, proposal.ProjectID, proposal.TargetID, now, proposal.SourceRunID, proposal.SourceEvaluationID, actorID); err != nil {
			return err
		}
		return store.progressEvent(ctx, tx, "progress.task.updated", proposal.ProjectID, actorID, proposal.TargetID, "task", current.Title, TaskDone, map[string]interface{}{"source": "proposal", "proposal_id": proposal.ID, "completion_confirmed": true})
	default:
		return ErrInvalid
	}
}

func (store PostgresStore) GetSettings(ctx context.Context, projectID string) (Settings, error) {
	var item Settings
	err := store.DB.QueryRowContext(ctx, settingsSelect+` WHERE project_id=$1`, projectID).Scan(settingsScanTargets(&item)...)
	if errors.Is(err, sql.ErrNoRows) {
		item = defaultSettings(projectID)
		err = nil
	}
	item.EvaluatorMode = store.evaluatorMode()
	return item, err
}

func (store PostgresStore) UpdateSettings(ctx context.Context, projectID, actorID string, autoTaskChanges bool) (Settings, error) {
	now := store.now()
	_, err := store.DB.ExecContext(ctx, `INSERT INTO progress_settings(project_id,auto_task_changes,updated_by,updated_at) VALUES($1,$2,$3,$4) ON CONFLICT(project_id) DO UPDATE SET auto_task_changes=EXCLUDED.auto_task_changes,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at`, projectID, autoTaskChanges, actorID, now)
	if err != nil {
		return Settings{}, err
	}
	return store.GetSettings(ctx, projectID)
}

func (store PostgresStore) progressEvent(ctx context.Context, tx transaction.Tx, eventType, projectID, actorID, resourceID, resourceType, title, status string, metadata map[string]interface{}) error {
	return store.progressEventWithID(ctx, tx, "", eventType, projectID, actorID, resourceID, resourceType, title, status, metadata)
}

func (store PostgresStore) progressEventWithID(ctx context.Context, tx transaction.Tx, eventID, eventType, projectID, actorID, resourceID, resourceType, title, status string, metadata map[string]interface{}) error {
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	metadata["resource_id"], metadata["resource_type"] = resourceID, resourceType
	metadata["title"], metadata["status"] = title, status
	returnEvent := outbox.Event{Actor: map[string]string{"user_id": actorID}, EventID: eventID, EventType: eventType, Payload: metadata, Producer: "progress", ProjectID: projectID}
	_, err := store.Outbox.Write(ctx, tx, returnEvent)
	return err
}

func (store PostgresStore) now() time.Time {
	if store.Clock == nil {
		return time.Now().UTC()
	}
	return store.Clock.Now().UTC()
}

const milestoneSelect = `SELECT milestone_id,project_id,title,description,status,critical,start_at,target_at,target_has_time,completed_at,source,source_run_id,created_by,updated_by,created_at,updated_at FROM progress_milestones`
const taskSelect = `SELECT task_id,project_id,COALESCE(milestone_id::text,''),title,description,status,work_state,COALESCE(assignee_id::text,''),start_at,due_at,completed_at,source,source_run_id,COALESCE(source_evaluation_id::text,''),manual_override_fields,related_object_ids,created_by,updated_by,created_at,updated_at FROM progress_tasks`
const reminderColumns = `reminder_id,project_id,COALESCE(task_id::text,''),COALESCE(milestone_id::text,''),remind_at,status,note,source,triggered_at,created_by,created_at,updated_at,available_at,attempts,max_attempts,COALESCE(locked_by,''),lease_expires_at,last_error_code,last_error_message`
const claimedReminderColumns = `reminder.reminder_id,reminder.project_id,COALESCE(reminder.task_id::text,''),COALESCE(reminder.milestone_id::text,''),reminder.remind_at,reminder.status,reminder.note,reminder.source,reminder.triggered_at,reminder.created_by,reminder.created_at,reminder.updated_at,reminder.available_at,reminder.attempts,reminder.max_attempts,COALESCE(reminder.locked_by,''),reminder.lease_expires_at,reminder.last_error_code,reminder.last_error_message`
const reminderSelect = `SELECT ` + reminderColumns + ` FROM progress_reminders`
const proposalSelect = `SELECT proposal_id,project_id,proposal_type,COALESCE(target_id::text,''),title,rationale,changes,source,source_run_id,COALESCE(source_evaluation_id::text,''),source_key,proposed_by,status,COALESCE(reviewed_by::text,''),reviewed_at,review_note,created_at,updated_at FROM progress_proposals`

type scanFunc func(...interface{}) error

func scanMilestone(scan scanFunc) (Milestone, error) {
	var item Milestone
	if err := scan(&item.ID, &item.ProjectID, &item.Title, &item.Description, &item.Status, &item.Critical, &item.StartAt, &item.TargetAt, &item.TargetHasTime, &item.CompletedAt, &item.Source, &item.SourceRunID, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Milestone{}, err
	}
	return item, nil
}
func scanTask(scan scanFunc) (Task, error) {
	var item Task
	var overrides, related []byte
	if err := scan(&item.ID, &item.ProjectID, &item.MilestoneID, &item.Title, &item.Description, &item.Status, &item.WorkState, &item.AssigneeID, &item.StartAt, &item.DueAt, &item.CompletedAt, &item.Source, &item.SourceRunID, &item.SourceEvaluationID, &overrides, &related, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Task{}, err
	}
	if err := json.Unmarshal(overrides, &item.ManualOverrideFields); err != nil {
		return Task{}, err
	}
	if err := json.Unmarshal(related, &item.RelatedObjectIDs); err != nil {
		return Task{}, err
	}
	item.ManualOverrideFields = nonNilStrings(item.ManualOverrideFields)
	item.RelatedObjectIDs = nonNilStrings(item.RelatedObjectIDs)
	return item, nil
}
func scanReminder(scan scanFunc) (Reminder, error) {
	var item Reminder
	if err := scan(&item.ID, &item.ProjectID, &item.TaskID, &item.MilestoneID, &item.RemindAt, &item.Status, &item.Note, &item.Source, &item.TriggeredAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.AvailableAt, &item.Attempts, &item.MaxAttempts, &item.LockedBy, &item.LeaseExpiresAt, &item.LastErrorCode, &item.LastErrorMessage); err != nil {
		return Reminder{}, err
	}
	return item, nil
}
func scanProposal(scan scanFunc) (Proposal, error) {
	var item Proposal
	var raw []byte
	if err := scan(&item.ID, &item.ProjectID, &item.ProposalType, &item.TargetID, &item.Title, &item.Rationale, &raw, &item.Source, &item.SourceRunID, &item.SourceEvaluationID, &item.SourceKey, &item.ProposedBy, &item.Status, &item.ReviewedBy, &item.ReviewedAt, &item.ReviewNote, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Proposal{}, err
	}
	if err := json.Unmarshal(raw, &item.Changes); err != nil {
		return Proposal{}, err
	}
	return item, nil
}

func taskInputFields(input UpdateTaskInput) []string {
	fields := []string{}
	if input.MilestoneID != nil {
		fields = append(fields, "milestone_id")
	}
	if input.Title != nil {
		fields = append(fields, "title")
	}
	if input.Description != nil {
		fields = append(fields, "description")
	}
	if input.Status != nil {
		fields = append(fields, "status")
	}
	if input.AssigneeID != nil {
		fields = append(fields, "assignee_id")
	}
	if input.StartAt != nil {
		fields = append(fields, "start_at")
	}
	if input.DueAt != nil {
		fields = append(fields, "due_at")
	}
	if input.RelatedObjectIDs != nil {
		fields = append(fields, "related_object_ids")
	}
	return fields
}

func mergeOverrideFields(current, changed []string) []string {
	seen := make(map[string]bool, len(current)+len(changed))
	for _, field := range append(append([]string{}, current...), changed...) {
		if field != "" {
			seen[field] = true
		}
	}
	result := make([]string, 0, len(seen))
	for field := range seen {
		result = append(result, field)
	}
	sort.Strings(result)
	return result
}

func filterTaskInput(input UpdateTaskInput, overrideFields []string) UpdateTaskInput {
	overridden := map[string]bool{}
	for _, field := range overrideFields {
		overridden[field] = true
	}
	if overridden["milestone_id"] {
		input.MilestoneID = nil
	}
	if overridden["title"] {
		input.Title = nil
	}
	if overridden["description"] {
		input.Description = nil
	}
	if overridden["status"] {
		input.Status = nil
	}
	if overridden["assignee_id"] {
		input.AssigneeID = nil
	}
	if overridden["start_at"] {
		input.StartAt = nil
	}
	if overridden["due_at"] {
		input.DueAt = nil
	}
	if overridden["related_object_ids"] {
		input.RelatedObjectIDs = nil
	}
	return input
}

func validProposalType(value string) bool {
	switch value {
	case "milestone.create", "milestone.update", "milestone.complete", "task.create", "task.update", "task.complete":
		return true
	default:
		return false
	}
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func mapPostgresMutationError(err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503", "22P02":
			return ErrReferenceInvalid
		case "23505":
			return ErrConflict
		case "23514":
			if postgresError.ConstraintName == "progress_tasks_related_object_ids_uuid_array_check" ||
				postgresError.ConstraintName == "progress_proposals_target_shape_check" ||
				postgresError.ConstraintName == "progress_proposals_task_reference_shapes_check" {
				return ErrReferenceInvalid
			}
			return ErrInvalid
		}
	}
	return err
}

func normalizeReferenceID(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.TrimSpace(value) != value {
		return "", ErrReferenceInvalid
	}
	parsed, err := uuid.Parse(value)
	if err != nil || !strings.EqualFold(parsed.String(), value) {
		return "", ErrReferenceInvalid
	}
	return parsed.String(), nil
}

func normalizeRelatedObjectIDs(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized, err := normalizeReferenceID(value)
		if err != nil || normalized == "" {
			return nil, ErrReferenceInvalid
		}
		result = append(result, normalized)
	}
	return result, nil
}

func validateMilestoneReference(ctx context.Context, tx transaction.Tx, projectID, milestoneID string) error {
	if milestoneID == "" {
		return nil
	}
	var found string
	err := tx.QueryRowContext(ctx, `SELECT milestone_id::text FROM progress_milestones WHERE project_id=$1 AND milestone_id=$2 FOR KEY SHARE`, projectID, milestoneID).Scan(&found)
	return mapReferenceLookupError(err)
}

func validateTaskReference(ctx context.Context, tx transaction.Tx, projectID, taskID string) error {
	if taskID == "" {
		return nil
	}
	var found string
	err := tx.QueryRowContext(ctx, `SELECT task_id::text FROM progress_tasks WHERE project_id=$1 AND task_id=$2 FOR KEY SHARE`, projectID, taskID).Scan(&found)
	return mapReferenceLookupError(err)
}

func validateAssigneeReference(ctx context.Context, tx transaction.Tx, projectID, assigneeID string) error {
	if assigneeID == "" {
		return nil
	}
	var found string
	err := tx.QueryRowContext(ctx, `SELECT user_id::text FROM project_members WHERE project_id=$1 AND user_id=$2 FOR KEY SHARE`, projectID, assigneeID).Scan(&found)
	return mapReferenceLookupError(err)
}

func mapReferenceLookupError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReferenceInvalid
	}
	return mapPostgresMutationError(err)
}

func (store PostgresStore) validateTaskReferences(
	ctx context.Context,
	tx transaction.Tx,
	projectID string,
	milestoneID string,
	assigneeID string,
	relatedObjectIDs []string,
	validateMilestone bool,
	validateAssignee bool,
	validateRelatedObjects bool,
) error {
	if validateMilestone {
		if err := validateMilestoneReference(ctx, tx, projectID, milestoneID); err != nil {
			return err
		}
	}
	if validateAssignee {
		if err := validateAssigneeReference(ctx, tx, projectID, assigneeID); err != nil {
			return err
		}
	}
	if !validateRelatedObjects || len(relatedObjectIDs) == 0 {
		return nil
	}
	if store.References == nil {
		return fmt.Errorf("Progress object reference validator is not configured")
	}
	valid, err := store.References.ValidateProgressReferences(ctx, tx, projectID, relatedObjectIDs)
	if err != nil {
		return mapPostgresMutationError(err)
	}
	if !valid {
		return ErrReferenceInvalid
	}
	return nil
}

func (store PostgresStore) validateProposalReferences(
	ctx context.Context,
	tx transaction.Tx,
	projectID string,
	proposalType string,
	targetID string,
	changes map[string]interface{},
) (string, map[string]interface{}, error) {
	normalizedChanges := make(map[string]interface{}, len(changes))
	for key, value := range changes {
		normalizedChanges[key] = value
	}
	normalizedTarget, err := normalizeReferenceID(targetID)
	if err != nil {
		return "", nil, err
	}
	if proposalType == "task.complete" || proposalType == "milestone.complete" {
		if len(normalizedChanges) != 0 {
			return "", nil, ErrInvalid
		}
	}
	if status, ok := normalizedChanges["status"].(string); ok {
		if (proposalType == "task.create" || proposalType == "task.update") && status == string(TaskDone) {
			return "", nil, ErrInvalid
		}
		if (proposalType == "milestone.create" || proposalType == "milestone.update") && status == StatusCompleted {
			return "", nil, ErrInvalid
		}
	}
	switch proposalType {
	case "milestone.create", "task.create":
		if normalizedTarget != "" {
			return "", nil, ErrReferenceInvalid
		}
	case "milestone.update", "milestone.complete":
		if normalizedTarget == "" {
			return "", nil, ErrReferenceInvalid
		}
		if err := validateMilestoneReference(ctx, tx, projectID, normalizedTarget); err != nil {
			return "", nil, err
		}
	case "task.update", "task.complete":
		if normalizedTarget == "" {
			return "", nil, ErrReferenceInvalid
		}
		if err := validateTaskReference(ctx, tx, projectID, normalizedTarget); err != nil {
			return "", nil, err
		}
	default:
		return "", nil, ErrInvalid
	}

	if proposalType != "task.create" && proposalType != "task.update" {
		return normalizedTarget, normalizedChanges, nil
	}
	milestoneID, milestonePresent, err := referenceChange(normalizedChanges, "milestone_id")
	if err != nil {
		return "", nil, err
	}
	if milestonePresent {
		normalizedChanges["milestone_id"] = milestoneID
	}
	assigneeID, assigneePresent, err := referenceChange(normalizedChanges, "assignee_id")
	if err != nil {
		return "", nil, err
	}
	if assigneePresent {
		normalizedChanges["assignee_id"] = assigneeID
	}
	relatedObjectIDs, relatedPresent, err := relatedObjectIDsChange(normalizedChanges)
	if err != nil {
		return "", nil, err
	}
	if relatedPresent {
		normalizedChanges["related_object_ids"] = relatedObjectIDs
	}
	if err := store.validateTaskReferences(ctx, tx, projectID, milestoneID, assigneeID, relatedObjectIDs, milestonePresent, assigneePresent, relatedPresent); err != nil {
		return "", nil, err
	}
	return normalizedTarget, normalizedChanges, nil
}

func referenceChange(changes map[string]interface{}, key string) (string, bool, error) {
	raw, exists := changes[key]
	if !exists {
		return "", false, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", true, ErrReferenceInvalid
	}
	normalized, err := normalizeReferenceID(value)
	return normalized, true, err
}

func relatedObjectIDsChange(changes map[string]interface{}) ([]string, bool, error) {
	raw, exists := changes["related_object_ids"]
	if !exists {
		return nil, false, nil
	}
	var values []string
	switch typed := raw.(type) {
	case []string:
		values = typed
	case []interface{}:
		values = make([]string, 0, len(typed))
		for _, item := range typed {
			value, ok := item.(string)
			if !ok {
				return nil, true, ErrReferenceInvalid
			}
			values = append(values, value)
		}
	default:
		return nil, true, ErrReferenceInvalid
	}
	normalized, err := normalizeRelatedObjectIDs(values)
	return normalized, true, err
}
func validMilestoneStatus(value string) bool {
	return value == StatusPlanned || value == StatusInProgress || value == StatusCompleted
}
func validTaskStatus(value string) bool {
	return value == TaskTodo || value == TaskInProgress || value == TaskBlocked || value == TaskDone
}
func nonNilStrings(value []string) []string {
	if value == nil {
		return []string{}
	}
	return value
}
func stringChange(values map[string]interface{}, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}
func boolChange(values map[string]interface{}, key string) bool {
	value, _ := values[key].(bool)
	return value
}
func timeChange(values map[string]interface{}, key string) *time.Time {
	value, ok := values[key].(string)
	if !ok || value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &parsed
}
