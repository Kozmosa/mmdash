package main

import (
	"context"
	"database/sql"
	"os"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/logging"
	"github.com/mmdash/mmdash/backend/internal/platform/migration"

	_ "github.com/jackc/pgx/v4/stdlib"
)

func main() {
	logger := logging.New(os.Stderr, clock.System{})
	if err := run(logger); err != nil {
		logger.Error("migration.failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
}

func run(logger *logging.Logger) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return &configurationError{"DATABASE_URL is required"}
	}
	migrationsDirectory := envOrDefault("MIGRATIONS_DIR", "backend/migrations")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	return (migration.Runner{
		DB:        db,
		Directory: migrationsDirectory,
		Logger:    logger,
	}).Run(ctx)
}

type configurationError struct {
	message string
}

func (err *configurationError) Error() string {
	return err.message
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
