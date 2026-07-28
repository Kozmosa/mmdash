package events

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/eventbus"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

type testTx struct{}

func (*testTx) Commit() error { return nil }
func (*testTx) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, nil
}
func (*testTx) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, nil
}
func (*testTx) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return nil
}
func (*testTx) Rollback() error { return nil }

type testBeginner struct {
	tx transaction.Tx
}

func (beginner testBeginner) Begin(context.Context, *sql.TxOptions) (transaction.Tx, error) {
	return beginner.tx, nil
}

type writerStub struct {
	event outbox.Event
}

func (writer *writerStub) Write(
	_ context.Context,
	_ transaction.Tx,
	event outbox.Event,
) (outbox.Event, error) {
	event.EventID = "00000000-0000-4000-8000-000000000001"
	writer.event = event
	return event, nil
}

type storeStub struct {
	consumers []string
	replay    outbox.Replay
	state     outbox.State
}

func (store *storeStub) CreateReplay(
	_ context.Context,
	_ string,
	_ string,
	consumers []string,
	_ string,
	_ string,
) (outbox.Replay, error) {
	store.consumers = append([]string(nil), consumers...)
	return store.replay, nil
}

func (store *storeStub) GetState(context.Context, string) (outbox.State, error) {
	return store.state, nil
}

func TestEmitTestRequiresAdminAndUsesOutboxTransaction(t *testing.T) {
	writer := &writerStub{}
	service := Service{
		Outbox: writer,
		Transaction: transaction.Manager{
			DB: testBeginner{tx: &testTx{}},
		},
	}
	if _, err := service.EmitTest(
		context.Background(),
		auth.Identity{Kind: "session", User: auth.User{SystemRole: "member"}},
		"hello",
		nil,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected admin policy, got %v", err)
	}
	enqueued, err := service.EmitTest(
		context.Background(),
		auth.Identity{
			Kind: "session",
			User: auth.User{ID: "user-1", SystemRole: "admin"},
		},
		"hello",
		map[string]interface{}{"value": 42},
	)
	if err != nil {
		t.Fatalf("emit test: %v", err)
	}
	if enqueued.Status != "pending" || writer.event.EventType != "system.test.emitted" {
		t.Fatalf("unexpected event: %#v %#v", enqueued, writer.event)
	}
	if writer.event.Payload["message"] != "hello" || writer.event.Payload["value"] != 42 {
		t.Fatalf("unexpected payload: %#v", writer.event.Payload)
	}
}

func TestReplayUsesFreshDeliveryScopeForMatchingConsumer(t *testing.T) {
	bus := eventbus.New()
	if err := bus.Register(eventbus.Consumer{
		Name:     "platform.test-receipt",
		Patterns: []string{"system.test.*"},
		Handler:  func(context.Context, contract.EventEnvelope) error { return nil },
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	store := &storeStub{
		replay: outbox.Replay{ID: "replay-1"},
		state: outbox.State{Record: outbox.Record{
			Envelope: contract.EventEnvelope{EventType: "system.test.emitted"},
		}},
	}
	service := Service{Bus: bus, Store: store}
	replay, err := service.Replay(
		context.Background(),
		auth.Identity{
			Kind: "api",
			User: auth.User{ID: "user-1", SystemRole: "admin"},
		},
		"event-1",
		"platform.test-receipt",
		"verify replay",
	)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.ID != "replay-1" ||
		len(store.consumers) != 1 ||
		store.consumers[0] != "platform.test-receipt" {
		t.Fatalf("unexpected replay: %#v %#v", replay, store.consumers)
	}
}

func TestReplayRejectsUnregisteredConsumer(t *testing.T) {
	store := &storeStub{state: outbox.State{Record: outbox.Record{
		Envelope: contract.EventEnvelope{
			EventType:  "system.test.emitted",
			OccurredAt: time.Now(),
		},
	}}}
	service := Service{Bus: eventbus.New(), Store: store}
	_, err := service.Replay(
		context.Background(),
		auth.Identity{
			Kind: "session",
			User: auth.User{ID: "user-1", SystemRole: "admin"},
		},
		"event-1",
		"missing",
		"verify",
	)
	if !errors.Is(err, outbox.ErrNoConsumers) {
		t.Fatalf("expected no matching consumers, got %v", err)
	}
}
