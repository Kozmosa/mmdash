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
	if err := store.ApplyInvitationOutcome(ctx, invitationID, OutcomeRevoked); err != nil {
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
