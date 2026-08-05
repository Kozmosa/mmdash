package progress

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type reminderProcessorStoreStub struct {
	claimed        []Reminder
	claimLease     time.Duration
	claimLimit     int
	claimOwner     string
	completeActor  string
	completeErr    error
	failedCode     string
	failedMessage  string
	failedReminder Reminder
}

func (store *reminderProcessorStoreStub) ClaimDueReminders(_ context.Context, owner string, lease time.Duration, limit int) ([]Reminder, error) {
	store.claimOwner, store.claimLease, store.claimLimit = owner, lease, limit
	items := store.claimed
	store.claimed = nil
	return items, nil
}

func (store *reminderProcessorStoreStub) CompleteReminder(_ context.Context, _ string, _ string, actorID string) (Reminder, error) {
	store.completeActor = actorID
	return Reminder{Status: ReminderTriggered}, store.completeErr
}

func (store *reminderProcessorStoreStub) FailReminder(_ context.Context, _, _, code, message string, _ time.Duration) (Reminder, error) {
	store.failedCode, store.failedMessage = code, message
	return store.failedReminder, nil
}

func TestReminderProcessorUsesConfiguredBatchAndReminderCreator(t *testing.T) {
	store := &reminderProcessorStoreStub{claimed: []Reminder{{ID: "reminder-1", CreatedBy: "creator-1"}}}
	processor := ReminderProcessor{BatchSize: 7, Lease: 45 * time.Second, Owner: "core-1", Store: store}

	processed, err := processor.RunBatch(context.Background())
	if err != nil || processed != 1 {
		t.Fatalf("process reminder batch: processed=%d err=%v", processed, err)
	}
	if store.claimOwner != "core-1" || store.claimLease != 45*time.Second || store.claimLimit != 7 {
		t.Fatalf("claim configuration: owner=%s lease=%s limit=%d", store.claimOwner, store.claimLease, store.claimLimit)
	}
	if store.completeActor != "creator-1" {
		t.Fatalf("automatic reminder actor: got %s, want creator-1", store.completeActor)
	}
}

func TestReminderProcessorFailureUsesBoundedGenericDiagnostics(t *testing.T) {
	secretNote := "private reminder note"
	store := &reminderProcessorStoreStub{
		claimed:        []Reminder{{ID: "reminder-1", CreatedBy: "creator-1", Note: secretNote}},
		completeErr:    errors.New("outbox unavailable"),
		failedReminder: Reminder{Status: ReminderPending},
	}
	processed, err := (ReminderProcessor{Owner: "core-1", Store: store}).RunBatch(context.Background())
	if err == nil || processed != 1 {
		t.Fatalf("failed reminder batch: processed=%d err=%v", processed, err)
	}
	if store.failedCode != "event_write_failed" || strings.Contains(store.failedMessage, secretNote) {
		t.Fatalf("unsafe reminder failure diagnostics: code=%q message=%q", store.failedCode, store.failedMessage)
	}
}
