package project

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
)

type invitationExpiryStoreStub struct {
	err       error
	expired   int
	limit     int
	processed time.Time
}

func (store *invitationExpiryStoreStub) ExpireInvitations(_ context.Context, now time.Time, limit int) (int, error) {
	store.processed = now
	store.limit = limit
	return store.expired, store.err
}

func TestInvitationExpiryProcessorUsesConfiguredBatchAndClock(t *testing.T) {
	now := time.Date(2026, time.August, 6, 5, 0, 0, 0, time.UTC)
	store := &invitationExpiryStoreStub{expired: 3}
	processed, err := (InvitationExpiryProcessor{
		BatchSize: 7,
		Clock:     clock.Fixed{Time: now},
		Store:     store,
	}).RunBatch(context.Background())
	if err != nil || processed != 3 {
		t.Fatalf("process invitation expiry batch: processed=%d err=%v", processed, err)
	}
	if store.limit != 7 || !store.processed.Equal(now) {
		t.Fatalf("expiry configuration: limit=%d now=%s", store.limit, store.processed)
	}
}

func TestInvitationExpiryProcessorReportsStoreFailure(t *testing.T) {
	expected := errors.New("database unavailable")
	store := &invitationExpiryStoreStub{err: expected}
	processed, err := (InvitationExpiryProcessor{Store: store}).RunBatch(context.Background())
	if processed != 0 || !errors.Is(err, expected) {
		t.Fatalf("process failure: processed=%d err=%v", processed, err)
	}
	if _, err := (InvitationExpiryProcessor{}).RunBatch(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing store: %v", err)
	}
}
