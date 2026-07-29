package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
)

type cleanupStoreFixture struct {
	claimed    []Repository
	completed  []string
	retryAt    *time.Time
	retryID    string
	storeError error
}

func (store *cleanupStoreFixture) ClaimCleanup(
	context.Context,
	time.Time,
	time.Duration,
	int,
) ([]Repository, error) {
	return store.claimed, store.storeError
}

func (store *cleanupStoreFixture) CompleteCleanup(
	_ context.Context,
	repositoryID string,
) error {
	store.completed = append(store.completed, repositoryID)
	return nil
}

func (store *cleanupStoreFixture) RetryCleanup(
	_ context.Context,
	repositoryID string,
	retryAt time.Time,
	_ time.Time,
) error {
	store.retryID = repositoryID
	store.retryAt = &retryAt
	return nil
}

type cleanupStorageFixture struct {
	removed []string
	err     error
}

func (storage *cleanupStorageFixture) RemoveRepository(storageKey string) error {
	storage.removed = append(storage.removed, storageKey)
	return storage.err
}

func TestCleanupReaperRemovesStorageBeforeMetadata(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	store := &cleanupStoreFixture{claimed: []Repository{{
		ID: "repository-1", StorageKey: "57c7c47f-a671-43df-938d-c813ffef8a99",
	}}}
	storage := &cleanupStorageFixture{}
	reaper := CleanupReaper{
		Clock: clock.Fixed{Time: now}, Storage: storage, Store: store,
	}

	if err := reaper.RunOnce(context.Background()); err != nil {
		t.Fatalf("clean disconnected repository: %v", err)
	}
	if len(storage.removed) != 1 || storage.removed[0] != store.claimed[0].StorageKey {
		t.Fatalf("managed storage was not removed: %#v", storage.removed)
	}
	if len(store.completed) != 1 || store.completed[0] != "repository-1" {
		t.Fatalf("metadata cleanup did not complete: %#v", store.completed)
	}
	if store.retryAt != nil {
		t.Fatalf("successful cleanup was rescheduled: %v", store.retryAt)
	}
}

func TestCleanupReaperReschedulesUnsafeStorageFailure(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	store := &cleanupStoreFixture{claimed: []Repository{{
		ID: "repository-unsafe", StorageKey: "unsafe",
	}}}
	storage := &cleanupStorageFixture{err: errors.New("unsafe storage")}
	reaper := CleanupReaper{
		Clock: clock.Fixed{Time: now}, Interval: 30 * time.Second,
		Storage: storage, Store: store,
	}

	if err := reaper.RunOnce(context.Background()); err == nil {
		t.Fatal("unsafe storage cleanup unexpectedly passed")
	}
	if len(store.completed) != 0 {
		t.Fatalf("metadata was deleted after storage failure: %#v", store.completed)
	}
	if store.retryID != "repository-unsafe" ||
		store.retryAt == nil ||
		!store.retryAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("cleanup was not safely rescheduled: %s %v", store.retryID, store.retryAt)
	}
}

func TestCleanupReaperRejectsMissingDependencies(t *testing.T) {
	if err := (CleanupReaper{}).RunOnce(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing dependencies should be invalid: %v", err)
	}
}
