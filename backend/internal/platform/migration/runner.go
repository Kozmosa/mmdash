// Package migration applies ordered PostgreSQL schema migrations.
package migration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var migrationNamePattern = regexp.MustCompile(`^(\d{6})_[a-z0-9]+(?:_[a-z0-9]+)*\.up\.sql$`)

// legacyMigrationAliases is an immutable compatibility ledger for migration
// filenames that were applied by development or merged branches before their
// canonical sequence numbers were assigned. The runner records the canonical
// name when an alias is present; it never executes the migration a second time.
var legacyMigrationAliases = map[string][]string{
	"000026_agent_sessions.up.sql": {
		"000023_agent_sessions.up.sql",
	},
	"000029_model_stage7.up.sql": {
		"000022_model_stage7.up.sql",
	},
	"000030_model_notion_oauth.up.sql": {
		"000023_model_notion_oauth.up.sql",
	},
	"000031_notification_routing_model.up.sql": {
		"000023_notification_routing_model.up.sql",
		"000024_notification_routing_model.up.sql",
	},
}

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
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	rollback := func() {
		_ = tx.Rollback()
	}

	var exists bool
	if err := tx.QueryRowContext(
		ctx,
		"SELECT EXISTS (SELECT 1 FROM system_schema_migrations WHERE version = $1)",
		name,
	).Scan(&exists); err != nil {
		rollback()
		return fmt.Errorf("check migration %s: %w", name, err)
	}
	if exists {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration check %s: %w", name, err)
		}
		return nil
	}

	for _, legacyName := range legacyMigrationAliases[name] {
		if err := tx.QueryRowContext(
			ctx,
			"SELECT EXISTS (SELECT 1 FROM system_schema_migrations WHERE version = $1)",
			legacyName,
		).Scan(&exists); err != nil {
			rollback()
			return fmt.Errorf("check legacy migration %s for %s: %w", legacyName, name, err)
		}
		if !exists {
			continue
		}
		if _, err := tx.ExecContext(
			ctx,
			"INSERT INTO system_schema_migrations (version) VALUES ($1)",
			name,
		); err != nil {
			rollback()
			return fmt.Errorf("reconcile migration %s from %s: %w", name, legacyName, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration reconciliation %s from %s: %w", name, legacyName, err)
		}
		if runner.Logger != nil {
			runner.Logger.Info("migration.reconciled", map[string]interface{}{
				"legacy_version": legacyName,
				"version":        name,
			})
		}
		return nil
	}

	contents, err := os.ReadFile(filepath.Join(runner.Directory, name))
	if err != nil {
		rollback()
		return fmt.Errorf("read migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, string(contents)); err != nil {
		rollback()
		return fmt.Errorf("execute migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		"INSERT INTO system_schema_migrations (version) VALUES ($1)",
		name,
	); err != nil {
		rollback()
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
			if !migrationNamePattern.MatchString(entry.Name()) {
				return nil, fmt.Errorf("invalid migration filename %s", entry.Name())
			}
			downName := strings.TrimSuffix(entry.Name(), ".up.sql") + ".down.sql"
			if _, err := os.Stat(filepath.Join(directory, downName)); err != nil {
				if os.IsNotExist(err) {
					return nil, fmt.Errorf("migration %s is missing %s", entry.Name(), downName)
				}
				return nil, fmt.Errorf("check down migration %s: %w", downName, err)
			}
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for index, name := range names {
		matches := migrationNamePattern.FindStringSubmatch(name)
		number, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, fmt.Errorf("parse migration number %s: %w", name, err)
		}
		expected := index + 1
		if number != expected {
			return nil, fmt.Errorf(
				"migration sequence must be unique and continuous: %s has number %06d, want %06d",
				name,
				number,
				expected,
			)
		}
	}
	return names, nil
}
