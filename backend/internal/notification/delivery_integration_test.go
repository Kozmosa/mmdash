package notification

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresDeliveryStateMachinePersistsAttempts(t *testing.T) {
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
		VALUES($1,$2,'Notification Delivery Test','test','active',$3,$3)
	`, userID, userID+"@delivery.test", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,created_at,updated_at)
		VALUES($1,'Notification Delivery Test',$2,$3,$3)
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

	completeID := createDeliveryTestEvent(t, ctx, store, projectID, userID, "complete")
	claimed, notification, err := store.ClaimDelivery(ctx, "delivery-test-owner", time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim complete delivery: delivery=%#v err=%v", claimed, err)
	}
	if notification.TypeKey != TypeReminderDue || claimed.Status != "sending" || claimed.Attempts != 1 {
		t.Fatalf("claim state: delivery=%#v notification=%#v", claimed, notification)
	}
	if err := store.CompleteDelivery(ctx, completeID, "delivery-test-owner"); err != nil {
		t.Fatalf("complete delivery: %v", err)
	}
	assertDeliveryAttempt(t, ctx, db, completeID, "delivered", "", 0)

	retryID := createDeliveryTestEvent(t, ctx, store, projectID, userID, "retry")
	claimed, _, err = store.ClaimDelivery(ctx, "delivery-test-owner", time.Minute)
	if err != nil || claimed == nil || claimed.ID != retryID {
		t.Fatalf("claim retry delivery: delivery=%#v err=%v", claimed, err)
	}
	longMessage := strings.Repeat("provider detail ", 100)
	if err := store.FailDelivery(ctx, retryID, "delivery-test-owner", "provider_retryable", 503, longMessage, true, 30*time.Second); err != nil {
		t.Fatalf("fail retryable delivery: %v", err)
	}
	assertDeliveryAttempt(t, ctx, db, retryID, "retrying", "provider_retryable", 503)
	var status, lastError string
	if err := db.QueryRowContext(ctx, `SELECT status,last_error_message FROM notification_deliveries WHERE delivery_id=$1`, retryID).Scan(&status, &lastError); err != nil {
		t.Fatalf("read retryable delivery: %v", err)
	}
	if status != "retrying" || len(lastError) != 500 {
		t.Fatalf("retryable delivery state: status=%s error length=%d", status, len(lastError))
	}

	cancelID := createDeliveryTestEvent(t, ctx, store, projectID, userID, "cancel")
	if err := store.CancelPending(ctx, projectID, "notification.generic_webhook"); err != nil {
		t.Fatalf("cancel pending deliveries: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM notification_deliveries WHERE delivery_id=$1`, cancelID).Scan(&status); err != nil {
		t.Fatalf("read cancelled delivery: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("cancelled delivery status: got %s", status)
	}
	if _, _, err := store.ClaimDelivery(ctx, "delivery-test-owner", time.Minute); err != nil {
		// The completed and retrying rows are not immediately claimable; this
		// assertion intentionally exercises the no-work path without requiring
		// a provider call.
		t.Fatalf("claim after cancellation: %v", err)
	}
}

func createDeliveryTestEvent(t *testing.T, ctx context.Context, store PostgresStore, projectID, userID, suffix string) string {
	t.Helper()
	notificationID := store.Generator.MustNew()
	now := store.now()
	if err := store.CreateEvent(ctx, Notification{
		ID:              notificationID,
		TypeKey:         TypeReminderDue,
		TemplateVersion: 1,
		SourceEventID:   store.Generator.MustNew(),
		ProjectID:       projectID,
		ActorID:         userID,
		ResourceType:    "reminder",
		ResourceID:      store.Generator.MustNew(),
		Priority:        "normal",
		Data:            map[string]interface{}{"reminder_id": suffix},
		OccurredAt:      now,
		CreatedAt:       now,
	}, []RecipientInput{{Key: "user:" + userID, UserID: userID}}, false, []DeliveryIntent{{ChannelKey: "notification.generic_webhook", SettingsVersion: 2}}); err != nil {
		t.Fatalf("create delivery event %s: %v", suffix, err)
	}
	var deliveryID string
	if err := store.DB.QueryRowContext(ctx, `SELECT delivery_id FROM notification_deliveries WHERE notification_id=$1`, notificationID).Scan(&deliveryID); err != nil {
		t.Fatalf("find delivery %s: %v", suffix, err)
	}
	return deliveryID
}

func assertDeliveryAttempt(t *testing.T, ctx context.Context, db *sql.DB, deliveryID, wantOutcome, wantCode string, wantStatus int) {
	t.Helper()
	var outcome, code string
	var providerStatus sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT outcome,error_code,provider_status FROM notification_delivery_attempts WHERE delivery_id=$1`, deliveryID).Scan(&outcome, &code, &providerStatus); err != nil {
		t.Fatalf("read delivery attempt: %v", err)
	}
	if outcome != wantOutcome || code != wantCode || (wantStatus == 0 && providerStatus.Valid) || (wantStatus != 0 && (!providerStatus.Valid || providerStatus.Int64 != int64(wantStatus))) {
		t.Fatalf("delivery attempt: outcome=%s code=%s provider_status=%#v", outcome, code, providerStatus)
	}
}
