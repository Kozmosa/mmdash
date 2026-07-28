package outbox

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

type processorStore struct {
	completed    string
	delivery     *Delivery
	event        *Record
	failed       string
	publishedFor []string
}

func (store *processorStore) ClaimDelivery(
	context.Context,
	string,
	time.Duration,
) (*Delivery, error) {
	return store.delivery, nil
}

func (store *processorStore) ClaimEvent(
	context.Context,
	string,
	time.Duration,
) (*Record, error) {
	return store.event, nil
}

func (store *processorStore) CompleteDelivery(
	_ context.Context,
	deliveryID string,
	_ string,
) error {
	store.completed = deliveryID
	return nil
}

func (store *processorStore) FailDelivery(
	_ context.Context,
	delivery Delivery,
	message string,
	_ time.Duration,
) error {
	store.failed = delivery.ID + ":" + message
	return nil
}

func (*processorStore) FailEvent(
	context.Context,
	Record,
	string,
	string,
	time.Duration,
) error {
	return nil
}

func (store *processorStore) Publish(
	_ context.Context,
	_ Record,
	_ string,
	consumers []string,
) error {
	store.publishedFor = append([]string(nil), consumers...)
	return nil
}

type processorBus struct {
	deliverErr error
	delivered  string
}

func (bus *processorBus) Deliver(
	_ context.Context,
	consumer string,
	_ contract.EventEnvelope,
) error {
	bus.delivered = consumer
	return bus.deliverErr
}

func (*processorBus) Matching(string) []string {
	return []string{"data-hub", "timeline"}
}

func TestProcessorPublishesAndCompletesIndependentDelivery(t *testing.T) {
	envelope := processorFixtureEnvelope()
	store := &processorStore{
		event:    &Record{Envelope: envelope},
		delivery: &Delivery{ID: "delivery-1", ConsumerName: "data-hub", Envelope: envelope},
	}
	bus := &processorBus{}
	worked, err := (Processor{
		Bus:   bus,
		Owner: "core-1",
		Store: store,
	}).RunOnce(context.Background())
	if err != nil || !worked {
		t.Fatalf("run once: %v %v", worked, err)
	}
	if !reflect.DeepEqual(store.publishedFor, []string{"data-hub", "timeline"}) {
		t.Fatalf("unexpected consumers: %#v", store.publishedFor)
	}
	if bus.delivered != "data-hub" || store.completed != "delivery-1" {
		t.Fatalf("delivery was not completed: %#v %#v", bus, store)
	}
}

func TestProcessorPersistsConsumerFailureForRetry(t *testing.T) {
	expected := errors.New("projection failed")
	envelope := processorFixtureEnvelope()
	store := &processorStore{
		delivery: &Delivery{ID: "delivery-1", ConsumerName: "data-hub", Envelope: envelope},
	}
	worked, err := (Processor{
		Bus:   &processorBus{deliverErr: expected},
		Owner: "core-1",
		Store: store,
	}).RunOnce(context.Background())
	if err != nil || !worked {
		t.Fatalf("run once: %v %v", worked, err)
	}
	if store.failed != "delivery-1:projection failed" || store.completed != "" {
		t.Fatalf("failure was not persisted: %#v", store)
	}
}

func processorFixtureEnvelope() contract.EventEnvelope {
	return contract.EventEnvelope{
		EventID:       "00000000-0000-4000-8000-000000000001",
		EventType:     "system.test.emitted",
		OccurredAt:    time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
		Payload:       map[string]interface{}{},
		Producer:      "system",
		SchemaVersion: 1,
	}
}
