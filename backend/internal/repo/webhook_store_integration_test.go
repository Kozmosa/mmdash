package repo

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresStoreRecordWebhookPersistsProcessedTimestamp(t *testing.T) {
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	_, err = db.ExecContext(ctx, `
		CREATE TEMP TABLE repo_webhook_deliveries (
			provider TEXT NOT NULL,
			delivery_id TEXT NOT NULL,
			repository_id UUID NOT NULL,
			event_name TEXT NOT NULL,
			ref_name TEXT,
			before_sha TEXT,
			after_sha TEXT,
			payload_sha256 TEXT NOT NULL,
			status TEXT NOT NULL,
			received_at TIMESTAMPTZ NOT NULL,
			processed_at TIMESTAMPTZ,
			PRIMARY KEY (provider, delivery_id)
		)
	`)
	if err != nil {
		t.Fatalf("create temporary webhook table: %v", err)
	}

	receivedAt := time.Date(2026, time.September, 4, 15, 43, 15, 0, time.UTC)
	store := PostgresStore{
		DB: db,
		Transaction: transaction.Manager{
			DB: transaction.SQLBeginner{DB: db},
		},
	}
	duplicate, err := store.RecordWebhook(ctx, WebhookDelivery{
		DeliveryID:   "integration-delivery",
		Event:        "ping",
		PayloadSHA:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ReceivedAt:   receivedAt,
		RepositoryID: "00000000-0000-4000-8000-000000000011",
		Status:       "processed",
	})
	if err != nil {
		t.Fatalf("record processed webhook: %v", err)
	}
	if duplicate {
		t.Fatal("first webhook delivery was reported as duplicate")
	}

	var processedAt time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT processed_at
		FROM repo_webhook_deliveries
		WHERE provider = 'github' AND delivery_id = 'integration-delivery'
	`).Scan(&processedAt); err != nil {
		t.Fatalf("read processed webhook timestamp: %v", err)
	}
	if !processedAt.Equal(receivedAt) {
		t.Fatalf("processed_at = %s, want %s", processedAt, receivedAt)
	}
}
