package progress

import (
	"context"
	"testing"
	"time"
)

type localCronStoreStub struct {
	TrackingStore
	claim     *CronClaim
	completed bool
	request   EvaluationTrigger
}

func (store *localCronStoreStub) ClaimCron(context.Context, string, time.Duration) (*CronClaim, error) {
	return store.claim, nil
}

func (store *localCronStoreStub) ScheduleRequest(_ context.Context, projectID, actorID, actorKind, triggerKind string, force bool, trigger EvaluationTrigger) (RecalculateResult, error) {
	store.request = trigger
	if projectID != store.claim.ProjectID || actorID != store.claim.ActorID || actorKind != "system" || triggerKind != "cron" || force {
		return RecalculateResult{}, ErrInvalid
	}
	return RecalculateResult{RequestID: "request-1", Status: "pending"}, nil
}

func (store *localCronStoreStub) CompleteCron(_ context.Context, projectID, _ string, scheduledFor time.Time) error {
	store.completed = projectID == store.claim.ProjectID && scheduledFor.Equal(store.claim.ScheduledFor)
	return nil
}

func TestNextCronOccurrenceUsesMmdashUTCClock(t *testing.T) {
	now := time.Date(2026, time.August, 11, 8, 7, 45, 0, time.UTC)
	next, err := nextCronOccurrence("*/15 8-10 * * 1-5", now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.August, 11, 8, 15, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next occurrence=%s want=%s", next, want)
	}
}

func TestNextCronOccurrenceSupportsStandardDayOrSemantics(t *testing.T) {
	now := time.Date(2026, time.August, 11, 8, 0, 0, 0, time.UTC)
	next, err := nextCronOccurrence("0 9 12 * 5", now)
	if err != nil {
		t.Fatal(err)
	}
	// The 12th matches before the next Friday, because standard cron treats
	// restricted day-of-month and day-of-week fields as an OR.
	want := time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next occurrence=%s want=%s", next, want)
	}
}

func TestParseCronRejectsInvalidRanges(t *testing.T) {
	for _, expression := range []string{"* * * *", "60 * * * *", "*/0 * * * *", "* * * * 8"} {
		if _, err := parseCron(expression); err == nil {
			t.Fatalf("accepted invalid expression %q", expression)
		}
	}
}

func TestTrackingProcessorSchedulesCronInsideMmdash(t *testing.T) {
	scheduledFor := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)
	store := &localCronStoreStub{claim: &CronClaim{
		ProjectID: "project-1", ActorID: "user-1", ScheduledFor: scheduledFor,
	}}
	processor := TrackingProcessor{Owner: "core-1", Store: store}
	if err := processor.ScheduleCronOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.completed || store.request.TriggerType != "cron" ||
		store.request.Payload["scheduler"] != "mmdash" ||
		!store.request.OccurredAt.Equal(scheduledFor) {
		t.Fatalf("local cron was not scheduled through mmdash: %#v", store)
	}
}
