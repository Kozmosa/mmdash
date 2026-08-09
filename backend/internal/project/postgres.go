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

	"github.com/jackc/pgconn"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func (store PostgresStore) CreateInvitation(ctx context.Context, actorID string, projectID string, email string, role Role, expiresAt time.Time) (IssuedInvitation, error) {
	now := store.Clock.Now().UTC()
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
		if _, err := tx.ExecContext(ctx, `
			UPDATE project_invitations
			SET status = 'expired', updated_at = $3
			WHERE project_id = $1 AND LOWER(email) = LOWER($2)
			  AND status = 'pending' AND expires_at <= $3
		`, projectID, email, now); err != nil {
			return err
		}
		var memberExists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM project_members AS member
				JOIN auth_users AS app_user USING (user_id)
				WHERE member.project_id = $1
				  AND LOWER(app_user.email) = LOWER($2)
			)
		`, projectID, email).Scan(&memberExists); err != nil {
			return err
		}
		if memberExists {
			return ErrMemberExists
		}
		var replacedInvitationID string
		if err := tx.QueryRowContext(ctx, `
			UPDATE project_invitations
			SET status = 'revoked', revoked_at = $3, updated_at = $3
			WHERE project_id = $1 AND LOWER(email) = LOWER($2)
			  AND status = 'pending' AND expires_at > $3
			RETURNING invitation_id
		`, projectID, email, now).Scan(&replacedInvitationID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO project_invitations (invitation_id, project_id, email, role, token_hash, status, invited_by, expires_at, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,'pending',$6,$7,$8,$8)
			RETURNING invitation_id, project_id, (SELECT name FROM projects WHERE project_id=$2), email, role, status, invited_by, expires_at, created_at
		`, invitationID, projectID, email, role, hashInvitationToken(token), actorID, expiresAt, now).Scan(&invitation.ID, &invitation.ProjectID, &invitation.ProjectName, &invitation.Email, &invitation.Role, &invitation.Status, &invitation.InvitedBy, &invitation.ExpiresAt, &invitation.CreatedAt); err != nil {
			return err
		}
		if replacedInvitationID != "" {
			if _, err := store.Outbox.Write(ctx, tx, outbox.Event{
				Actor:     map[string]string{"user_id": actorID},
				EventType: "project.invitation.revoked",
				Payload: map[string]interface{}{
					"invitation_id": replacedInvitationID,
					"project_id":    projectID,
					"reason":        "reissued",
				},
				Producer:  "project",
				ProjectID: projectID,
			}); err != nil {
				return err
			}
		}
		_, err := store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"user_id": actorID}, EventType: "project.member.invited", Payload: map[string]interface{}{
			"normalized_email":   strings.ToLower(strings.TrimSpace(email)),
			"invitation_id":      invitationID,
			"project_id":         projectID,
			"role":               role,
			"invited_by_user_id": actorID,
			"expires_at":         expiresAt,
		}, Producer: "project", ProjectID: projectID})
		return err
	})
	if err != nil {
		if isUniqueViolation(err) {
			return IssuedInvitation{}, ErrConflict
		}
		return IssuedInvitation{}, wrap("create invitation", err)
	}
	return IssuedInvitation{Invitation: invitation, Token: token}, nil
}

func (store PostgresStore) ListInvitations(ctx context.Context, projectID string, now time.Time) ([]Invitation, error) {
	if err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		rows, err := tx.QueryContext(ctx, `UPDATE project_invitations SET status='expired', updated_at=$2 WHERE project_id=$1 AND status='pending' AND expires_at <= $2 RETURNING invitation_id`, projectID, now)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var invitationID string
			if err := rows.Scan(&invitationID); err != nil {
				return err
			}
			if _, err := store.Outbox.Write(ctx, tx, outbox.Event{EventType: "project.invitation.expired", Payload: map[string]interface{}{"invitation_id": invitationID, "project_id": projectID}, Producer: "project", ProjectID: projectID}); err != nil {
				return err
			}
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
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
	err := store.DB.QueryRowContext(ctx, `SELECT i.invitation_id,i.project_id,p.name,i.email,i.role,i.status,i.invited_by,i.expires_at,i.created_at FROM project_invitations i JOIN projects p USING(project_id) WHERE i.token_hash=$1 AND i.status='pending' AND i.expires_at>$2 AND p.deleted_at IS NULL`, tokenHash, now).Scan(&i.ID, &i.ProjectID, &i.ProjectName, &i.Email, &i.Role, &i.Status, &i.InvitedBy, &i.ExpiresAt, &i.CreatedAt)
	return i, err
}

func (store PostgresStore) DeclineInvitation(ctx context.Context, tokenHash string, now time.Time) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var invitationID, projectID string
		if err := tx.QueryRowContext(ctx, `
			UPDATE project_invitations
			SET status = 'declined', declined_at = $2, updated_at = $2
			WHERE token_hash = $1 AND status = 'pending' AND expires_at > $2
			RETURNING invitation_id, project_id
		`, tokenHash, now).Scan(&invitationID, &projectID); err != nil {
			return err
		}
		_, err := store.Outbox.Write(ctx, tx, outbox.Event{
			EventType: "project.invitation.declined",
			Payload: map[string]interface{}{
				"invitation_id": invitationID,
				"project_id":    projectID,
			},
			Producer:  "project",
			ProjectID: projectID,
		})
		return err
	})
}

func (store PostgresStore) AcceptInvitation(ctx context.Context, tokenHash string, userID string, email string, now time.Time) (Member, error) {
	var member Member
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var acceptErr error
		member, acceptErr = store.AcceptInvitationInTransaction(ctx, tx, tokenHash, userID, email, now)
		return acceptErr
	})
	if err != nil {
		return Member{}, err
	}
	return member, nil
}

func (store PostgresStore) AcceptInvitationByID(ctx context.Context, invitationID, userID, email string, now time.Time) (Member, error) {
	var member Member
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var projectID string
		var role Role
		if err := tx.QueryRowContext(ctx, `SELECT project_id,role FROM project_invitations WHERE invitation_id=$1 AND LOWER(email)=LOWER($2) AND status='pending' AND expires_at>$3 FOR UPDATE`, invitationID, email, now).Scan(&projectID, &role); err != nil {
			return err
		}
		if !isInvitableHumanRole(role) {
			return ErrInvalid
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES($1,$2,$3,$4,$4) ON CONFLICT(project_id,user_id) DO NOTHING`, projectID, userID, role, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE project_invitations SET status='accepted',accepted_by=$2,accepted_at=$3,updated_at=$3 WHERE invitation_id=$1`, invitationID, userID, now); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT u.user_id,u.email,u.display_name,m.role,m.created_at FROM project_members m JOIN auth_users u USING(user_id) WHERE m.project_id=$1 AND m.user_id=$2`, projectID, userID).Scan(&member.UserID, &member.Email, &member.DisplayName, &member.Role, &member.JoinedAt); err != nil {
			return err
		}
		_, err := store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"user_id": userID}, EventType: "project.member.joined", Payload: map[string]interface{}{"invitation_id": invitationID, "project_id": projectID, "role": member.Role, "user_id": userID}, Producer: "project", ProjectID: projectID})
		return err
	})
	return member, err
}

// AcceptInvitationInTransaction consumes an invitation through a transaction
// owned by Auth or Project, preventing account and membership divergence.
func (store PostgresStore) AcceptInvitationInTransaction(ctx context.Context, tx transaction.Tx, tokenHash string, userID string, email string, now time.Time) (Member, error) {
	var invitationID, projectID string
	var invitationRole Role
	if err := tx.QueryRowContext(ctx, `SELECT invitation.invitation_id,invitation.project_id,invitation.role FROM project_invitations AS invitation JOIN projects AS project USING(project_id) WHERE invitation.token_hash=$1 AND LOWER(invitation.email)=LOWER($2) AND invitation.status='pending' AND invitation.expires_at>$3 AND project.deleted_at IS NULL FOR UPDATE OF invitation`, tokenHash, email, now).Scan(&invitationID, &projectID, &invitationRole); err != nil {
		return Member{}, err
	}
	if !isInvitableHumanRole(invitationRole) {
		return Member{}, ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES($1,$2,$3,$4,$4) ON CONFLICT(project_id,user_id) DO NOTHING`, projectID, userID, invitationRole, now); err != nil {
		return Member{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_invitations SET status='accepted',accepted_by=$2,accepted_at=$3,updated_at=$3 WHERE invitation_id=$1`, invitationID, userID, now); err != nil {
		return Member{}, err
	}
	var member Member
	if err := tx.QueryRowContext(ctx, `SELECT u.user_id,u.email,u.display_name,m.role,m.created_at FROM project_members m JOIN auth_users u USING(user_id) WHERE m.project_id=$1 AND m.user_id=$2`, projectID, userID).Scan(&member.UserID, &member.Email, &member.DisplayName, &member.Role, &member.JoinedAt); err != nil {
		return Member{}, err
	}
	if _, err := store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"user_id": userID}, EventType: "project.member.joined", Payload: map[string]interface{}{"invitation_id": invitationID, "project_id": projectID, "role": member.Role, "user_id": userID}, Producer: "project", ProjectID: projectID}); err != nil {
		return Member{}, err
	}
	return member, nil
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

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
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
		       archived_at, deleted_at, purge_at,
		       project.created_at, project.updated_at, member.role
		FROM projects AS project
		JOIN project_members AS member USING (project_id)
		WHERE member.user_id = $1
		  AND project.deleted_at IS NULL
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

func (store PostgresStore) ListTrash(
	ctx context.Context,
	userID string,
	now time.Time,
) ([]Project, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT project_id, name, problem_title, problem_summary,
		       project_constraints, source_artifact_ids, created_by,
		       archived_at, deleted_at, purge_at,
		       project.created_at, project.updated_at, member.role
		FROM projects AS project
		JOIN project_members AS member USING (project_id)
		WHERE member.user_id = $1
		  AND member.role = 'owner'
		  AND project.deleted_at IS NOT NULL
		  AND project.purge_at > $2
		ORDER BY project.deleted_at DESC, project.project_id
	`, userID, now)
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
		       archived_at, deleted_at, purge_at,
		       project.created_at, project.updated_at, member.role
		FROM projects AS project
		JOIN project_members AS member USING (project_id)
		WHERE project.project_id = $1
		  AND member.user_id = $2
		  AND project.deleted_at IS NULL
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
		SELECT member.role
		FROM project_members AS member
		JOIN projects AS project USING (project_id)
		WHERE member.project_id = $1
		  AND member.user_id = $2
		  AND project.deleted_at IS NULL
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
			WHERE project_id = $1 AND deleted_at IS NULL
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

func (store PostgresStore) Trash(
	ctx context.Context,
	actorID string,
	projectID string,
	deletedAt time.Time,
	purgeAt time.Time,
) (Project, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE projects AS project
			SET archived_at = NULL,
			    deleted_at = $3,
			    purge_at = $4,
			    updated_at = $3
			WHERE project.project_id = $1
			  AND project.deleted_at IS NULL
			  AND EXISTS (
			    SELECT 1
			    FROM project_members AS member
			    WHERE member.project_id = project.project_id
			      AND member.user_id = $2
			      AND member.role = 'owner'
			  )
		`, projectID, actorID, deletedAt, purgeAt)
		if err := requireProjectAffected(result, err); err != nil {
			return err
		}
		_, err = store.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": actorID},
			EventType: "project.trashed",
			Payload: map[string]interface{}{
				"project_id": projectID,
				"purge_at":   purgeAt,
			},
			Producer:  "project",
			ProjectID: projectID,
		})
		return err
	})
	if err != nil {
		return Project{}, wrap("trash project", err)
	}
	return store.getTrashed(ctx, actorID, projectID, deletedAt)
}

func (store PostgresStore) Restore(
	ctx context.Context,
	actorID string,
	projectID string,
	now time.Time,
) (Project, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE projects AS project
			SET deleted_at = NULL,
			    purge_at = NULL,
			    updated_at = $3
			WHERE project.project_id = $1
			  AND project.deleted_at IS NOT NULL
			  AND project.purge_at > $3
			  AND EXISTS (
			    SELECT 1
			    FROM project_members AS member
			    WHERE member.project_id = project.project_id
			      AND member.user_id = $2
			      AND member.role = 'owner'
			  )
		`, projectID, actorID, now)
		if err := requireProjectAffected(result, err); err != nil {
			return err
		}
		_, err = store.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": actorID},
			EventType: "project.restored",
			Payload:   map[string]interface{}{"project_id": projectID},
			Producer:  "project",
			ProjectID: projectID,
		})
		return err
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Project{}, ErrNotFound
		}
		return Project{}, wrap("restore project", err)
	}
	return store.Get(ctx, actorID, projectID)
}

func (store PostgresStore) PurgeExpired(ctx context.Context, now time.Time) error {
	_, err := store.DB.ExecContext(ctx, `
		DELETE FROM projects
		WHERE deleted_at IS NOT NULL AND purge_at <= $1
	`, now)
	return wrap("purge expired projects", err)
}

func (store PostgresStore) getTrashed(
	ctx context.Context,
	userID string,
	projectID string,
	now time.Time,
) (Project, error) {
	project, err := scanProject(store.DB.QueryRowContext(ctx, `
		SELECT project.project_id, project.name, project.problem_title,
		       project.problem_summary, project.project_constraints,
		       project.source_artifact_ids, project.created_by,
		       project.archived_at, project.deleted_at, project.purge_at,
		       project.created_at, project.updated_at, member.role
		FROM projects AS project
		JOIN project_members AS member USING (project_id)
		WHERE project.project_id = $1
		  AND member.user_id = $2
		  AND member.role = 'owner'
		  AND project.deleted_at IS NOT NULL
		  AND project.purge_at > $3
	`, projectID, userID, now).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return project, err
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
			WHERE project_members.role <> 'owner'
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

// TransferOwnership atomically promotes an existing member and demotes the
// current owner to maintainer. Regular role updates cannot perform either half
// of this transition independently.
func (store PostgresStore) TransferOwnership(
	ctx context.Context,
	actorID string,
	projectID string,
	userID string,
) (Member, error) {
	now := store.Clock.Now().UTC()
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var actorRole Role
		if err := tx.QueryRowContext(ctx, `
			SELECT role
			FROM project_members
			WHERE project_id = $1 AND user_id = $2
			FOR UPDATE
		`, projectID, actorID).Scan(&actorRole); err != nil {
			return err
		}
		if actorRole != RoleOwner {
			return ErrForbidden
		}
		rows, err := tx.QueryContext(ctx, `
			UPDATE project_members
			SET role = 'maintainer', updated_at = $3
			WHERE project_id = $1 AND role = 'owner' AND user_id <> $2
			RETURNING user_id
		`, projectID, userID, now)
		if err != nil {
			return err
		}
		demotedOwnerIDs := []string{}
		for rows.Next() {
			var ownerID string
			if err := rows.Scan(&ownerID); err != nil {
				rows.Close()
				return err
			}
			demotedOwnerIDs = append(demotedOwnerIDs, ownerID)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(demotedOwnerIDs) == 0 {
			return ErrForbidden
		}
		targetResult, err := tx.ExecContext(ctx, `
			UPDATE project_members
			SET role = 'owner', updated_at = $3
			WHERE project_id = $1 AND user_id = $2 AND role <> 'owner'
		`, projectID, userID, now)
		if err := requireProjectAffected(targetResult, err); err != nil {
			return err
		}
		for _, changedUserID := range append(demotedOwnerIDs, userID) {
			role := RoleMaintainer
			if changedUserID == userID {
				role = RoleOwner
			}
			if _, err := store.Outbox.Write(ctx, tx, outbox.Event{
				Actor:     map[string]string{"user_id": actorID},
				EventType: "project.member.role_changed",
				Payload: map[string]interface{}{
					"project_id": projectID,
					"role":       role,
					"user_id":    changedUserID,
				},
				Producer:  "project",
				ProjectID: projectID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Member{}, wrap("transfer project ownership", err)
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
		&project.DeletedAt,
		&project.PurgeAt,
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
