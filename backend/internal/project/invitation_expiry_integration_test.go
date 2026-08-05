package project

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresInvitationExpiryProcessorConcurrencyAndListPurity(t *testing.T) {
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
	userID := generator.MustNew()
	projectID := generator.MustNew()
	dueA := generator.MustNew()
	dueB := generator.MustNew()
	future := generator.MustNew()
	revoked := generator.MustNew()
	accepted := generator.MustNew()
	now := time.Date(2026, time.August, 6, 6, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM system_outbox WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})

	email := userID + "@invitation-expiry.test"
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES($1,$2,'Invitation Expiry Owner','test','active',$3,$3)`, userID, email, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Invitation Expiry Project',$2,$3,$3)`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES($1,$2,'owner',$3,$3)`, projectID, userID, now); err != nil {
		t.Fatalf("insert owner membership: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_invitations(invitation_id,project_id,email,role,token_hash,status,invited_by,expires_at,revoked_at,created_at,updated_at) VALUES
		($1,$5,'due-a@example.test','viewer',$6,'pending',$9,$10,NULL,$11,$11),
		($2,$5,'due-b@example.test','editor',$7,'pending',$9,$10,NULL,$11,$11),
		($3,$5,'future@example.test','viewer',$8,'pending',$9,$12,NULL,$11,$11),
		($4,$5,'revoked@example.test','viewer',$13,'revoked',$9,$10,$11,$11,$11)
	`, dueA, dueB, future, revoked, projectID, "due-a-"+dueA, "due-b-"+dueB, "future-"+future, userID, now.Add(-time.Minute), now.Add(-time.Hour), now.Add(time.Hour), "revoked-"+revoked); err != nil {
		t.Fatalf("insert invitations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_invitations(invitation_id,project_id,email,role,token_hash,status,invited_by,expires_at,accepted_by,accepted_at,created_at,updated_at)
		VALUES($1,$2,'accepted@example.test','viewer',$3,'accepted',$4,$5,$4,$6,$7,$7)
	`, accepted, projectID, "accepted-"+accepted, userID, now.Add(-time.Minute), now.Add(-30*time.Minute), now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert accepted invitation: %v", err)
	}

	store := PostgresStore{
		Clock:       clock.Fixed{Time: now},
		DB:          db,
		Generator:   generator,
		Outbox:      outbox.Writer{Clock: clock.Fixed{Time: now}, Generator: generator},
		Transaction: transaction.Manager{DB: transaction.SQLBeginner{DB: db}},
	}
	items, err := store.ListInvitations(ctx, projectID, now)
	if err != nil || len(items) != 5 {
		t.Fatalf("list invitations: items=%d err=%v", len(items), err)
	}
	assertInvitationExpiryStatus(t, ctx, db, dueA, "pending")
	assertInvitationExpiryStatus(t, ctx, db, dueB, "pending")
	assertInvitationExpiryEvents(t, ctx, db, projectID, dueA, 0)
	assertInvitationExpiryEvents(t, ctx, db, projectID, dueB, 0)

	start := make(chan struct{})
	counts := make(chan int, 2)
	errorsCh := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			count, processErr := (InvitationExpiryProcessor{
				BatchSize: 1,
				Clock:     clock.Fixed{Time: now},
				Store:     store,
			}).RunBatch(ctx)
			counts <- count
			errorsCh <- processErr
		}()
	}
	close(start)
	wait.Wait()
	close(counts)
	close(errorsCh)
	for processErr := range errorsCh {
		if processErr != nil {
			t.Fatalf("concurrent expiry processor: %v", processErr)
		}
	}
	total := 0
	for count := range counts {
		total += count
	}
	if total != 2 {
		t.Fatalf("concurrent expired count: got %d, want 2", total)
	}

	assertInvitationExpiryStatus(t, ctx, db, dueA, "expired")
	assertInvitationExpiryStatus(t, ctx, db, dueB, "expired")
	assertInvitationExpiryStatus(t, ctx, db, future, "pending")
	assertInvitationExpiryStatus(t, ctx, db, revoked, "revoked")
	assertInvitationExpiryStatus(t, ctx, db, accepted, "accepted")
	assertInvitationExpiryEvents(t, ctx, db, projectID, dueA, 1)
	assertInvitationExpiryEvents(t, ctx, db, projectID, dueB, 1)
	assertInvitationExpiryEvents(t, ctx, db, projectID, future, 0)
	assertInvitationExpiryEvents(t, ctx, db, projectID, revoked, 0)
	assertInvitationExpiryEvents(t, ctx, db, projectID, accepted, 0)

	restarted, err := (InvitationExpiryProcessor{
		BatchSize: 10,
		Clock:     clock.Fixed{Time: now},
		Store:     store,
	}).RunBatch(ctx)
	if err != nil || restarted != 0 {
		t.Fatalf("restart expiry scan: processed=%d err=%v", restarted, err)
	}
	assertInvitationExpiryEvents(t, ctx, db, projectID, dueA, 1)
	assertInvitationExpiryEvents(t, ctx, db, projectID, dueB, 1)

	reissuedInvitationID := generator.MustNew()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_invitations(invitation_id,project_id,email,role,token_hash,status,invited_by,expires_at,created_at,updated_at)
		VALUES($1,$2,'reissue@example.test','viewer',$3,'pending',$4,$5,$6,$6)
	`, reissuedInvitationID, projectID, "reissue-old-"+reissuedInvitationID, userID, now.Add(-time.Minute), now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert invitation for reissue: %v", err)
	}
	issued, err := store.CreateInvitation(ctx, userID, projectID, "reissue@example.test", RoleViewer, now.Add(time.Hour))
	if err != nil || issued.Invitation.Status != "pending" {
		t.Fatalf("reissue expired invitation: invitation=%#v err=%v", issued.Invitation, err)
	}
	assertInvitationExpiryStatus(t, ctx, db, reissuedInvitationID, "expired")
	assertInvitationExpiryEvents(t, ctx, db, projectID, reissuedInvitationID, 1)
}

func assertInvitationExpiryStatus(t *testing.T, ctx context.Context, db *sql.DB, invitationID, want string) {
	t.Helper()
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM project_invitations WHERE invitation_id=$1`, invitationID).Scan(&status); err != nil {
		t.Fatalf("read invitation status: %v", err)
	}
	if status != want {
		t.Fatalf("invitation %s status: got %q, want %q", invitationID, status, want)
	}
}

func assertInvitationExpiryEvents(t *testing.T, ctx context.Context, db *sql.DB, projectID, invitationID string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM system_outbox
		WHERE event_type='project.invitation.expired'
		  AND schema_version=1
		  AND producer='project'
		  AND project_id=$1
		  AND payload->>'project_id'=$1
		  AND payload->>'invitation_id'=$2
	`, projectID, invitationID).Scan(&count); err != nil {
		t.Fatalf("count invitation expiry events: %v", err)
	}
	if count != want {
		t.Fatalf("invitation %s expiry events: got %d, want %d", invitationID, count, want)
	}
}
