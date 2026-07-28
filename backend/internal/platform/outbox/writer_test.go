package outbox

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
)

type txStub struct {
	args  []interface{}
	query string
}

func (*txStub) Commit() error {
	return nil
}

func (tx *txStub) ExecContext(_ context.Context, query string, args ...interface{}) (sql.Result, error) {
	tx.query = query
	tx.args = args
	return nil, nil
}

func (*txStub) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, nil
}

func (*txStub) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return nil
}

func (*txStub) Rollback() error {
	return nil
}

func TestWriterUsesCallerTransactionAndFillsEnvelope(t *testing.T) {
	tx := &txStub{}
	instant := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	writer := Writer{
		Clock:     clock.Fixed{Time: instant},
		Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
	}

	event, err := writer.Write(context.Background(), tx, Event{
		EventType: "example.checked",
		Payload:   map[string]interface{}{"status": "ok"},
		Producer:  "example",
	})
	if err != nil {
		t.Fatalf("write outbox event: %v", err)
	}
	if event.SchemaVersion != 1 || event.OccurredAt != instant || event.EventID == "" {
		t.Fatalf("unexpected event defaults: %#v", event)
	}
	if !strings.Contains(tx.query, "INSERT INTO system_outbox") || len(tx.args) != 10 {
		t.Fatalf("unexpected transaction call: %s %#v", tx.query, tx.args)
	}
}

func TestWriterRejectsEnvelopeThatViolatesStableContract(t *testing.T) {
	tx := &txStub{}
	writer := Writer{
		Clock:     clock.Fixed{Time: time.Now()},
		Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
	}
	_, err := writer.Write(context.Background(), tx, Event{
		EventType: "Not_A_Stable_Type",
		Payload:   map[string]interface{}{},
		Producer:  "test",
	})
	if err == nil || !strings.Contains(err.Error(), "event_type is invalid") {
		t.Fatalf("expected stable envelope validation, got %v", err)
	}
}
