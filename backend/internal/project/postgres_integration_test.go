package project

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestAcceptInvitationByIDRejectsDeletedProjects(t *testing.T) {
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	generator := identity.Generator{}
	ownerID := generator.MustNew()
	deletedInviteeID := generator.MustNew()
	activeInviteeID := generator.MustNew()
	deletedProjectID := generator.MustNew()
	activeProjectID := generator.MustNew()
	deletedInvitationID := generator.MustNew()
	activeInvitationID := generator.MustNew()
	now := time.Date(2026, time.August, 6, 4, 0, 0, 0, time.UTC)
	deletedAt := now.Add(-time.Hour)
	purgeAt := now.Add(30 * 24 * time.Hour)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM system_outbox WHERE project_id IN ($1,$2)`, deletedProjectID, activeProjectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM projects WHERE project_id IN ($1,$2)`, deletedProjectID, activeProjectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE user_id IN ($1,$2,$3)`, ownerID, deletedInviteeID, activeInviteeID)
	})

	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES
		($1,$4,'Invitation Owner','test','active',$7,$7),
		($2,$5,'Deleted Project Invitee','test','active',$7,$7),
		($3,$6,'Active Project Invitee','test','active',$7,$7)
	`, ownerID, deletedInviteeID, activeInviteeID, ownerID+"@invitation-project.test", deletedInviteeID+"@invitation-project.test", activeInviteeID+"@invitation-project.test", now); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,deleted_at,purge_at,created_at,updated_at) VALUES
		($1,'Deleted Invitation Project',$3,$4,$5,$6,$6),
		($2,'Active Invitation Project',$3,NULL,NULL,$6,$6)
	`, deletedProjectID, activeProjectID, ownerID, deletedAt, purgeAt, now); err != nil {
		t.Fatalf("insert projects: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES
		($1,$3,'owner',$4,$4),
		($2,$3,'owner',$4,$4)
	`, deletedProjectID, activeProjectID, ownerID, now); err != nil {
		t.Fatalf("insert owner memberships: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_invitations(invitation_id,project_id,email,role,token_hash,status,invited_by,expires_at,created_at,updated_at) VALUES
		($1,$3,$5,'viewer',$7,'pending',$9,$10,$11,$11),
		($2,$4,$6,'editor',$8,'pending',$9,$10,$11,$11)
	`, deletedInvitationID, activeInvitationID, deletedProjectID, activeProjectID, deletedInviteeID+"@invitation-project.test", activeInviteeID+"@invitation-project.test", "deleted-"+deletedInvitationID, "active-"+activeInvitationID, ownerID, now.Add(time.Hour), now); err != nil {
		t.Fatalf("insert invitations: %v", err)
	}
	store := PostgresStore{
		Clock:       clock.Fixed{Time: now},
		DB:          db,
		Generator:   generator,
		Outbox:      outbox.Writer{Clock: clock.Fixed{Time: now}, Generator: generator},
		Transaction: transaction.Manager{DB: transaction.SQLBeginner{DB: db}},
	}
	deletedEmail := deletedInviteeID + "@invitation-project.test"
	if _, err := store.AcceptInvitationByID(ctx, deletedInvitationID, deletedInviteeID, deletedEmail, now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("accept deleted project invitation: got %v", err)
	}
	assertInvitationAcceptState(t, ctx, db, deletedProjectID, deletedInviteeID, deletedInvitationID, "pending", 0, 0)

	activeEmail := activeInviteeID + "@invitation-project.test"
	member, err := store.AcceptInvitationByID(ctx, activeInvitationID, activeInviteeID, activeEmail, now)
	if err != nil {
		t.Fatalf("accept active project invitation: %v", err)
	}
	if member.UserID != activeInviteeID || member.Role != RoleEditor {
		t.Fatalf("accepted member: %#v", member)
	}
	assertInvitationAcceptState(t, ctx, db, activeProjectID, activeInviteeID, activeInvitationID, "accepted", 1, 1)

	if _, err := store.AcceptInvitationByID(ctx, activeInvitationID, activeInviteeID, activeEmail, now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("accept invitation twice: got %v", err)
	}
	assertInvitationAcceptState(t, ctx, db, activeProjectID, activeInviteeID, activeInvitationID, "accepted", 1, 1)
}

func assertInvitationAcceptState(t *testing.T, ctx context.Context, db *sql.DB, projectID, userID, invitationID, wantStatus string, wantMemberships, wantEvents int) {
	t.Helper()
	var memberships int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, userID).Scan(&memberships); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if memberships != wantMemberships {
		t.Fatalf("memberships: got %d, want %d", memberships, wantMemberships)
	}
	var status string
	var acceptedBy sql.NullString
	var acceptedAt sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT status,accepted_by::text,accepted_at FROM project_invitations WHERE invitation_id=$1`, invitationID).Scan(&status, &acceptedBy, &acceptedAt); err != nil {
		t.Fatalf("read invitation: %v", err)
	}
	if status != wantStatus {
		t.Fatalf("invitation status: got %q, want %q", status, wantStatus)
	}
	if wantStatus == "pending" && (acceptedBy.Valid || acceptedAt.Valid) {
		t.Fatalf("pending invitation was mutated: accepted_by=%v accepted_at=%v", acceptedBy, acceptedAt)
	}
	var events int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_outbox WHERE project_id=$1 AND event_type='project.member.joined' AND payload->>'invitation_id'=$2`, projectID, invitationID).Scan(&events); err != nil {
		t.Fatalf("count joined events: %v", err)
	}
	if events != wantEvents {
		t.Fatalf("joined events: got %d, want %d", events, wantEvents)
	}
}
