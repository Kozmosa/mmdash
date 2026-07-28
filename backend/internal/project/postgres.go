package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PostgresStore persists projects and collaboration membership.
type PostgresStore struct {
	Clock       clock.Clock
	DB          *sql.DB
	Generator   identity.Generator
	Outbox      outbox.Writer
	Transaction transaction.Manager
}

func (store PostgresStore) Create(
	ctx context.Context,
	userID string,
	input CreateInput,
) (Project, error) {
	projectID, err := store.Generator.New()
	if err != nil {
		return Project{}, err
	}
	now := store.Clock.Now().UTC()
	project := Project{
		CreatedAt:          now,
		CreatedBy:          userID,
		ID:                 projectID,
		Name:               input.Name,
		ProblemSummary:     input.ProblemSummary,
		ProblemTitle:       input.ProblemTitle,
		ProjectConstraints: nonNil(input.ProjectConstraints),
		Role:               RoleOwner,
		SourceArtifactIDs:  nonNil(input.SourceArtifactIDs),
		UpdatedAt:          now,
	}
	constraints, _ := json.Marshal(project.ProjectConstraints)
	artifacts, _ := json.Marshal(project.SourceArtifactIDs)
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO projects (
				project_id, name, problem_title, problem_summary,
				project_constraints, source_artifact_ids,
				created_by, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		`, project.ID, project.Name, project.ProblemTitle, project.ProblemSummary,
			constraints, artifacts, userID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO project_members (
				project_id, user_id, role, created_at, updated_at
			) VALUES ($1, $2, 'owner', $3, $3)
		`, project.ID, userID, now); err != nil {
			return err
		}
		_, err := store.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": userID},
			EventType: "project.created",
			Payload: map[string]interface{}{
				"name":       project.Name,
				"project_id": project.ID,
			},
			Producer:  "project",
			ProjectID: project.ID,
		})
		return err
	})
	return project, wrap("create project", err)
}

func (store PostgresStore) List(
	ctx context.Context,
	userID string,
	includeArchived bool,
) ([]Project, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT project_id, name, problem_title, problem_summary,
		       project_constraints, source_artifact_ids, created_by,
		       archived_at, project.created_at, project.updated_at, member.role
		FROM projects AS project
		JOIN project_members AS member USING (project_id)
		WHERE member.user_id = $1
		  AND ($2 OR project.archived_at IS NULL)
		ORDER BY project.updated_at DESC, project.project_id
	`, userID, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []Project{}
	for rows.Next() {
		project, err := scanProject(rows.Scan)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (store PostgresStore) Get(
	ctx context.Context,
	userID string,
	projectID string,
) (Project, error) {
	project, err := scanProject(store.DB.QueryRowContext(ctx, `
		SELECT project_id, name, problem_title, problem_summary,
		       project_constraints, source_artifact_ids, created_by,
		       archived_at, project.created_at, project.updated_at, member.role
		FROM projects AS project
		JOIN project_members AS member USING (project_id)
		WHERE project.project_id = $1 AND member.user_id = $2
	`, projectID, userID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return project, err
}

func (store PostgresStore) FindRole(
	ctx context.Context,
	userID string,
	projectID string,
) (Role, error) {
	var role Role
	err := store.DB.QueryRowContext(ctx, `
		SELECT role FROM project_members
		WHERE project_id = $1 AND user_id = $2
	`, projectID, userID).Scan(&role)
	return role, err
}

func (store PostgresStore) Update(
	ctx context.Context,
	actorID string,
	projectID string,
	input UpdateInput,
) (Project, error) {
	now := store.Clock.Now().UTC()
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE projects
			SET name = COALESCE($2, name),
			    problem_title = COALESCE($3, problem_title),
			    problem_summary = COALESCE($4, problem_summary),
			    project_constraints = COALESCE($5::JSONB, project_constraints),
			    source_artifact_ids = COALESCE($6::JSONB, source_artifact_ids),
			    archived_at = CASE
			      WHEN $7::BOOLEAN IS NULL THEN archived_at
			      WHEN $7 THEN COALESCE(archived_at, $8)
			      ELSE NULL
			    END,
			    updated_at = $8
			WHERE project_id = $1
		`,
			projectID,
			input.Name,
			input.ProblemTitle,
			input.ProblemSummary,
			jsonValue(input.ProjectConstraints),
			jsonValue(input.SourceArtifactIDs),
			input.Archived,
			now,
		)
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
		_, err = store.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": actorID},
			EventType: "project.updated",
			Payload:   map[string]interface{}{"project_id": projectID},
			Producer:  "project",
			ProjectID: projectID,
		})
		return err
	})
	if err != nil {
		return Project{}, wrap("update project", err)
	}
	return store.Get(ctx, actorID, projectID)
}

func (store PostgresStore) ListMembers(ctx context.Context, projectID string) ([]Member, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT users.user_id, users.email, users.display_name,
		       member.role, member.created_at
		FROM project_members AS member
		JOIN auth_users AS users USING (user_id)
		WHERE member.project_id = $1
		ORDER BY CASE member.role
			WHEN 'owner' THEN 0 WHEN 'maintainer' THEN 1
			WHEN 'editor' THEN 2 WHEN 'viewer' THEN 3
			WHEN 'agent' THEN 4 ELSE 5 END,
			users.display_name
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := []Member{}
	for rows.Next() {
		var member Member
		if err := rows.Scan(
			&member.UserID,
			&member.Email,
			&member.DisplayName,
			&member.Role,
			&member.JoinedAt,
		); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (store PostgresStore) UpsertMember(
	ctx context.Context,
	actorID string,
	projectID string,
	userID string,
	role Role,
) (Member, error) {
	now := store.Clock.Now().UTC()
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO project_members (
				project_id, user_id, role, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $4)
			ON CONFLICT (project_id, user_id)
			DO UPDATE SET role = EXCLUDED.role, updated_at = EXCLUDED.updated_at
		`, projectID, userID, role, now); err != nil {
			return err
		}
		_, err := store.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": actorID},
			EventType: "project.member.updated",
			Payload: map[string]interface{}{
				"project_id": projectID,
				"role":       role,
				"user_id":    userID,
			},
			Producer:  "project",
			ProjectID: projectID,
		})
		return err
	})
	if err != nil {
		return Member{}, wrap("upsert project member", err)
	}
	var member Member
	err = store.DB.QueryRowContext(ctx, `
		SELECT users.user_id, users.email, users.display_name,
		       member.role, member.created_at
		FROM project_members AS member
		JOIN auth_users AS users USING (user_id)
		WHERE member.project_id = $1 AND member.user_id = $2
	`, projectID, userID).Scan(
		&member.UserID,
		&member.Email,
		&member.DisplayName,
		&member.Role,
		&member.JoinedAt,
	)
	return member, err
}

func (store PostgresStore) RemoveMember(
	ctx context.Context,
	actorID string,
	projectID string,
	userID string,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			DELETE FROM project_members AS target
			WHERE target.project_id = $1
			  AND target.user_id = $2
			  AND (
			    target.role <> 'owner'
			    OR 1 < (
			      SELECT COUNT(*) FROM project_members
			      WHERE project_id = $1 AND role = 'owner'
			    )
			  )
		`, projectID, userID)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrConflict
		}
		_, err = store.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": actorID},
			EventType: "project.member.removed",
			Payload: map[string]interface{}{
				"project_id": projectID,
				"user_id":    userID,
			},
			Producer:  "project",
			ProjectID: projectID,
		})
		return err
	})
}

type scanFunction func(...interface{}) error

func scanProject(scan scanFunction) (Project, error) {
	var project Project
	var constraints []byte
	var artifacts []byte
	err := scan(
		&project.ID,
		&project.Name,
		&project.ProblemTitle,
		&project.ProblemSummary,
		&constraints,
		&artifacts,
		&project.CreatedBy,
		&project.ArchivedAt,
		&project.CreatedAt,
		&project.UpdatedAt,
		&project.Role,
	)
	if err != nil {
		return Project{}, err
	}
	if err := json.Unmarshal(constraints, &project.ProjectConstraints); err != nil {
		return Project{}, fmt.Errorf("decode project constraints: %w", err)
	}
	if err := json.Unmarshal(artifacts, &project.SourceArtifactIDs); err != nil {
		return Project{}, fmt.Errorf("decode source artifact ids: %w", err)
	}
	return project, nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func jsonValue(values *[]string) interface{} {
	if values == nil {
		return nil
	}
	encoded, _ := json.Marshal(nonNil(*values))
	return encoded
}
