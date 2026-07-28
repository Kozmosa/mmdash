// Package transaction coordinates database work within one PostgreSQL transaction.
package transaction

import (
	"context"
	"database/sql"
	"fmt"
)

// Tx is the SQL transaction surface exposed to application services.
type Tx interface {
	Commit() error
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
	Rollback() error
}

// Beginner starts SQL transactions.
type Beginner interface {
	Begin(context.Context, *sql.TxOptions) (Tx, error)
}

// SQLBeginner adapts database/sql for Manager.
type SQLBeginner struct {
	DB *sql.DB
}

// Begin starts a database/sql transaction.
func (beginner SQLBeginner) Begin(ctx context.Context, options *sql.TxOptions) (Tx, error) {
	return beginner.DB.BeginTx(ctx, options)
}

// Manager provides commit and rollback semantics.
type Manager struct {
	DB Beginner
}

// Within executes work and commits only on success.
func (manager Manager) Within(
	ctx context.Context,
	options *sql.TxOptions,
	work func(Tx) error,
) error {
	tx, err := manager.DB.Begin(ctx, options)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := work(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("rollback transaction after %v: %w", err, rollbackErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
