package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mmdash/mmdash/backend/internal/platform/config"

	_ "github.com/jackc/pgx/v4/stdlib"
)

// OpenPostgres opens and verifies the shared PostgreSQL connection pool.
func OpenPostgres(ctx context.Context, databaseConfig config.DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseConfig.URL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetConnMaxIdleTime(databaseConfig.ConnMaxIdleTime)
	db.SetConnMaxLifetime(databaseConfig.ConnMaxLifetime)
	db.SetMaxIdleConns(databaseConfig.MaxIdleConns)
	db.SetMaxOpenConns(databaseConfig.MaxOpenConns)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return db, nil
}

// Checker exposes the PostgreSQL readiness boundary.
type Checker struct {
	DB *sql.DB
}

// Name returns the dependency name used in readiness output.
func (Checker) Name() string {
	return "postgres"
}

// Check verifies PostgreSQL is reachable.
func (checker Checker) Check(ctx context.Context) error {
	return checker.DB.PingContext(ctx)
}
