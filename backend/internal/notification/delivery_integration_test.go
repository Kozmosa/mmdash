package notification

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
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
	wantProviderResult := ProviderSendResult{ProviderMessageID: "provider-message-complete", ResponseSummary: "http_status=200; request_id=request-complete"}
	if err := store.CompleteDelivery(ctx, completeID, "delivery-test-owner", wantProviderResult); err != nil {
		t.Fatalf("complete delivery: %v", err)
	}
	assertDeliveryAttempt(t, ctx, db, completeID, "delivered", "", 0)
	assertPersistedDeliveryResult(t, ctx, db, completeID, wantProviderResult, "delivered")
	page, err := store.ListDeliveries(ctx, projectID, "", pagination.Request{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != completeID || page.Items[0].ProviderMessage != wantProviderResult.ProviderMessageID || page.Items[0].ResponseSummary != wantProviderResult.ResponseSummary {
		t.Fatalf("list completed delivery result: page=%#v err=%v", page, err)
	}

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

func TestPostgresCompleteDeliveryResultSafetyAndLeaseOwnership(t *testing.T) {
	fixture := newDeliveryStoreFixture(t)
	deliveryID := createDeliveryTestEvent(t, fixture.ctx, fixture.store, fixture.projectID, fixture.userID, "provider-result-safety")
	claimed, _, err := fixture.store.ClaimDelivery(fixture.ctx, "result-owner", time.Minute)
	if err != nil || claimed == nil || claimed.ID != deliveryID {
		t.Fatalf("claim provider result delivery: delivery=%#v err=%v", claimed, err)
	}

	unsafe := ProviderSendResult{
		ProviderMessageID: strings.Repeat("p", providerResultMaxRunes+25),
		ResponseSummary: "http_status=200 Authorization: Bearer authorization-secret token=token-secret " +
			"https://user:password@example.test/callback?access_token=query-secret#fragment person@example.test " +
			strings.Repeat("界", providerResultMaxRunes),
	}
	if err := fixture.store.CompleteDelivery(fixture.ctx, deliveryID, "wrong-owner", unsafe); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong owner completion: got %v, want %v", err, ErrNotFound)
	}
	assertPersistedDeliveryResult(t, fixture.ctx, fixture.db, deliveryID, ProviderSendResult{}, "sending")

	want := sanitizeProviderResult(unsafe)
	if err := fixture.store.CompleteDelivery(fixture.ctx, deliveryID, "result-owner", unsafe); err != nil {
		t.Fatalf("complete provider result delivery: %v", err)
	}
	assertPersistedDeliveryResult(t, fixture.ctx, fixture.db, deliveryID, want, "delivered")
	if len([]rune(want.ProviderMessageID)) != providerResultMaxRunes || len([]rune(want.ResponseSummary)) != providerResultMaxRunes {
		t.Fatalf("provider result limits: %#v", want)
	}
	for _, forbidden := range []string{"authorization-secret", "token-secret", "query-secret", "password", "fragment", "person@example.test"} {
		if strings.Contains(want.ResponseSummary, forbidden) {
			t.Fatalf("persisted provider result leaked %q: %q", forbidden, want.ResponseSummary)
		}
	}

	if err := fixture.store.CompleteDelivery(fixture.ctx, deliveryID, "result-owner", ProviderSendResult{ProviderMessageID: "overwritten", ResponseSummary: "overwritten"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeat completion: got %v, want %v", err, ErrNotFound)
	}
	assertPersistedDeliveryResult(t, fixture.ctx, fixture.db, deliveryID, want, "delivered")

	emptyID := createDeliveryTestEvent(t, fixture.ctx, fixture.store, fixture.projectID, fixture.userID, "provider-result-empty")
	claimed, _, err = fixture.store.ClaimDelivery(fixture.ctx, "empty-result-owner", time.Minute)
	if err != nil || claimed == nil || claimed.ID != emptyID {
		t.Fatalf("claim empty provider result delivery: delivery=%#v err=%v", claimed, err)
	}
	if err := fixture.store.CompleteDelivery(fixture.ctx, emptyID, "empty-result-owner", ProviderSendResult{}); err != nil {
		t.Fatalf("complete empty provider result delivery: %v", err)
	}
	assertPersistedDeliveryResult(t, fixture.ctx, fixture.db, emptyID, ProviderSendResult{}, "delivered")
}

func TestPostgresManualRetryRequiresFailedAndIsConcurrentSafe(t *testing.T) {
	fixture := newDeliveryStoreFixture(t)

	failedID := createDeliveryTestEvent(t, fixture.ctx, fixture.store, fixture.projectID, fixture.userID, "manual-failed")
	setDeliveryStatus(t, fixture, failedID, "failed")
	first, err := fixture.store.CreateRetry(fixture.ctx, fixture.projectID, failedID, "first operator retry")
	if err != nil {
		t.Fatalf("retry failed delivery: %v", err)
	}
	second, err := fixture.store.CreateRetry(fixture.ctx, fixture.projectID, failedID, "replayed operator retry")
	if err != nil {
		t.Fatalf("replay failed delivery retry: %v", err)
	}
	if first.ID == "" || second.ID != first.ID || first.DeliveryKey != "retry:"+failedID || second.Status != "pending" {
		t.Fatalf("idempotent retry result: first=%#v second=%#v", first, second)
	}
	assertManualRetryCount(t, fixture, failedID, 1)

	for _, status := range []string{"pending", "sending", "delivered", "retrying", "cancelled"} {
		t.Run("reject "+status, func(t *testing.T) {
			deliveryID := createDeliveryTestEvent(t, fixture.ctx, fixture.store, fixture.projectID, fixture.userID, "manual-"+status)
			setDeliveryStatus(t, fixture, deliveryID, status)
			if _, err := fixture.store.CreateRetry(fixture.ctx, fixture.projectID, deliveryID, "invalid state retry"); !errors.Is(err, ErrDeliveryRetryConflict) {
				t.Fatalf("retry %s delivery: got %v, want %v", status, err, ErrDeliveryRetryConflict)
			}
			assertManualRetryCount(t, fixture, deliveryID, 0)
		})
	}

	missingID := fixture.store.Generator.MustNew()
	if _, err := fixture.store.CreateRetry(fixture.ctx, fixture.projectID, missingID, "missing delivery retry"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retry missing delivery: got %v, want %v", err, ErrNotFound)
	}

	concurrentID := createDeliveryTestEvent(t, fixture.ctx, fixture.store, fixture.projectID, fixture.userID, "manual-concurrent")
	setDeliveryStatus(t, fixture, concurrentID, "failed")
	const callers = 8
	start := make(chan struct{})
	results := make(chan Delivery, callers)
	errorsCh := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			delivery, retryErr := fixture.store.CreateRetry(fixture.ctx, fixture.projectID, concurrentID, "concurrent operator retry")
			results <- delivery
			errorsCh <- retryErr
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsCh)
	for retryErr := range errorsCh {
		if retryErr != nil {
			t.Fatalf("concurrent retry: %v", retryErr)
		}
	}
	var retryID string
	for delivery := range results {
		if retryID == "" {
			retryID = delivery.ID
		}
		if delivery.ID != retryID || delivery.DeliveryKey != "retry:"+concurrentID {
			t.Fatalf("concurrent retry result: %#v, want delivery %s", delivery, retryID)
		}
	}
	assertManualRetryCount(t, fixture, concurrentID, 1)
}

type deliveryStoreFixture struct {
	ctx       context.Context
	db        *sql.DB
	projectID string
	store     PostgresStore
	userID    string
}

func newDeliveryStoreFixture(t *testing.T) deliveryStoreFixture {
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
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES($1,$2,'Manual Retry Test','test','active',$3,$3)`, userID, userID+"@manual-retry.test", now); err != nil {
		t.Fatalf("insert manual retry user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Manual Retry Test',$2,$3,$3)`, projectID, userID, now); err != nil {
		t.Fatalf("insert manual retry project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})
	store := PostgresStore{Clock: clock.Fixed{Time: now}, DB: db, Generator: generator, Transaction: transaction.Manager{DB: transaction.SQLBeginner{DB: db}}}
	return deliveryStoreFixture{ctx: ctx, db: db, projectID: projectID, store: store, userID: userID}
}

func setDeliveryStatus(t *testing.T, fixture deliveryStoreFixture, deliveryID, status string) {
	t.Helper()
	if _, err := fixture.db.ExecContext(fixture.ctx, `UPDATE notification_deliveries SET status=$2 WHERE delivery_id=$1`, deliveryID, status); err != nil {
		t.Fatalf("set delivery %s status %s: %v", deliveryID, status, err)
	}
}

func assertManualRetryCount(t *testing.T, fixture deliveryStoreFixture, sourceDeliveryID string, want int) {
	t.Helper()
	var count int
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT COUNT(*) FROM notification_deliveries WHERE delivery_key=$1`, "retry:"+sourceDeliveryID).Scan(&count); err != nil {
		t.Fatalf("count manual retries: %v", err)
	}
	if count != want {
		t.Fatalf("manual retry count for %s: got %d, want %d", sourceDeliveryID, count, want)
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

func assertPersistedDeliveryResult(t *testing.T, ctx context.Context, db *sql.DB, deliveryID string, want ProviderSendResult, wantOutcome string) {
	t.Helper()
	var status, providerMessageID, deliverySummary, attemptOutcome, attemptSummary string
	if err := db.QueryRowContext(ctx, `
		SELECT delivery.status,COALESCE(delivery.provider_message_id,''),COALESCE(delivery.response_summary,''),
		       attempt.outcome,COALESCE(attempt.response_summary,'')
		FROM notification_deliveries delivery
		JOIN notification_delivery_attempts attempt USING(delivery_id)
		WHERE delivery.delivery_id=$1
		ORDER BY attempt.attempt_number DESC
		LIMIT 1
	`, deliveryID).Scan(&status, &providerMessageID, &deliverySummary, &attemptOutcome, &attemptSummary); err != nil {
		t.Fatalf("read persisted provider result: %v", err)
	}
	if status != wantOutcome || attemptOutcome != wantOutcome || providerMessageID != want.ProviderMessageID || deliverySummary != want.ResponseSummary || attemptSummary != want.ResponseSummary {
		t.Fatalf("persisted provider result: status=%q provider_message_id=%q delivery_summary=%q attempt_outcome=%q attempt_summary=%q want=%#v outcome=%q", status, providerMessageID, deliverySummary, attemptOutcome, attemptSummary, want, wantOutcome)
	}
}
