package eventbus

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

func TestBusMatchesAndPublishesInDeterministicOrder(t *testing.T) {
	bus := New()
	calls := []string{}
	for _, consumer := range []Consumer{
		{
			Name:     "projector.z",
			Patterns: []string{"project.*"},
			Handler: func(_ context.Context, event contract.EventEnvelope) error {
				calls = append(calls, "z:"+event.EventType)
				return nil
			},
		},
		{
			Name:     "projector.a",
			Patterns: []string{"project.created"},
			Handler: func(_ context.Context, event contract.EventEnvelope) error {
				calls = append(calls, "a:"+event.EventType)
				return nil
			},
		},
	} {
		if err := bus.Register(consumer); err != nil {
			t.Fatalf("register consumer: %v", err)
		}
	}
	results, err := bus.Publish(context.Background(), fixtureEvent("project.created"))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"a:project.created", "z:project.created"}) {
		t.Fatalf("unexpected delivery order: %#v", calls)
	}
	if len(results) != 2 || results[0].Consumer != "projector.a" {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestBusRejectsDuplicatesAndNonMatchingDelivery(t *testing.T) {
	bus := New()
	consumer := Consumer{
		Name:     "projector",
		Patterns: []string{"project.*"},
		Handler:  func(context.Context, contract.EventEnvelope) error { return nil },
	}
	if err := bus.Register(consumer); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := bus.Register(consumer); err == nil {
		t.Fatal("expected duplicate registration to fail")
	}
	if err := bus.Deliver(
		context.Background(),
		"projector",
		fixtureEvent("settings.updated"),
	); err == nil {
		t.Fatal("expected non-matching delivery to fail")
	}
}

func TestPublishRetainsConsumerFailure(t *testing.T) {
	expected := errors.New("projection unavailable")
	bus := New()
	if err := bus.Register(Consumer{
		Name:     "failing",
		Patterns: []string{"*"},
		Handler: func(context.Context, contract.EventEnvelope) error {
			return expected
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	results, err := bus.Publish(context.Background(), fixtureEvent("project.created"))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(results) != 1 || !errors.Is(results[0].Err, expected) {
		t.Fatalf("unexpected result: %#v", results)
	}
}

func TestPublishRejectsInvalidEnvelopeEvenWithoutConsumers(t *testing.T) {
	_, err := New().Publish(context.Background(), contract.EventEnvelope{})
	if err == nil {
		t.Fatal("expected invalid envelope to fail before matching")
	}
}

func fixtureEvent(eventType string) contract.EventEnvelope {
	return contract.EventEnvelope{
		EventID:       "00000000-0000-4000-8000-000000000001",
		EventType:     eventType,
		OccurredAt:    time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
		Payload:       map[string]interface{}{},
		Producer:      "test",
		SchemaVersion: 1,
	}
}
