package repo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
)

type reaperCheckoutStore struct {
	expired []Checkout
	marked  []string
}

func (*reaperCheckoutStore) CreateCheckout(context.Context, Checkout) error {
	return nil
}

func (store *reaperCheckoutStore) ExpireCheckouts(
	context.Context, time.Time, int,
) ([]Checkout, error) {
	return store.expired, nil
}

func (*reaperCheckoutStore) GetCheckout(
	context.Context, string, string,
) (Checkout, error) {
	return Checkout{}, ErrCheckoutNotFound
}

func (*reaperCheckoutStore) ListActiveCheckouts(
	context.Context, string,
) ([]Checkout, error) {
	return nil, nil
}

func (store *reaperCheckoutStore) MarkCheckoutError(
	_ context.Context,
	checkoutID string,
) error {
	store.marked = append(store.marked, checkoutID)
	return nil
}

func (*reaperCheckoutStore) ReleaseCheckout(
	context.Context, string, string, time.Time,
) (Checkout, error) {
	return Checkout{}, nil
}

type reaperRepositoryStore struct {
	repository Repository
}

func (store reaperRepositoryStore) GetByID(
	context.Context,
	string,
) (Repository, error) {
	return store.repository, nil
}

func TestCheckoutReaperRemovesExpiredWorktree(t *testing.T) {
	reader, repository, head := readerFixture(t)
	runtime := Runtime{
		Clock: reader.Clock, CloneTimeout: 30 * time.Second,
		Git: reader.Git, Storage: reader.Storage,
	}
	relative, err := runtime.CreateCheckout(
		context.Background(), repository,
		"00000000-0000-4000-8000-000000000103", head,
	)
	if err != nil {
		t.Fatalf("create checkout: %v", err)
	}
	target, err := reader.Storage.ManagedPath(repository.StorageKey, relative)
	if err != nil {
		t.Fatal(err)
	}
	store := &reaperCheckoutStore{expired: []Checkout{{
		CheckoutID:      "00000000-0000-4000-8000-000000000103",
		CheckoutRelpath: relative,
		RepositoryID:    repository.ID,
	}}}
	reaper := CheckoutReaper{
		Clock: clock.Fixed{Time: time.Now()}, Limit: 10,
		Repositories: reaperRepositoryStore{repository: repository},
		Runtime:      runtime, Store: store,
	}
	if err := reaper.RunOnce(context.Background()); err != nil {
		t.Fatalf("reap expired checkout: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired checkout survived: %v", err)
	}
	if len(store.marked) != 0 {
		t.Fatalf("successful checkout was marked as failed: %#v", store.marked)
	}
}

func TestCheckoutReaperMarksUnsafeCheckoutPath(t *testing.T) {
	reader, repository, _ := readerFixture(t)
	store := &reaperCheckoutStore{expired: []Checkout{{
		CheckoutID:      "checkout-unsafe",
		CheckoutRelpath: "../outside",
		RepositoryID:    repository.ID,
	}}}
	reaper := CheckoutReaper{
		Clock: clock.Fixed{Time: time.Now()}, Limit: 10,
		Repositories: reaperRepositoryStore{repository: repository},
		Runtime:      Runtime{Git: reader.Git, Storage: reader.Storage},
		Store:        store,
	}
	if err := reaper.RunOnce(context.Background()); err == nil {
		t.Fatal("unsafe checkout path was accepted")
	}
	if len(store.marked) != 1 || store.marked[0] != "checkout-unsafe" {
		t.Fatalf("unsafe checkout was not marked: %#v", store.marked)
	}
}
