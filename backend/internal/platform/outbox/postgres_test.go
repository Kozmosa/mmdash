package outbox

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

type deliveryTx struct {
	committed bool
	queries   []string
}

func (tx *deliveryTx) Commit() error {
	tx.committed = true
	return nil
}

func (tx *deliveryTx) ExecContext(
	_ context.Context,
	query string,
	_ ...interface{},
) (sql.Result, error) {
	tx.queries = append(tx.queries, query)
	return deliveryResult(1), nil
}

func (*deliveryTx) QueryContext(
	context.Context,
	string,
	...interface{},
) (*sql.Rows, error) {
	return nil, nil
}

func (*deliveryTx) QueryRowContext(
	context.Context,
	string,
	...interface{},
) *sql.Row {
	return nil
}

func (*deliveryTx) Rollback() error { return nil }

type deliveryResult int64

func (result deliveryResult) LastInsertId() (int64, error) { return int64(result), nil }
func (result deliveryResult) RowsAffected() (int64, error) { return int64(result), nil }

type deliveryBeginner struct {
	tx transaction.Tx
}

func (beginner deliveryBeginner) Begin(
	context.Context,
	*sql.TxOptions,
) (transaction.Tx, error) {
	return beginner.tx, nil
}

func TestClaimQueriesUseSkipLocked(t *testing.T) {
	for name, query := range map[string]string{
		"publication": claimEventQuery,
		"delivery":    claimDeliveryQuery,
	} {
		if !strings.Contains(query, "FOR UPDATE") ||
			!strings.Contains(query, "SKIP LOCKED") {
			t.Fatalf("%s claim must be concurrency safe: %s", name, query)
		}
	}
}

func TestSafeErrorIsBoundedAndNonEmpty(t *testing.T) {
	if safeError("") == "" {
		t.Fatal("empty errors must receive a safe fallback")
	}
	if got := len(safeError(strings.Repeat("x", 1200))); got != 1000 {
		t.Fatalf("unexpected bounded error length: %d", got)
	}
}

func TestPublishSeedsLiveDeliveriesAndMarksEventInOneTransaction(t *testing.T) {
	tx := &deliveryTx{}
	store := PostgresStore{
		Clock:     clock.Fixed{Time: time.Now()},
		Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		Transaction: transaction.Manager{
			DB: deliveryBeginner{tx: tx},
		},
	}
	err := store.Publish(context.Background(), Record{
		Envelope: contract.EventEnvelope{
			EventID: "00000000-0000-4000-8000-000000000001",
		},
	}, "core-1", []string{"data-hub"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !tx.committed || len(tx.queries) != 2 {
		t.Fatalf("publication was not atomic: %#v", tx)
	}
	if !strings.Contains(tx.queries[0], "'live'") ||
		!strings.Contains(tx.queries[1], "status = 'published'") {
		t.Fatalf("unexpected publication SQL: %#v", tx.queries)
	}
}

func TestCompleteDeliveryRecordsIdempotencyInSuccessTransaction(t *testing.T) {
	tx := &deliveryTx{}
	store := PostgresStore{
		Clock: clock.Fixed{Time: time.Now()},
		Transaction: transaction.Manager{
			DB: deliveryBeginner{tx: tx},
		},
	}
	if err := store.CompleteDelivery(
		context.Background(),
		"delivery-1",
		"core-1",
	); err != nil {
		t.Fatalf("complete delivery: %v", err)
	}
	if !tx.committed || len(tx.queries) != 2 {
		t.Fatalf("completion was not atomic: %#v", tx)
	}
	if !strings.Contains(tx.queries[0], "status = 'succeeded'") ||
		!strings.Contains(tx.queries[1], "system_event_consumptions") ||
		!strings.Contains(tx.queries[1], "ON CONFLICT") {
		t.Fatalf("idempotency record is missing: %#v", tx.queries)
	}
}
