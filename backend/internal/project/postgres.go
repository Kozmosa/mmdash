package project

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
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

func (store PostgresStore) CreateInvitation(ctx context.Context, actorID string, projectID string, email string, role Role, expiresAt time.Time) (IssuedInvitation, error) {
	now := store.Clock.Now().UTC()
	if _, err := store.pendingInvitation(ctx, projectID, email, now); err == nil {
		return IssuedInvitation{}, ErrConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return IssuedInvitation{}, err
	}
	invitationID, err := store.Generator.New()
	if err != nil {
		return IssuedInvitation{}, err
	}
	token, err := newInvitationToken()
	if err != nil {
		return IssuedInvitation{}, err
	}
	var invitation Invitation
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO project_invitations (invitation_id, project_id, email, role, token_hash, status, invited_by, expires_at, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,'pending',$6,$7,$8,$8)
			RETURNING invitation_id, project_id, (SELECT name FROM projects WHERE project_id=$2), email, role, status, invited_by, expires_at, created_at
		`, invitationID, projectID, email, role, hashInvitationToken(token), actorID, expiresAt, now).Scan(&invitation.ID, &invitation.ProjectID, &invitation.ProjectName, &invitation.Email, &invitation.Role, &invitation.Status, &invitation.InvitedBy, &invitation.ExpiresAt, &invitation.CreatedAt); err != nil {
			return err
		}
		_, err := store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"user_id": actorID}, EventType: "project.member.invited", Payload: map[string]interface{}{"email": email, "invitation_id": invitationID, "project_id": projectID, "role": role}, Producer: "project", ProjectID: projectID})
		return err
	})
	if err != nil {
		return IssuedInvitation{}, wrap("create invitation", err)
	}
	return IssuedInvitation{Invitation: invitation, Token: token}, nil
}

func (store PostgresStore) pendingInvitation(ctx context.Context, projectID string, email string, now time.Time) (Invitation, error) {
	var invitation Invitation
	err := store.DB.QueryRowContext(ctx, `SELECT i.invitation_id,i.project_id,p.name,i.email,i.role,i.status,i.invited_by,i.expires_at,i.created_at FROM project_invitations i JOIN projects p USING(project_id) WHERE i.project_id=$1 AND LOWER(i.email)=LOWER($2) AND i.status='pending' AND i.expires_at>$3`, projectID, email, now).Scan(&invitation.ID, &invitation.ProjectID, &invitation.ProjectName, &invitation.Email, &invitation.Role, &invitation.Status, &invitation.InvitedBy, &invitation.ExpiresAt, &invitation.CreatedAt)
	return invitation, err
}

func (store PostgresStore) ListInvitations(ctx context.Context, projectID string, now time.Time) ([]Invitation, error) {
	_, _ = store.DB.ExecContext(ctx, `UPDATE project_invitations SET status='expired', updated_at=$2 WHERE project_id=$1 AND status='pending' AND expires_at <= $2`, projectID, now)
	rows, err := store.DB.QueryContext(ctx, `SELECT i.invitation_id,i.project_id,p.name,i.email,i.role,i.status,i.invited_by,i.expires_at,i.created_at FROM project_invitations i JOIN projects p USING(project_id) WHERE i.project_id=$1 ORDER BY i.created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Invitation{}
	for rows.Next() {
		var i Invitation
		if err := rows.Scan(&i.ID, &i.ProjectID, &i.ProjectName, &i.Email, &i.Role, &i.Status, &i.InvitedBy, &i.ExpiresAt, &i.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func (store PostgresStore) PreviewInvitation(ctx context.Context, tokenHash string, now time.Time) (Invitation, error) {
	var i Invitation
	err := store.DB.QueryRowContext(ctx, `SELECT i.invitation_id,i.project_id,p.name,i.email,i.role,i.status,i.invited_by,i.expires_at,i.created_at FROM project_invitations i JOIN projects p USING(project_id) WHERE i.token_hash=$1 AND i.status='pending' AND i.expires_at>$2`, tokenHash, now).Scan(&i.ID, &i.ProjectID, &i.ProjectName, &i.Email, &i.Role, &i.Status, &i.InvitedBy, &i.ExpiresAt, &i.CreatedAt)
	return i, err
}

func (store PostgresStore) AcceptInvitation(ctx context.Context, tokenHash string, userID string, email string, now time.Time) (Member, error) {
	var member Member
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var invitationID, projectID string
		var role Role
		if err := tx.QueryRowContext(ctx, `SELECT invitation_id,project_id,role FROM project_invitations WHERE token_hash=$1 AND LOWER(email)=LOWER($2) AND status='pending' AND expires_at>$3 FOR UPDATE`, tokenHash, email, now).Scan(&invitationID, &projectID, &role); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES($1,$2,$3,$4,$4) ON CONFLICT(project_id,user_id) DO NOTHING`, projectID, userID, role, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE project_invitations SET status='accepted',accepted_by=$2,accepted_at=$3,updated_at=$3 WHERE invitation_id=$1`, invitationID, userID, now); err != nil {
			return err
		}
		_, err := store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"user_id": userID}, EventType: "project.member.joined", Payload: map[string]interface{}{"project_id": projectID, "role": role, "user_id": userID}, Producer: "project", ProjectID: projectID})
		return err
	})
	if err != nil {
		return Member{}, err
	}
	err = store.DB.QueryRowContext(ctx, `SELECT u.user_id,u.email,u.display_name,m.role,m.created_at FROM project_members m JOIN auth_users u USING(user_id) JOIN project_invitations i ON i.project_id=m.project_id AND i.accepted_by=m.user_id WHERE i.token_hash=$1`, tokenHash).Scan(&member.UserID, &member.Email, &member.DisplayName, &member.Role, &member.JoinedAt)
	return member, err
}

func (store PostgresStore) RevokeInvitation(ctx context.Context, actorID string, projectID string, invitationID string, now time.Time) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE project_invitations SET status='revoked',revoked_at=$3,updated_at=$3 WHERE invitation_id=$1 AND project_id=$2 AND status='pending'`, invitationID, projectID, now)
		if err := requireProjectAffected(result, err); err != nil {
			return err
		}
		_, err = store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"user_id": actorID}, EventType: "project.invitation.revoked", Payload: map[string]interface{}{"invitation_id": invitationID, "project_id": projectID}, Producer: "project", ProjectID: projectID})
		return err
	})
}

func newInvitationToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "mmdash_inv_" + base64.RawURLEncoding.EncodeToString(b), nil
}
func hashInvitationToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
func requireProjectAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

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
			EventType: "project.member.role_changed",
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
