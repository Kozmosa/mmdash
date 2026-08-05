package notification

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
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresRuleJSONBReadWrite(t *testing.T) {
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	generator := identity.Generator{}
	userID := generator.MustNew()
	projectID := generator.MustNew()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at)
		VALUES($1,$2,'Notification Rule Test','test','active',$3,$3)
	`, userID, userID+"@notification.test", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,created_at,updated_at)
		VALUES($1,'Notification Rule Test',$2,$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})

	store := PostgresStore{
		Clock:     clock.Fixed{Time: now},
		DB:        db,
		Generator: generator,
	}
	want := Rule{
		ProjectID:       projectID,
		TypeKey:         TypeReminderDue,
		InboxEnabled:    true,
		ExternalEnabled: true,
		ChannelKeys:     []string{"notification.generic_webhook", "notification.feishu_webhook"},
		MinimumPriority: "high",
		UpdatedBy:       userID,
	}
	got, err := store.UpsertRule(ctx, want)
	if err != nil {
		t.Fatalf("upsert rule: %v", err)
	}
	if len(got.ChannelKeys) != 2 || got.ChannelKeys[0] != want.ChannelKeys[0] || got.ChannelKeys[1] != want.ChannelKeys[1] {
		t.Fatalf("upsert channel keys: got %#v", got.ChannelKeys)
	}

	loaded, err := store.GetRule(ctx, projectID, TypeReminderDue)
	if err != nil {
		t.Fatalf("get rule: %v", err)
	}
	if len(loaded.ChannelKeys) != 2 || loaded.ChannelKeys[0] != want.ChannelKeys[0] || loaded.ChannelKeys[1] != want.ChannelKeys[1] {
		t.Fatalf("loaded channel keys: got %#v", loaded.ChannelKeys)
	}
	if _, err := store.UpsertRule(ctx, want); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale rule update: got %v", err)
	}
	want.Version = loaded.Version
	want.ChannelKeys = []string{"notification.generic_webhook"}
	updated, err := store.UpsertRule(ctx, want)
	if err != nil {
		t.Fatalf("versioned rule update: %v", err)
	}
	if updated.Version != loaded.Version+1 || len(updated.ChannelKeys) != 1 {
		t.Fatalf("versioned rule result: %#v", updated)
	}
	var columnType string
	if err := db.QueryRowContext(ctx, `SELECT pg_typeof(channel_keys)::text FROM notification_rules WHERE project_id=$1 AND type_key=$2`, projectID, TypeReminderDue).Scan(&columnType); err != nil {
		t.Fatalf("inspect channel key type: %v", err)
	}
	if columnType != "jsonb" {
		t.Fatalf("channel_keys type: got %s want jsonb", columnType)
	}
}

func TestPostgresInvitationActionAndOutcomeCancelPendingDelivery(t *testing.T) {
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	generator := identity.Generator{}
	userID := generator.MustNew()
	projectID := generator.MustNew()
	invitationID := generator.MustNew()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at)
		VALUES($1,$2,'Notification Action Test','test','active',$3,$3)
	`, userID, userID+"@notification-action.test", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,created_at,updated_at)
		VALUES($1,'Notification Action Test',$2,$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})

	store := PostgresStore{
		Clock:       clock.Fixed{Time: now},
		DB:          db,
		Generator:   generator,
		Transaction: transaction.Manager{DB: transaction.SQLBeginner{DB: db}},
	}
	notificationID := generator.MustNew()
	if err := store.CreateEvent(ctx, Notification{
		ID:              notificationID,
		TypeKey:         TypeInvitationReceived,
		TemplateVersion: 1,
		SourceEventID:   generator.MustNew(),
		ProjectID:       projectID,
		ResourceType:    "invitation",
		ResourceID:      invitationID,
		Priority:        "high",
		Data:            map[string]interface{}{"invitation_id": invitationID},
		Action:          &Action{Type: "project.invitation.accept", ResourceID: invitationID, Route: "/inbox"},
		OccurredAt:      now,
		CreatedAt:       now,
	}, []RecipientInput{{Key: "user:" + userID, UserID: userID}}, true, []DeliveryIntent{{ChannelKey: "notification.generic_webhook", RuleVersion: 3, SettingsVersion: 4}}); err != nil {
		t.Fatalf("create invitation notification: %v", err)
	}
	page, err := store.ListInbox(ctx, userID, Filter{}, pagination.Request{Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list invitation inbox: items=%d err=%v", len(page.Items), err)
	}
	if page.Items[0].Notification.Action == nil || page.Items[0].Notification.Action.ResourceID != invitationID {
		t.Fatalf("invitation action was not persisted: %#v", page.Items[0].Notification.Action)
	}
	if err := store.ApplyInvitationOutcome(ctx, InvitationOutcome{
		InvitationID:  invitationID,
		ProjectID:     projectID,
		Outcome:       OutcomeRevoked,
		SourceEventID: generator.MustNew(),
		OccurredAt:    now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("apply invitation outcome: %v", err)
	}
	var outcome, deliveryStatus string
	if err := db.QueryRowContext(ctx, `SELECT outcome FROM notification_inbox_items WHERE notification_id=$1`, notificationID).Scan(&outcome); err != nil {
		t.Fatalf("read invitation outcome: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM notification_deliveries WHERE notification_id=$1`, notificationID).Scan(&deliveryStatus); err != nil {
		t.Fatalf("read invitation delivery: %v", err)
	}
	if outcome != OutcomeRevoked || deliveryStatus != "cancelled" {
		t.Fatalf("invitation terminal state: outcome=%s delivery=%s", outcome, deliveryStatus)
	}
}

func TestPostgresInvitationOutcomeOrderingReplayAndIsolation(t *testing.T) {
	fixture := newInvitationStoreFixture(t)

	outcomeFirstInvitationID := fixture.generator.MustNew()
	outcomeFirst := InvitationOutcome{
		InvitationID:  outcomeFirstInvitationID,
		ProjectID:     fixture.projectID,
		Outcome:       OutcomeRevoked,
		SourceEventID: fixture.generator.MustNew(),
		OccurredAt:    fixture.now.Add(time.Minute),
	}
	if err := fixture.store.ApplyInvitationOutcome(fixture.ctx, outcomeFirst); err != nil {
		t.Fatalf("apply outcome before invitation: %v", err)
	}
	if err := fixture.store.ApplyInvitationOutcome(fixture.ctx, outcomeFirst); err != nil {
		t.Fatalf("replay outcome before invitation: %v", err)
	}
	outcomeFirstNotificationID := createInvitationNotification(t, fixture, outcomeFirstInvitationID, fixture.now)
	assertInvitationState(t, fixture, outcomeFirstNotificationID, OutcomeRevoked, "cancelled")

	olderConflictingOutcome := outcomeFirst
	olderConflictingOutcome.Outcome = OutcomeResolved
	olderConflictingOutcome.SourceEventID = fixture.generator.MustNew()
	olderConflictingOutcome.OccurredAt = fixture.now.Add(-time.Minute)
	if err := fixture.store.ApplyInvitationOutcome(fixture.ctx, olderConflictingOutcome); err != nil {
		t.Fatalf("replay older conflicting outcome: %v", err)
	}
	assertInvitationState(t, fixture, outcomeFirstNotificationID, OutcomeRevoked, "cancelled")

	resolvedFirstInvitationID := fixture.generator.MustNew()
	resolvedFirst := InvitationOutcome{
		InvitationID:  resolvedFirstInvitationID,
		ProjectID:     fixture.projectID,
		Outcome:       OutcomeResolved,
		SourceEventID: fixture.generator.MustNew(),
		OccurredAt:    fixture.now.Add(2 * time.Minute),
	}
	if err := fixture.store.ApplyInvitationOutcome(fixture.ctx, resolvedFirst); err != nil {
		t.Fatalf("apply resolved outcome before invitation: %v", err)
	}
	resolvedFirstNotificationID := createInvitationNotification(t, fixture, resolvedFirstInvitationID, fixture.now.Add(time.Minute))
	assertInvitationState(t, fixture, resolvedFirstNotificationID, OutcomeResolved, "pending")

	invitationFirstID := fixture.generator.MustNew()
	invitationFirstNotificationID := createInvitationNotification(t, fixture, invitationFirstID, fixture.now.Add(3*time.Minute))
	assertInvitationState(t, fixture, invitationFirstNotificationID, OutcomeActive, "pending")
	invitationFirst := InvitationOutcome{
		InvitationID:  invitationFirstID,
		ProjectID:     fixture.projectID,
		Outcome:       OutcomeExpired,
		SourceEventID: fixture.generator.MustNew(),
		OccurredAt:    fixture.now.Add(4 * time.Minute),
	}
	if err := fixture.store.ApplyInvitationOutcome(fixture.ctx, invitationFirst); err != nil {
		t.Fatalf("apply outcome after invitation: %v", err)
	}
	if err := fixture.store.ApplyInvitationOutcome(fixture.ctx, invitationFirst); err != nil {
		t.Fatalf("replay outcome after invitation: %v", err)
	}
	assertInvitationState(t, fixture, invitationFirstNotificationID, OutcomeExpired, "cancelled")

	unrelatedInvitationID := fixture.generator.MustNew()
	unrelatedNotificationID := createInvitationNotification(t, fixture, unrelatedInvitationID, fixture.now.Add(5*time.Minute))
	assertInvitationState(t, fixture, unrelatedNotificationID, OutcomeActive, "pending")

	var lifecycleRows int
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT COUNT(*) FROM notification_invitation_outcomes WHERE project_id=$1`, fixture.projectID).Scan(&lifecycleRows); err != nil {
		t.Fatalf("count invitation lifecycle rows: %v", err)
	}
	if lifecycleRows != 4 {
		t.Fatalf("invitation lifecycle rows: got %d, want 4", lifecycleRows)
	}
}

func TestPostgresInvitationOutcomeSerializesWithConcurrentCreation(t *testing.T) {
	t.Run("outcome transaction commits first", func(t *testing.T) {
		fixture := newInvitationStoreFixture(t)
		invitationID := fixture.generator.MustNew()
		fact := InvitationOutcome{
			InvitationID:  invitationID,
			ProjectID:     fixture.projectID,
			Outcome:       OutcomeRevoked,
			SourceEventID: fixture.generator.MustNew(),
			OccurredAt:    fixture.now.Add(time.Minute),
		}
		tx, err := fixture.db.BeginTx(fixture.ctx, nil)
		if err != nil {
			t.Fatalf("begin outcome transaction: %v", err)
		}
		if _, err = fixture.store.applyInvitationOutcomeTx(fixture.ctx, tx, fact, fixture.now); err != nil {
			_ = tx.Rollback()
			t.Fatalf("apply uncommitted outcome: %v", err)
		}
		notification, recipients, deliveries := invitationNotification(fixture, invitationID, fixture.now)
		done := make(chan error, 1)
		go func() {
			done <- fixture.store.CreateEvent(fixture.ctx, notification, recipients, true, deliveries)
		}()
		assertInvitationOperationWaits(t, done)
		if err = tx.Commit(); err != nil {
			t.Fatalf("commit outcome transaction: %v", err)
		}
		awaitInvitationOperation(t, done)
		assertInvitationState(t, fixture, notification.ID, OutcomeRevoked, "cancelled")
	})

	t.Run("invitation transaction commits first", func(t *testing.T) {
		fixture := newInvitationStoreFixture(t)
		invitationID := fixture.generator.MustNew()
		notification, recipients, deliveries := invitationNotification(fixture, invitationID, fixture.now)
		tx, err := fixture.db.BeginTx(fixture.ctx, nil)
		if err != nil {
			t.Fatalf("begin invitation transaction: %v", err)
		}
		if err = fixture.store.createEventTx(fixture.ctx, tx, notification, notification.Data, notification.RenderedSnapshot, recipients, true, deliveries, fixture.now); err != nil {
			_ = tx.Rollback()
			t.Fatalf("create uncommitted invitation: %v", err)
		}
		fact := InvitationOutcome{
			InvitationID:  invitationID,
			ProjectID:     fixture.projectID,
			Outcome:       OutcomeExpired,
			SourceEventID: fixture.generator.MustNew(),
			OccurredAt:    fixture.now.Add(time.Minute),
		}
		done := make(chan error, 1)
		go func() {
			done <- fixture.store.ApplyInvitationOutcome(fixture.ctx, fact)
		}()
		assertInvitationOperationWaits(t, done)
		if err = tx.Commit(); err != nil {
			t.Fatalf("commit invitation transaction: %v", err)
		}
		awaitInvitationOperation(t, done)
		assertInvitationState(t, fixture, notification.ID, OutcomeExpired, "cancelled")
	})
}

type invitationStoreFixture struct {
	ctx       context.Context
	db        *sql.DB
	generator identity.Generator
	now       time.Time
	projectID string
	store     PostgresStore
	userID    string
}

func newInvitationStoreFixture(t *testing.T) invitationStoreFixture {
	t.Helper()
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	generator := identity.Generator{}
	userID := generator.MustNew()
	projectID := generator.MustNew()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at)
		VALUES($1,$2,'Notification Lifecycle Test','test','active',$3,$3)
	`, userID, userID+"@notification-lifecycle.test", now); err != nil {
		t.Fatalf("insert lifecycle user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,created_at,updated_at)
		VALUES($1,'Notification Lifecycle Test',$2,$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert lifecycle project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})
	store := PostgresStore{
		Clock:       clock.Fixed{Time: now},
		DB:          db,
		Generator:   generator,
		Transaction: transaction.Manager{DB: transaction.SQLBeginner{DB: db}},
	}
	return invitationStoreFixture{ctx: ctx, db: db, generator: generator, now: now, projectID: projectID, store: store, userID: userID}
}

func invitationNotification(fixture invitationStoreFixture, invitationID string, occurredAt time.Time) (Notification, []RecipientInput, []DeliveryIntent) {
	data := map[string]interface{}{"invitation_id": invitationID}
	notification := Notification{
		ID:               fixture.generator.MustNew(),
		TypeKey:          TypeInvitationReceived,
		TemplateVersion:  1,
		SourceEventID:    fixture.generator.MustNew(),
		ProjectID:        fixture.projectID,
		ResourceType:     "invitation",
		ResourceID:       invitationID,
		Priority:         "high",
		Data:             data,
		RenderedSnapshot: data,
		Action:           &Action{Type: "project.invitation.accept", ResourceID: invitationID, Route: "/inbox"},
		OccurredAt:       occurredAt,
		CreatedAt:        occurredAt,
	}
	recipients := []RecipientInput{{Key: "user:" + fixture.userID, UserID: fixture.userID}}
	deliveries := []DeliveryIntent{{ChannelKey: "notification.generic_webhook", RuleVersion: 1, SettingsVersion: 1}}
	return notification, recipients, deliveries
}

func createInvitationNotification(t *testing.T, fixture invitationStoreFixture, invitationID string, occurredAt time.Time) string {
	t.Helper()
	notification, recipients, deliveries := invitationNotification(fixture, invitationID, occurredAt)
	if err := fixture.store.CreateEvent(fixture.ctx, notification, recipients, true, deliveries); err != nil {
		t.Fatalf("create invitation notification: %v", err)
	}
	return notification.ID
}

func assertInvitationState(t *testing.T, fixture invitationStoreFixture, notificationID, wantOutcome, wantDeliveryStatus string) {
	t.Helper()
	var outcome, deliveryStatus string
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT outcome FROM notification_inbox_items WHERE notification_id=$1`, notificationID).Scan(&outcome); err != nil {
		t.Fatalf("read invitation outcome: %v", err)
	}
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT status FROM notification_deliveries WHERE notification_id=$1`, notificationID).Scan(&deliveryStatus); err != nil {
		t.Fatalf("read invitation delivery: %v", err)
	}
	if outcome != wantOutcome || deliveryStatus != wantDeliveryStatus {
		t.Fatalf("invitation state: outcome=%s delivery=%s, want outcome=%s delivery=%s", outcome, deliveryStatus, wantOutcome, wantDeliveryStatus)
	}
}

func assertInvitationOperationWaits(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("concurrent invitation operation completed before lifecycle transaction committed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func awaitInvitationOperation(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("concurrent invitation operation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent invitation operation")
	}
}
