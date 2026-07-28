package transaction

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

type fakeBeginner struct {
	tx  *fakeTx
	err error
}

func (beginner fakeBeginner) Begin(context.Context, *sql.TxOptions) (Tx, error) {
	return beginner.tx, beginner.err
}

type fakeTx struct {
	committed  bool
	rolledBack bool
}

func (tx *fakeTx) Commit() error {
	tx.committed = true
	return nil
}

func (*fakeTx) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, nil
}

func (*fakeTx) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, nil
}

func (*fakeTx) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return nil
}

func (tx *fakeTx) Rollback() error {
	tx.rolledBack = true
	return nil
}

func TestManagerCommitsSuccessfulWork(t *testing.T) {
	tx := &fakeTx{}
	manager := Manager{DB: fakeBeginner{tx: tx}}

	if err := manager.Within(context.Background(), nil, func(Tx) error { return nil }); err != nil {
		t.Fatalf("run transaction: %v", err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("unexpected transaction state: %#v", tx)
	}
}

func TestManagerRollsBackFailedWork(t *testing.T) {
	tx := &fakeTx{}
	manager := Manager{DB: fakeBeginner{tx: tx}}
	expected := errors.New("failed")

	err := manager.Within(context.Background(), nil, func(Tx) error { return expected })
	if !errors.Is(err, expected) {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.committed || !tx.rolledBack {
		t.Fatalf("unexpected transaction state: %#v", tx)
	}
}
