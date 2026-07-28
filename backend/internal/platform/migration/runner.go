// Package migration applies ordered PostgreSQL schema migrations.
package migration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// EventLogger records applied migrations.
type EventLogger interface {
	Info(string, map[string]interface{})
}

// Runner applies *.up.sql files exactly once.
type Runner struct {
	DB        *sql.DB
	Directory string
	Logger    EventLogger
}

// Run applies pending migrations under a PostgreSQL advisory lock.
func (runner Runner) Run(ctx context.Context) error {
	connection, err := runner.DB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve migration connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "SELECT pg_advisory_lock(hashtext('mmdash:migrations'))"); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		unlockContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = connection.ExecContext(unlockContext, "SELECT pg_advisory_unlock(hashtext('mmdash:migrations'))")
	}()

	if _, err := connection.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS system_schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	names, err := migrationNames(runner.Directory)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := runner.apply(ctx, connection, name); err != nil {
			return err
		}
	}
	return nil
}

func (runner Runner) apply(ctx context.Context, connection *sql.Conn, name string) error {
	var exists bool
	if err := connection.QueryRowContext(
		ctx,
		"SELECT EXISTS (SELECT 1 FROM system_schema_migrations WHERE version = $1)",
		name,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check migration %s: %w", name, err)
	}
	if exists {
		return nil
	}

	contents, err := os.ReadFile(filepath.Join(runner.Directory, name))
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, string(contents)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("execute migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		"INSERT INTO system_schema_migrations (version) VALUES ($1)",
		name,
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	if runner.Logger != nil {
		runner.Logger.Info("migration.applied", map[string]interface{}{"version": name})
	}
	return nil
}

func migrationNames(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
