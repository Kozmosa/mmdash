package datahub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PostgresStore persists derived Data Hub state and formal project context.
type PostgresStore struct {
	Clock       clock.Clock
	DB          *sql.DB
	Generator   identity.Generator
	Outbox      outbox.Writer
	Transaction transaction.Manager
}

func (store PostgresStore) ListObjects(
	ctx context.Context,
	projectID string,
	objectType string,
	page pagination.Request,
) (ObjectPage, error) {
	cursorTime, cursorID, err := decodePage(page.Cursor)
	if err != nil {
		return ObjectPage{}, ErrInvalid
	}
	rows, err := store.DB.QueryContext(ctx, `
		SELECT object_id, project_id, object_type, source_module, source_id,
		       title, summary, status, version, metadata, occurred_at,
		       created_at, updated_at
		FROM data_objects
		WHERE project_id = $1
		  AND ($2 = '' OR object_type = $2)
		  AND status <> 'hidden'
		  AND (
		    NULLIF($3, '') IS NULL
		    OR (updated_at, object_id) <
		       (NULLIF($3, '')::timestamptz, NULLIF($4, '')::uuid)
		  )
		ORDER BY updated_at DESC, object_id DESC
		LIMIT $5
	`, projectID, objectType, cursorTime, cursorID, page.Limit+1)
	if err != nil {
		return ObjectPage{}, fmt.Errorf("list data objects: %w", err)
	}
	defer rows.Close()
	items := make([]Object, 0, page.Limit)
	for rows.Next() {
		object, scanErr := scanObject(rows.Scan)
		if scanErr != nil {
			return ObjectPage{}, scanErr
		}
		items = append(items, object)
	}
	if err := rows.Err(); err != nil {
		return ObjectPage{}, err
	}
	hasMore := len(items) > page.Limit
	if hasMore {
		items = items[:page.Limit]
	}
	next, err := objectCursor(items, hasMore)
	return ObjectPage{HasMore: hasMore, Items: items, NextCursor: next}, err
}

func (store PostgresStore) GetObject(
	ctx context.Context,
	projectID string,
	objectID string,
) (Object, error) {
	object, err := scanObject(store.DB.QueryRowContext(ctx, `
		SELECT object_id, project_id, object_type, source_module, source_id,
		       title, summary, status, version, metadata, occurred_at,
		       created_at, updated_at
		FROM data_objects
		WHERE project_id = $1 AND object_id = $2 AND status <> 'hidden'
	`, projectID, objectID).Scan)
	return object, mapNotFound("get data object", err)
}

func (store PostgresStore) ListActivity(
	ctx context.Context,
	projectID string,
	page pagination.Request,
) (ActivityPage, error) {
	cursorTime, cursorID, err := decodePage(page.Cursor)
	if err != nil {
		return ActivityPage{}, ErrInvalid
	}
	rows, err := store.DB.QueryContext(ctx, `
		SELECT activity_id, project_id, COALESCE(object_id::text, ''),
		       activity_type, title, summary, actor, metadata,
		       occurred_at, created_at
		FROM data_activity
		WHERE project_id = $1
		  AND (
		    NULLIF($2, '') IS NULL
		    OR (occurred_at, activity_id) <
		       (NULLIF($2, '')::timestamptz, NULLIF($3, '')::uuid)
		  )
		ORDER BY occurred_at DESC, activity_id DESC
		LIMIT $4
	`, projectID, cursorTime, cursorID, page.Limit+1)
	if err != nil {
		return ActivityPage{}, fmt.Errorf("list data activity: %w", err)
	}
	defer rows.Close()
	items := make([]Activity, 0, page.Limit)
	for rows.Next() {
		activity, scanErr := scanActivity(rows.Scan)
		if scanErr != nil {
			return ActivityPage{}, scanErr
		}
		items = append(items, activity)
	}
	if err := rows.Err(); err != nil {
		return ActivityPage{}, err
	}
	hasMore := len(items) > page.Limit
	if hasMore {
		items = items[:page.Limit]
	}
	next, err := activityCursor(items, hasMore)
	return ActivityPage{HasMore: hasMore, Items: items, NextCursor: next}, err
}

func (store PostgresStore) CreateProposal(
	ctx context.Context,
	projectID string,
	userID string,
	input CreateProposalInput,
) (ContextProposal, error) {
	proposalID, err := store.Generator.New()
	if err != nil {
		return ContextProposal{}, err
	}
	activityID, err := store.Generator.New()
	if err != nil {
		return ContextProposal{}, err
	}
	now := store.Clock.Now().UTC()
	proposal := ContextProposal{
		Content: input.Content, ContextType: input.ContextType, CreatedAt: now,
		ID: proposalID, ProjectID: projectID, ProposedBy: userID,
		Rationale: input.Rationale, ReviewNote: "",
		SourceObjectIDs: nonNilStrings(input.SourceObjectIDs),
		Status:          "pending", Title: input.Title, UpdatedAt: now,
	}
	sourceIDs, _ := json.Marshal(proposal.SourceObjectIDs)
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO data_context_proposals (
				proposal_id, project_id, title, content, context_type,
				source_object_ids, rationale, proposed_by, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		`, proposal.ID, projectID, proposal.Title, proposal.Content,
			proposal.ContextType, sourceIDs, proposal.Rationale, userID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO data_objects (
				object_id, project_id, object_type, source_module, source_id,
				title, summary, status, metadata, occurred_at, created_at, updated_at
			) VALUES ($1, $2, 'context-proposal', 'datahub', $1, $3, $4,
			          'pending', '{}'::jsonb, $5, $5, $5)
		`, proposal.ID, projectID, proposal.Title, proposal.Rationale, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO data_activity (
				activity_id, project_id, object_id, activity_type, title,
				summary, actor, metadata, occurred_at, created_at
			) VALUES ($1, $2, $3, 'context.proposal.created', $4, $5,
			          $6, '{}'::jsonb, $7, $7)
		`, activityID, projectID, proposal.ID, proposal.Title, proposal.Rationale,
			jsonBytes(map[string]string{"user_id": userID}), now); err != nil {
			return err
		}
		_, err := store.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": userID},
			EventType: "context.proposal.created",
			Payload: map[string]interface{}{
				"proposal_id":  proposal.ID,
				"context_type": proposal.ContextType,
			},
			Producer: "datahub", ProjectID: projectID,
		})
		return err
	})
	return proposal, wrap("create context proposal", err)
}

func (store PostgresStore) ListProposals(
	ctx context.Context,
	projectID string,
) ([]ContextProposal, error) {
	rows, err := store.DB.QueryContext(ctx, proposalSelect+`
		WHERE project_id = $1
		ORDER BY created_at DESC, proposal_id DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ContextProposal{}
	for rows.Next() {
		item, scanErr := scanProposal(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) GetProposal(
	ctx context.Context,
	projectID string,
	proposalID string,
) (ContextProposal, error) {
	item, err := scanProposal(store.DB.QueryRowContext(ctx, proposalSelect+`
		WHERE project_id = $1 AND proposal_id = $2
	`, projectID, proposalID).Scan)
	return item, mapNotFound("get context proposal", err)
}

func (store PostgresStore) ReviewProposal(
	ctx context.Context,
	projectID string,
	proposalID string,
	reviewerID string,
	input ReviewProposalInput,
) (ContextProposal, error) {
	var reviewed ContextProposal
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		current, err := scanProposal(tx.QueryRowContext(ctx, proposalSelect+`
			WHERE project_id = $1 AND proposal_id = $2
			FOR UPDATE
		`, projectID, proposalID).Scan)
		if err != nil {
			return mapNotFound("get context proposal for review", err)
		}
		if current.Status != "pending" {
			return ErrConflict
		}
		now := store.Clock.Now().UTC()
		var contextID string
		if input.Decision == "accepted" {
			contextID, err = store.Generator.New()
			if err != nil {
				return err
			}
			sourceIDs, _ := json.Marshal(current.SourceObjectIDs)
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO data_context_entries (
					context_id, project_id, title, content, context_type,
					source_object_ids, proposed_by, confirmed_by, confirmed_at,
					created_at, updated_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, $9)
			`, contextID, projectID, current.Title, current.Content,
				current.ContextType, sourceIDs, current.ProposedBy, reviewerID, now); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO data_objects (
					object_id, project_id, object_type, source_module, source_id,
					title, summary, status, metadata, occurred_at, created_at, updated_at
				) VALUES ($1, $2, 'project-context', 'datahub', $1, $3, $4,
				          'confirmed', '{}'::jsonb, $5, $5, $5)
			`, contextID, projectID, current.Title, current.Content, now); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE data_context_proposals
			SET status = $3, reviewed_by = $4, reviewed_at = $5,
			    review_note = $6, promoted_context_id = NULLIF($7, '')::uuid,
			    updated_at = $5
			WHERE project_id = $1 AND proposal_id = $2
		`, projectID, proposalID, input.Decision, reviewerID, now,
			input.Note, contextID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE data_objects
			SET status = $3, version = version + 1, updated_at = $4
			WHERE project_id = $1 AND object_id = $2
		`, projectID, proposalID, input.Decision, now); err != nil {
			return err
		}
		activityID, err := store.Generator.New()
		if err != nil {
			return err
		}
		eventType := "context.proposal.rejected"
		objectID := proposalID
		if input.Decision == "accepted" {
			eventType = "context.confirmed"
			objectID = contextID
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO data_activity (
				activity_id, project_id, object_id, activity_type, title,
				summary, actor, metadata, occurred_at, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, '{}'::jsonb, $8, $8)
		`, activityID, projectID, objectID, eventType, current.Title,
			input.Note, jsonBytes(map[string]string{"user_id": reviewerID}), now); err != nil {
			return err
		}
		_, err = store.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": reviewerID},
			EventType: eventType,
			Payload: map[string]interface{}{
				"proposal_id": proposalID,
				"context_id":  contextID,
			},
			Producer: "datahub", ProjectID: projectID,
		})
		if err != nil {
			return err
		}
		current.Status = input.Decision
		current.ReviewedBy = reviewerID
		current.ReviewedAt = &now
		current.ReviewNote = input.Note
		current.PromotedContext = contextID
		current.UpdatedAt = now
		reviewed = current
		return nil
	})
	return reviewed, wrap("review context proposal", err)
}

func (store PostgresStore) ListContext(
	ctx context.Context,
	projectID string,
) ([]ContextEntry, error) {
	rows, err := store.DB.QueryContext(ctx, contextSelect+`
		WHERE project_id = $1
		ORDER BY confirmed_at DESC, context_id DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ContextEntry{}
	for rows.Next() {
		item, scanErr := scanContext(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) GetContext(
	ctx context.Context,
	projectID string,
	contextID string,
) (ContextEntry, error) {
	item, err := scanContext(store.DB.QueryRowContext(ctx, contextSelect+`
		WHERE project_id = $1 AND context_id = $2
	`, projectID, contextID).Scan)
	return item, mapNotFound("get project context", err)
}

// ProjectCreated applies the project.created projection exactly once per event.
func (store PostgresStore) ProjectCreated(
	ctx context.Context,
	event contract.EventEnvelope,
) error {
	if event.ProjectID == nil {
		return ErrInvalid
	}
	name, ok := event.Payload["name"].(string)
	if !ok || name == "" {
		return ErrInvalid
	}
	return store.projectEvent(ctx, event, name, "active")
}

// ProjectUpdated refreshes the registry row from authoritative Project state.
func (store PostgresStore) ProjectUpdated(
	ctx context.Context,
	event contract.EventEnvelope,
) error {
	if event.ProjectID == nil {
		return ErrInvalid
	}
	var name, status string
	if err := store.DB.QueryRowContext(ctx, `
		SELECT name, CASE WHEN archived_at IS NULL THEN 'active' ELSE 'archived' END
		FROM projects WHERE project_id = $1
	`, *event.ProjectID).Scan(&name, &status); err != nil {
		return mapNotFound("load project projection source", err)
	}
	return store.projectEvent(ctx, event, name, status)
}

// ProjectMemberChanged projects membership lifecycle events into Data Hub.
func (store PostgresStore) ProjectMemberChanged(ctx context.Context, event contract.EventEnvelope) error {
	if event.ProjectID == nil {
		return ErrInvalid
	}
	userID, ok := event.Payload["user_id"].(string)
	if !ok || userID == "" {
		return ErrInvalid
	}
	projectID := *event.ProjectID
	role, _ := event.Payload["role"].(string)
	title := userID
	status := "active"
	var displayName, currentRole string
	if err := store.DB.QueryRowContext(ctx, `SELECT u.display_name,m.role FROM project_members m JOIN auth_users u USING(user_id) WHERE m.project_id=$1 AND m.user_id=$2`, projectID, userID).Scan(&displayName, &currentRole); err == nil {
		title = displayName
		role = currentRole
	} else if event.EventType == "project.member.removed" {
		status = "removed"
	} else {
		return err
	}
	objectID, err := store.Generator.New()
	if err != nil {
		return err
	}
	activityID, err := store.Generator.New()
	if err != nil {
		return err
	}
	sourceID := projectID + ":" + userID
	actor := jsonBytes(event.Actor)
	metadata := jsonBytes(map[string]interface{}{"role": role, "user_id": userID})
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var done bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM data_activity WHERE event_id=$1)`, event.EventID).Scan(&done); err != nil {
			return err
		}
		if done {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO data_objects(object_id,project_id,object_type,source_module,source_id,title,summary,status,metadata,occurred_at,created_at,updated_at) VALUES($1,$2,'project-member','project',$3,$4,$5,$6,$7,$8,$8,$8) ON CONFLICT(source_module,object_type,source_id) DO UPDATE SET title=EXCLUDED.title,summary=EXCLUDED.summary,status=EXCLUDED.status,metadata=EXCLUDED.metadata,version=data_objects.version+1,occurred_at=EXCLUDED.occurred_at,updated_at=EXCLUDED.updated_at`, objectID, projectID, sourceID, title, role, status, metadata, event.OccurredAt); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO data_activity(activity_id,project_id,object_id,event_id,activity_type,title,summary,actor,metadata,occurred_at,created_at) SELECT $1,$2,object_id,$3,$4,$5,$6,$7,$8,$9,$10 FROM data_objects WHERE source_module='project' AND object_type='project-member' AND source_id=$11 ON CONFLICT(event_id) WHERE event_id IS NOT NULL DO NOTHING`, activityID, projectID, event.EventID, event.EventType, title, role, actor, metadata, event.OccurredAt, store.Clock.Now().UTC(), sourceID)
		return err
	})
}

func (store PostgresStore) projectEvent(
	ctx context.Context,
	event contract.EventEnvelope,
	name string,
	status string,
) error {
	objectID, err := store.Generator.New()
	if err != nil {
		return err
	}
	activityID, err := store.Generator.New()
	if err != nil {
		return err
	}
	projectID := *event.ProjectID
	actor := jsonBytes(event.Actor)
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var alreadyProjected bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM data_activity WHERE event_id = $1
			)
		`, event.EventID).Scan(&alreadyProjected); err != nil {
			return err
		}
		if alreadyProjected {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO data_objects (
				object_id, project_id, object_type, source_module, source_id,
				title, summary, status, metadata, occurred_at, created_at, updated_at
			) VALUES ($1, $2, 'project', 'project', $2::uuid::text, $3, '', $4,
			          '{}'::jsonb, $5, $5, $5)
			ON CONFLICT (source_module, object_type, source_id) DO UPDATE
			SET title = EXCLUDED.title,
			    status = EXCLUDED.status,
			    version = data_objects.version + 1,
			    occurred_at = EXCLUDED.occurred_at,
			    updated_at = EXCLUDED.updated_at
		`, objectID, projectID, name, status, event.OccurredAt); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO data_activity (
				activity_id, project_id, object_id, event_id, activity_type,
				title, summary, actor, metadata, occurred_at, created_at
			)
			SELECT $1, $2, object_id, $3, $4, $5, '',
			       $6, '{}'::jsonb, $7, $8
			FROM data_objects
			WHERE source_module = 'project' AND object_type = 'project'
			  AND source_id = $2::uuid::text
			ON CONFLICT (event_id) WHERE event_id IS NOT NULL DO NOTHING
		`, activityID, projectID, event.EventID, event.EventType, name, actor,
			event.OccurredAt, store.Clock.Now().UTC())
		return err
	})
}

const proposalSelect = `
	SELECT proposal_id, project_id, title, content, context_type,
	       source_object_ids, rationale, proposed_by, status,
	       COALESCE(reviewed_by::text, ''), reviewed_at, review_note,
	       COALESCE(promoted_context_id::text, ''), created_at, updated_at
	FROM data_context_proposals
`

const contextSelect = `
	SELECT context_id, project_id, title, content, context_type,
	       source_object_ids, proposed_by, confirmed_by, confirmed_at,
	       created_at, updated_at
	FROM data_context_entries
`

type scanFunc func(...interface{}) error

func scanObject(scan scanFunc) (Object, error) {
	var item Object
	var metadata []byte
	err := scan(&item.ID, &item.ProjectID, &item.ObjectType, &item.SourceModule,
		&item.SourceID, &item.Title, &item.Summary, &item.Status, &item.Version,
		&metadata, &item.OccurredAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return Object{}, err
	}
	if err := json.Unmarshal(metadata, &item.Metadata); err != nil {
		return Object{}, err
	}
	return item, nil
}

func scanActivity(scan scanFunc) (Activity, error) {
	var item Activity
	var actor, metadata []byte
	err := scan(&item.ID, &item.ProjectID, &item.ObjectID, &item.ActivityType,
		&item.Title, &item.Summary, &actor, &metadata,
		&item.OccurredAt, &item.CreatedAt)
	if err != nil {
		return Activity{}, err
	}
	if err := json.Unmarshal(actor, &item.Actor); err != nil {
		return Activity{}, err
	}
	if err := json.Unmarshal(metadata, &item.Metadata); err != nil {
		return Activity{}, err
	}
	return item, nil
}

func scanProposal(scan scanFunc) (ContextProposal, error) {
	var item ContextProposal
	var sourceIDs []byte
	err := scan(&item.ID, &item.ProjectID, &item.Title, &item.Content,
		&item.ContextType, &sourceIDs, &item.Rationale, &item.ProposedBy,
		&item.Status, &item.ReviewedBy, &item.ReviewedAt, &item.ReviewNote,
		&item.PromotedContext, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return ContextProposal{}, err
	}
	if err := json.Unmarshal(sourceIDs, &item.SourceObjectIDs); err != nil {
		return ContextProposal{}, err
	}
	return item, nil
}

func scanContext(scan scanFunc) (ContextEntry, error) {
	var item ContextEntry
	var sourceIDs []byte
	err := scan(&item.ID, &item.ProjectID, &item.Title, &item.Content,
		&item.ContextType, &sourceIDs, &item.ProposedBy, &item.ConfirmedBy,
		&item.ConfirmedAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return ContextEntry{}, err
	}
	if err := json.Unmarshal(sourceIDs, &item.SourceObjectIDs); err != nil {
		return ContextEntry{}, err
	}
	return item, nil
}

func decodePage(value string) (string, string, error) {
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

func objectCursor(items []Object, hasMore bool) (string, error) {
	if !hasMore || len(items) == 0 {
		return "", nil
	}
	last := items[len(items)-1]
	return pagination.Encode(pagination.Cursor{
		ID: last.ID, SortValue: last.UpdatedAt.Format(time.RFC3339Nano),
	})
}

func activityCursor(items []Activity, hasMore bool) (string, error) {
	if !hasMore || len(items) == 0 {
		return "", nil
	}
	last := items[len(items)-1]
	return pagination.Encode(pagination.Cursor{
		ID: last.ID, SortValue: last.OccurredAt.Format(time.RFC3339Nano),
	})
}

func jsonBytes(value interface{}) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func mapNotFound(operation string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return wrap(operation, err)
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
