package progress

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

type PostgresStore struct {
	Clock       clock.Clock
	DB          *sql.DB
	Generator   identity.Generator
	Outbox      outbox.Writer
	Transaction transaction.Manager
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
	item := Milestone{ID: id, ProjectID: projectID, Title: input.Title, Description: input.Description, Status: StatusPlanned, Critical: input.Critical, StartAt: input.StartAt, TargetAt: input.TargetAt, Source: "human", CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_milestones(milestone_id,project_id,title,description,status,critical,start_at,target_at,source,created_by,updated_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10,$11,$11)`, item.ID, item.ProjectID, item.Title, item.Description, item.Status, item.Critical, item.StartAt, item.TargetAt, item.Source, actorID, now); err != nil {
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
		if _, err := tx.ExecContext(ctx, `UPDATE progress_milestones SET title=$3,description=$4,status=$5,critical=$6,start_at=$7,target_at=$8,completed_at=$9,source=$10,updated_by=$11,updated_at=$12 WHERE project_id=$1 AND milestone_id=$2`, projectID, id, current.Title, current.Description, current.Status, current.Critical, current.StartAt, current.TargetAt, current.CompletedAt, current.Source, actorID, now); err != nil {
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

func (store PostgresStore) CreateTask(ctx context.Context, projectID, actorID string, input CreateTaskInput, source string) (Task, error) {
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
	item := Task{ID: id, ProjectID: projectID, MilestoneID: input.MilestoneID, Title: input.Title, Description: input.Description, Status: input.Status, AssigneeID: input.AssigneeID, StartAt: input.StartAt, DueAt: input.DueAt, Source: source, SourceRunID: input.SourceRunID, RelatedObjectIDs: nonNilStrings(input.RelatedObjectIDs), CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	if item.Status == TaskDone {
		item.CompletedAt = &now
	}
	metadata, _ := json.Marshal(item.RelatedObjectIDs)
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_tasks(task_id,project_id,milestone_id,title,description,status,assignee_id,start_at,due_at,completed_at,source,source_run_id,related_object_ids,created_by,updated_by,created_at,updated_at) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,NULLIF($7,'')::uuid,$8,$9,$10,$11,$12,$13,$14,$14,$15,$15)`, item.ID, projectID, item.MilestoneID, item.Title, item.Description, item.Status, item.AssigneeID, item.StartAt, item.DueAt, item.CompletedAt, item.Source, item.SourceRunID, metadata, actorID, now); err != nil {
			return err
		}
		return store.progressEvent(ctx, tx, "progress.task.created", projectID, actorID, item.ID, "task", item.Title, item.Status, map[string]interface{}{"source": item.Source, "source_run_id": item.SourceRunID})
	})
	return item, err
}

func (store PostgresStore) UpdateTask(ctx context.Context, projectID, id, actorID string, input UpdateTaskInput, source string) (Task, error) {
	var item Task
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		current, err := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE project_id=$1 AND task_id=$2 FOR UPDATE`, projectID, id).Scan)
		if err != nil {
			return mapNotFound(err)
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
		now := store.now()
		if current.Status == TaskDone {
			current.CompletedAt = &now
		} else {
			current.CompletedAt = nil
		}
		current.Source, current.UpdatedBy, current.UpdatedAt = source, actorID, now
		metadata, _ := json.Marshal(nonNilStrings(current.RelatedObjectIDs))
		if _, err := tx.ExecContext(ctx, `UPDATE progress_tasks SET milestone_id=NULLIF($3,'')::uuid,title=$4,description=$5,status=$6,assignee_id=NULLIF($7,'')::uuid,start_at=$8,due_at=$9,completed_at=$10,source=$11,source_run_id=$12,related_object_ids=$13,updated_by=$14,updated_at=$15 WHERE project_id=$1 AND task_id=$2`, projectID, id, current.MilestoneID, current.Title, current.Description, current.Status, current.AssigneeID, current.StartAt, current.DueAt, current.CompletedAt, current.Source, current.SourceRunID, metadata, actorID, now); err != nil {
			return err
		}
		if err := store.progressEvent(ctx, tx, "progress.task.updated", projectID, actorID, id, "task", current.Title, current.Status, map[string]interface{}{"source": current.Source, "source_run_id": current.SourceRunID}); err != nil {
			return err
		}
		item = current
		return nil
	})
	return item, err
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
	id, err := store.Generator.New()
	if err != nil {
		return Dependency{}, err
	}
	now := store.now()
	item := Dependency{ID: id, ProjectID: projectID, TaskID: input.TaskID, DependsOnTaskID: input.DependsOnTaskID, Kind: input.Kind, CreatedBy: actorID, CreatedAt: now}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM progress_tasks WHERE project_id=$1 AND task_id IN ($2,$3)`, projectID, input.TaskID, input.DependsOnTaskID).Scan(&count); err != nil {
			return err
		}
		if count != 2 {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_dependencies(dependency_id,project_id,task_id,depends_on_task_id,kind,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, projectID, item.TaskID, item.DependsOnTaskID, item.Kind, actorID, now); err != nil {
			return mapConflict(err)
		}
		return store.progressEvent(ctx, tx, "progress.dependency.created", projectID, actorID, id, "dependency", item.TaskID, item.Kind, map[string]interface{}{"task_id": item.TaskID, "depends_on_task_id": item.DependsOnTaskID})
	})
	return item, err
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
	rows, err := store.DB.QueryContext(ctx, `SELECT reminder_id,project_id,COALESCE(task_id::text,''),COALESCE(milestone_id::text,''),remind_at,status,note,source,triggered_at,created_by,created_at,updated_at FROM progress_reminders WHERE project_id=$1 ORDER BY remind_at,reminder_id`, projectID)
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
	id, err := store.Generator.New()
	if err != nil {
		return Reminder{}, err
	}
	now := store.now()
	item := Reminder{ID: id, ProjectID: projectID, TaskID: input.TaskID, MilestoneID: input.MilestoneID, RemindAt: input.RemindAt, Status: "pending", Note: input.Note, Source: "human", CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_reminders(reminder_id,project_id,task_id,milestone_id,remind_at,status,note,source,created_by,created_at,updated_at) VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$10)`, item.ID, projectID, item.TaskID, item.MilestoneID, item.RemindAt, item.Status, item.Note, item.Source, actorID, now); err != nil {
			return err
		}
		title := strings.TrimSpace(input.Note)
		if title == "" {
			title = "Progress reminder"
		}
		return store.progressEvent(ctx, tx, "progress.reminder.created", projectID, actorID, id, "reminder", title, item.Status, map[string]interface{}{"remind_at": item.RemindAt.Format(time.RFC3339)})
	})
	return item, err
}

func (store PostgresStore) TriggerReminder(ctx context.Context, projectID, id, actorID string) (Reminder, error) {
	var item Reminder
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		now := store.now()
		updated, err := scanReminder(tx.QueryRowContext(ctx, `UPDATE progress_reminders SET status='triggered',triggered_at=$3,updated_at=$3 WHERE project_id=$1 AND reminder_id=$2 AND status='pending' RETURNING reminder_id,project_id,COALESCE(task_id::text,''),COALESCE(milestone_id::text,''),remind_at,status,note,source,triggered_at,created_by,created_at,updated_at`, projectID, id, now).Scan)
		if err != nil {
			return mapConflict(err)
		}
		item = updated
		title := strings.TrimSpace(item.Note)
		if title == "" {
			title = "Progress reminder"
		}
		return store.progressEvent(ctx, tx, "progress.reminder.due", projectID, actorID, id, "reminder", title, item.Status, map[string]interface{}{"reminder_id": id, "task_id": item.TaskID, "milestone_id": item.MilestoneID})
	})
	return item, err
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
	changes, _ := json.Marshal(item.Changes)
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_proposals(proposal_id,project_id,proposal_type,target_id,title,rationale,changes,source,source_run_id,proposed_by,status,review_note,created_at,updated_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)`, item.ID, projectID, item.ProposalType, item.TargetID, item.Title, item.Rationale, changes, item.Source, item.SourceRunID, actorID, item.Status, item.ReviewNote, now); err != nil {
			return err
		}
		return store.progressEvent(ctx, tx, "progress.proposal.created", projectID, actorID, id, "progress_proposal", item.Title, item.Status, map[string]interface{}{"proposal_type": item.ProposalType, "source": item.Source, "source_run_id": item.SourceRunID})
	})
	return item, err
}

func (store PostgresStore) ReviewProposal(ctx context.Context, projectID, id, reviewerID string, input ReviewProposalInput) (Proposal, error) {
	var result Proposal
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		proposal, err := scanProposal(tx.QueryRowContext(ctx, proposalSelect+` WHERE project_id=$1 AND proposal_id=$2 FOR UPDATE`, projectID, id).Scan)
		if err != nil {
			return mapNotFound(err)
		}
		if proposal.Status != "pending" {
			return ErrConflict
		}
		now := store.now()
		if input.Decision == "accepted" {
			if err := store.applyProposal(ctx, tx, proposal, reviewerID, now); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE progress_proposals SET status=$3,reviewed_by=$4,reviewed_at=$5,review_note=$6,updated_at=$5 WHERE project_id=$1 AND proposal_id=$2`, projectID, id, input.Decision, reviewerID, now, input.Note); err != nil {
			return err
		}
		proposal.Status, proposal.ReviewedBy, proposal.ReviewedAt, proposal.ReviewNote, proposal.UpdatedAt = input.Decision, reviewerID, &now, input.Note, now
		result = proposal
		return store.progressEvent(ctx, tx, "progress.proposal.reviewed", projectID, reviewerID, id, "progress_proposal", proposal.Title, proposal.Status, map[string]interface{}{"proposal_type": proposal.ProposalType, "decision": input.Decision})
	})
	return result, err
}

func (store PostgresStore) applyProposal(ctx context.Context, tx transaction.Tx, proposal Proposal, actorID string, now time.Time) error {
	changes := proposal.Changes
	switch proposal.ProposalType {
	case "milestone.create":
		input := CreateMilestoneInput{Title: stringChange(changes, "title"), Description: stringChange(changes, "description"), Critical: boolChange(changes, "critical"), StartAt: timeChange(changes, "start_at"), TargetAt: timeChange(changes, "target_at")}
		if input.Title == "" {
			return ErrInvalid
		}
		id, err := store.Generator.New()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_milestones(milestone_id,project_id,title,description,status,critical,start_at,target_at,source,source_run_id,created_by,updated_by,created_at,updated_at) VALUES($1,$2,$3,$4,'planned',$5,$6,$7,'proposal',$8,$9,$9,$10,$10)`, id, proposal.ProjectID, input.Title, input.Description, input.Critical, input.StartAt, input.TargetAt, proposal.SourceRunID, actorID, now); err != nil {
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
		if !validMilestoneStatus(current.Status) || current.Title == "" {
			return ErrInvalid
		}
		completed := interface{}(nil)
		if current.Status == StatusCompleted {
			completed = now
		}
		if _, err := tx.ExecContext(ctx, `UPDATE progress_milestones SET title=$3,description=$4,status=$5,critical=$6,start_at=$7,target_at=$8,completed_at=$9,source='proposal',source_run_id=$10,updated_by=$11,updated_at=$12 WHERE project_id=$1 AND milestone_id=$2`, proposal.ProjectID, proposal.TargetID, current.Title, current.Description, current.Status, current.Critical, current.StartAt, current.TargetAt, completed, proposal.SourceRunID, actorID, now); err != nil {
			return err
		}
		return store.progressEvent(ctx, tx, "progress.milestone.updated", proposal.ProjectID, actorID, proposal.TargetID, "milestone", current.Title, current.Status, map[string]interface{}{"source": "proposal", "proposal_id": proposal.ID})
	case "task.create":
		input := CreateTaskInput{MilestoneID: stringChange(changes, "milestone_id"), Title: stringChange(changes, "title"), Description: stringChange(changes, "description"), Status: stringChange(changes, "status"), AssigneeID: stringChange(changes, "assignee_id"), StartAt: timeChange(changes, "start_at"), DueAt: timeChange(changes, "due_at"), SourceRunID: proposal.SourceRunID}
		if input.Title == "" {
			return ErrInvalid
		}
		if input.Status == "" {
			input.Status = TaskTodo
		}
		id, err := store.Generator.New()
		if err != nil {
			return err
		}
		metadata, _ := json.Marshal([]string{})
		var completed interface{}
		if input.Status == TaskDone {
			completed = now
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO progress_tasks(task_id,project_id,milestone_id,title,description,status,assignee_id,start_at,due_at,completed_at,source,source_run_id,related_object_ids,created_by,updated_by,created_at,updated_at) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,NULLIF($7,'')::uuid,$8,$9,$10,'proposal',$11,$12,$13,$13,$14,$14)`, id, proposal.ProjectID, input.MilestoneID, input.Title, input.Description, input.Status, input.AssigneeID, input.StartAt, input.DueAt, completed, proposal.SourceRunID, metadata, actorID, now); err != nil {
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
		}
		if value, ok := changes["assignee_id"].(string); ok {
			current.AssigneeID = value
		}
		if value, ok := changes["milestone_id"].(string); ok {
			current.MilestoneID = value
		}
		if value := timeChange(changes, "start_at"); value != nil {
			current.StartAt = value
		}
		if value := timeChange(changes, "due_at"); value != nil {
			current.DueAt = value
		}
		if current.Title == "" || !validTaskStatus(current.Status) {
			return ErrInvalid
		}
		var completed interface{}
		if current.Status == TaskDone {
			completed = now
		}
		if _, err := tx.ExecContext(ctx, `UPDATE progress_tasks SET milestone_id=NULLIF($3,'')::uuid,title=$4,description=$5,status=$6,assignee_id=NULLIF($7,'')::uuid,start_at=$8,due_at=$9,completed_at=$10,source='proposal',source_run_id=$11,updated_by=$12,updated_at=$13 WHERE project_id=$1 AND task_id=$2`, proposal.ProjectID, proposal.TargetID, current.MilestoneID, current.Title, current.Description, current.Status, current.AssigneeID, current.StartAt, current.DueAt, completed, proposal.SourceRunID, actorID, now); err != nil {
			return err
		}
		return store.progressEvent(ctx, tx, "progress.task.updated", proposal.ProjectID, actorID, proposal.TargetID, "task", current.Title, current.Status, map[string]interface{}{"source": "proposal", "proposal_id": proposal.ID})
	default:
		return ErrInvalid
	}
}

func (store PostgresStore) GetSettings(ctx context.Context, projectID string) (Settings, error) {
	var item Settings
	err := store.DB.QueryRowContext(ctx, `SELECT project_id,auto_task_changes,updated_by,updated_at FROM progress_settings WHERE project_id=$1`, projectID).Scan(&item.ProjectID, &item.AutoTaskChanges, &item.UpdatedBy, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{ProjectID: projectID, AutoTaskChanges: true}, nil
	}
	return item, err
}

func (store PostgresStore) UpdateSettings(ctx context.Context, projectID, actorID string, autoTaskChanges bool) (Settings, error) {
	now := store.now()
	_, err := store.DB.ExecContext(ctx, `INSERT INTO progress_settings(project_id,auto_task_changes,updated_by,updated_at) VALUES($1,$2,$3,$4) ON CONFLICT(project_id) DO UPDATE SET auto_task_changes=EXCLUDED.auto_task_changes,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at`, projectID, autoTaskChanges, actorID, now)
	return Settings{ProjectID: projectID, AutoTaskChanges: autoTaskChanges, UpdatedBy: actorID, UpdatedAt: now}, err
}

func (store PostgresStore) progressEvent(ctx context.Context, tx transaction.Tx, eventType, projectID, actorID, resourceID, resourceType, title, status string, metadata map[string]interface{}) error {
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	metadata["resource_id"], metadata["resource_type"] = resourceID, resourceType
	metadata["title"], metadata["status"] = title, status
	returnEvent := outbox.Event{Actor: map[string]string{"user_id": actorID}, EventType: eventType, Payload: metadata, Producer: "progress", ProjectID: projectID}
	_, err := store.Outbox.Write(ctx, tx, returnEvent)
	return err
}

func (store PostgresStore) now() time.Time {
	if store.Clock == nil {
		return time.Now().UTC()
	}
	return store.Clock.Now().UTC()
}

const milestoneSelect = `SELECT milestone_id,project_id,title,description,status,critical,start_at,target_at,completed_at,source,source_run_id,created_by,updated_by,created_at,updated_at FROM progress_milestones`
const taskSelect = `SELECT task_id,project_id,COALESCE(milestone_id::text,''),title,description,status,COALESCE(assignee_id::text,''),start_at,due_at,completed_at,source,source_run_id,related_object_ids,created_by,updated_by,created_at,updated_at FROM progress_tasks`
const proposalSelect = `SELECT proposal_id,project_id,proposal_type,COALESCE(target_id::text,''),title,rationale,changes,source,source_run_id,proposed_by,status,COALESCE(reviewed_by::text,''),reviewed_at,review_note,created_at,updated_at FROM progress_proposals`

type scanFunc func(...interface{}) error

func scanMilestone(scan scanFunc) (Milestone, error) {
	var item Milestone
	if err := scan(&item.ID, &item.ProjectID, &item.Title, &item.Description, &item.Status, &item.Critical, &item.StartAt, &item.TargetAt, &item.CompletedAt, &item.Source, &item.SourceRunID, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Milestone{}, err
	}
	return item, nil
}
func scanTask(scan scanFunc) (Task, error) {
	var item Task
	var raw []byte
	if err := scan(&item.ID, &item.ProjectID, &item.MilestoneID, &item.Title, &item.Description, &item.Status, &item.AssigneeID, &item.StartAt, &item.DueAt, &item.CompletedAt, &item.Source, &item.SourceRunID, &raw, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Task{}, err
	}
	if err := json.Unmarshal(raw, &item.RelatedObjectIDs); err != nil {
		return Task{}, err
	}
	return item, nil
}
func scanReminder(scan scanFunc) (Reminder, error) {
	var item Reminder
	if err := scan(&item.ID, &item.ProjectID, &item.TaskID, &item.MilestoneID, &item.RemindAt, &item.Status, &item.Note, &item.Source, &item.TriggeredAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Reminder{}, err
	}
	return item, nil
}
func scanProposal(scan scanFunc) (Proposal, error) {
	var item Proposal
	var raw []byte
	if err := scan(&item.ID, &item.ProjectID, &item.ProposalType, &item.TargetID, &item.Title, &item.Rationale, &raw, &item.Source, &item.SourceRunID, &item.ProposedBy, &item.Status, &item.ReviewedBy, &item.ReviewedAt, &item.ReviewNote, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Proposal{}, err
	}
	if err := json.Unmarshal(raw, &item.Changes); err != nil {
		return Proposal{}, err
	}
	return item, nil
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func mapConflict(err error) error {
	if err != nil {
		return ErrConflict
	}
	return nil
}
func validMilestoneStatus(value string) bool {
	return value == StatusPlanned || value == StatusInProgress || value == StatusCompleted || value == StatusCancelled
}
func validTaskStatus(value string) bool {
	return value == TaskTodo || value == TaskInProgress || value == TaskBlocked || value == TaskDone || value == TaskCancelled
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
