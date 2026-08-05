package progress

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresReminderProcessorDueConcurrencyAndRestartIdempotency(t *testing.T) {
	fixture := newReminderProcessorFixture(t)
	future := fixture.createReminder(t, fixture.clock.Now().Add(time.Hour), "future")
	due := fixture.createReminder(t, fixture.clock.Now().Add(-time.Minute), "due")
	processor := ReminderProcessor{BatchSize: 10, Lease: time.Minute, Owner: "core-a", Store: fixture.store}

	processed, err := processor.RunBatch(fixture.ctx)
	if err != nil || processed != 1 {
		t.Fatalf("process due reminders: processed=%d err=%v", processed, err)
	}
	fixture.assertReminder(t, future.ID, ReminderPending, 0)
	fixture.assertReminder(t, due.ID, ReminderTriggered, 1)
	fixture.assertDueEvent(t, due.ID, 1)

	if _, err := fixture.store.CompleteReminder(fixture.ctx, due.ID, "core-a", fixture.userID); !errors.Is(err, ErrReminderLeaseLost) {
		t.Fatalf("repeat complete: got %v, want lease lost", err)
	}
	if _, err := fixture.store.TriggerReminder(fixture.ctx, fixture.projectID, due.ID, fixture.userID); !errors.Is(err, ErrConflict) {
		t.Fatalf("repeat manual trigger: got %v, want conflict", err)
	}
	restarted := ReminderProcessor{BatchSize: 10, Lease: time.Minute, Owner: "core-restarted", Store: fixture.store}
	processed, err = restarted.RunBatch(fixture.ctx)
	if err != nil || processed != 0 {
		t.Fatalf("restart replay: processed=%d err=%v", processed, err)
	}
	fixture.assertDueEvent(t, due.ID, 1)

	concurrent := fixture.createReminder(t, fixture.clock.Now().Add(-time.Minute), "concurrent")
	start := make(chan struct{})
	results := make(chan int, 2)
	errorsCh := make(chan error, 2)
	var wait sync.WaitGroup
	for _, owner := range []string{"core-b", "core-c"} {
		wait.Add(1)
		go func(owner string) {
			defer wait.Done()
			<-start
			count, processErr := (ReminderProcessor{BatchSize: 1, Lease: time.Minute, Owner: owner, Store: fixture.store}).RunBatch(fixture.ctx)
			results <- count
			errorsCh <- processErr
		}(owner)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsCh)
	for processErr := range errorsCh {
		if processErr != nil {
			t.Fatalf("concurrent processor: %v", processErr)
		}
	}
	total := 0
	for count := range results {
		total += count
	}
	if total != 1 {
		t.Fatalf("concurrent processed count: got %d, want 1", total)
	}
	fixture.assertReminder(t, concurrent.ID, ReminderTriggered, 1)
	fixture.assertDueEvent(t, concurrent.ID, 1)
}

func TestPostgresReminderProcessorRecoversLeasesRetriesAndFailsTerminally(t *testing.T) {
	fixture := newReminderProcessorFixture(t)
	crashed := fixture.createReminder(t, fixture.clock.Now().Add(-time.Minute), "crashed")
	claimed, err := fixture.store.ClaimDueReminders(fixture.ctx, "crashed-core", time.Second, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim crashed reminder: items=%#v err=%v", claimed, err)
	}
	fixture.clock.Advance(2 * time.Second)
	recovered, err := fixture.store.ClaimDueReminders(fixture.ctx, "recovered-core", time.Minute, 1)
	if err != nil || len(recovered) != 1 || recovered[0].ID != crashed.ID || recovered[0].Attempts != 2 {
		t.Fatalf("recover expired lease: items=%#v err=%v", recovered, err)
	}
	if _, err := fixture.store.CompleteReminder(fixture.ctx, crashed.ID, "recovered-core", fixture.userID); err != nil {
		t.Fatalf("complete recovered reminder: %v", err)
	}
	fixture.assertDueEvent(t, crashed.ID, 1)

	retried := fixture.createReminder(t, fixture.clock.Now().Add(-time.Minute), "retry-delay")
	claimed, err = fixture.store.ClaimDueReminders(fixture.ctx, "retry-core", time.Minute, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != retried.ID {
		t.Fatalf("claim retry reminder: items=%#v err=%v", claimed, err)
	}
	failed, err := fixture.store.FailReminder(fixture.ctx, retried.ID, "retry-core", "temporary", "safe retry", time.Minute)
	if err != nil || failed.Status != ReminderPending || failed.LastErrorCode != "temporary" {
		t.Fatalf("release retry reminder: item=%#v err=%v", failed, err)
	}
	claimed, err = fixture.store.ClaimDueReminders(fixture.ctx, "early-core", time.Minute, 1)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("retry claimed before available_at: items=%#v err=%v", claimed, err)
	}
	fixture.clock.Advance(time.Minute)
	claimed, err = fixture.store.ClaimDueReminders(fixture.ctx, "retry-core-2", time.Minute, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != retried.ID || claimed[0].Attempts != 2 {
		t.Fatalf("claim available retry: items=%#v err=%v", claimed, err)
	}
	if _, err := fixture.store.CompleteReminder(fixture.ctx, retried.ID, "retry-core-2", fixture.userID); err != nil {
		t.Fatalf("complete retried reminder: %v", err)
	}

	terminal := fixture.createReminder(t, fixture.clock.Now().Add(-time.Minute), "terminal")
	if _, err := fixture.db.ExecContext(fixture.ctx, `UPDATE progress_reminders SET max_attempts=1 WHERE reminder_id=$1`, terminal.ID); err != nil {
		t.Fatalf("set terminal attempt limit: %v", err)
	}
	claimed, err = fixture.store.ClaimDueReminders(fixture.ctx, "terminal-crash", time.Second, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != terminal.ID {
		t.Fatalf("claim terminal reminder: items=%#v err=%v", claimed, err)
	}
	fixture.clock.Advance(2 * time.Second)
	claimed, err = fixture.store.ClaimDueReminders(fixture.ctx, "terminal-recovery", time.Minute, 1)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("terminal lease recovery claimed work: items=%#v err=%v", claimed, err)
	}
	fixture.assertReminder(t, terminal.ID, ReminderFailed, 1)
	var code string
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT last_error_code FROM progress_reminders WHERE reminder_id=$1`, terminal.ID).Scan(&code); err != nil || code != "lease_expired" {
		t.Fatalf("terminal lease error: code=%q err=%v", code, err)
	}
	fixture.assertDueEvent(t, terminal.ID, 0)
}

func TestPostgresManualAndAutomaticReminderTriggerDoNotDuplicate(t *testing.T) {
	fixture := newReminderProcessorFixture(t)
	item := fixture.createReminder(t, fixture.clock.Now().Add(-time.Minute), "manual-race")
	start := make(chan struct{})
	manualErr := make(chan error, 1)
	processorResult := make(chan struct {
		count int
		err   error
	}, 1)
	go func() {
		<-start
		_, err := fixture.store.TriggerReminder(fixture.ctx, fixture.projectID, item.ID, fixture.userID)
		manualErr <- err
	}()
	go func() {
		<-start
		count, err := (ReminderProcessor{BatchSize: 1, Lease: time.Minute, Owner: "automatic-core", Store: fixture.store}).RunBatch(fixture.ctx)
		processorResult <- struct {
			count int
			err   error
		}{count: count, err: err}
	}()
	close(start)
	triggerErr := <-manualErr
	result := <-processorResult
	if triggerErr != nil && !errors.Is(triggerErr, ErrConflict) {
		t.Fatalf("manual trigger race: %v", triggerErr)
	}
	if result.err != nil || result.count < 0 || result.count > 1 {
		t.Fatalf("automatic trigger race: count=%d err=%v", result.count, result.err)
	}
	fixture.assertReminder(t, item.ID, ReminderTriggered, 1)
	fixture.assertDueEvent(t, item.ID, 1)
}

type mutableReminderClock struct {
	mutex sync.RWMutex
	now   time.Time
}

func (clock *mutableReminderClock) Now() time.Time {
	clock.mutex.RLock()
	defer clock.mutex.RUnlock()
	return clock.now
}

func (clock *mutableReminderClock) Advance(duration time.Duration) {
	clock.mutex.Lock()
	clock.now = clock.now.Add(duration)
	clock.mutex.Unlock()
}

type reminderProcessorFixture struct {
	clock     *mutableReminderClock
	ctx       context.Context
	db        *sql.DB
	projectID string
	store     PostgresStore
	taskID    string
	userID    string
}

func newReminderProcessorFixture(t *testing.T) reminderProcessorFixture {
	t.Helper()
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	userID, projectID, taskID := generator.MustNew(), generator.MustNew(), generator.MustNew()
	now := time.Date(2026, time.August, 6, 2, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES($1,$2,'Reminder Processor Test','test','active',$3,$3)`, userID, userID+"@progress-reminder.test", now); err != nil {
		t.Fatalf("insert reminder user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Reminder Processor Test',$2,$3,$3)`, projectID, userID, now); err != nil {
		t.Fatalf("insert reminder project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO progress_tasks(task_id,project_id,title,status,source,created_by,updated_by,created_at,updated_at) VALUES($1,$2,'Reminder Task','todo','human',$3,$3,$4,$4)`, taskID, projectID, userID, now); err != nil {
		t.Fatalf("insert reminder task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})
	mutableClock := &mutableReminderClock{now: now}
	manager := transaction.Manager{DB: transaction.SQLBeginner{DB: db}}
	store := PostgresStore{
		Clock: mutableClock, DB: db, Generator: generator,
		Outbox:        outbox.Writer{Clock: mutableClock, Generator: generator},
		ReminderLease: time.Minute, ReminderRetryDelay: time.Second,
		Transaction: manager,
	}
	return reminderProcessorFixture{clock: mutableClock, ctx: ctx, db: db, projectID: projectID, store: store, taskID: taskID, userID: userID}
}

func (fixture reminderProcessorFixture) createReminder(t *testing.T, remindAt time.Time, note string) Reminder {
	t.Helper()
	item, err := fixture.store.CreateReminder(fixture.ctx, fixture.projectID, fixture.userID, CreateReminderInput{TaskID: fixture.taskID, RemindAt: remindAt, Note: note})
	if err != nil {
		t.Fatalf("create reminder %s: %v", note, err)
	}
	return item
}

func (fixture reminderProcessorFixture) assertReminder(t *testing.T, id, wantStatus string, wantAttempts int) {
	t.Helper()
	var status, lockedBy string
	var attempts int
	var leaseExpiresAt *time.Time
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT status,attempts,COALESCE(locked_by,''),lease_expires_at FROM progress_reminders WHERE reminder_id=$1`, id).Scan(&status, &attempts, &lockedBy, &leaseExpiresAt); err != nil {
		t.Fatalf("read reminder %s: %v", id, err)
	}
	if status != wantStatus || attempts != wantAttempts || lockedBy != "" || leaseExpiresAt != nil {
		t.Fatalf("reminder %s: status=%s attempts=%d locked_by=%q lease=%v; want status=%s attempts=%d unlocked", id, status, attempts, lockedBy, leaseExpiresAt, wantStatus, wantAttempts)
	}
}

func (fixture reminderProcessorFixture) assertDueEvent(t *testing.T, reminderID string, want int) {
	t.Helper()
	var count int
	var eventID, actorID string
	err := fixture.db.QueryRowContext(fixture.ctx, `SELECT COUNT(*),COALESCE(MIN(event_id::text),''),COALESCE(MIN(actor->>'user_id'),'') FROM system_outbox WHERE event_type='progress.reminder.due' AND payload->>'reminder_id'=$1`, reminderID).Scan(&count, &eventID, &actorID)
	if err != nil {
		t.Fatalf("read due event for %s: %v", reminderID, err)
	}
	if count != want {
		t.Fatalf("due event count for %s: got %d, want %d", reminderID, count, want)
	}
	if want == 1 && (eventID != reminderID || actorID != fixture.userID) {
		t.Fatalf("stable due event for %s: event_id=%s actor=%s", reminderID, eventID, actorID)
	}
}
