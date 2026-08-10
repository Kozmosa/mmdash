package model

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresManualSyncReusesActiveTaskAndResetsCountdown(t *testing.T) {
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	generator := identity.Generator{}
	userID := generator.MustNew()
	projectID := generator.MustNew()
	sourceID := generator.MustNew()
	firstSyncID := generator.MustNew()
	secondSyncID := generator.MustNew()
	firstClick := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	secondClick := firstClick.Add(30 * time.Second)
	firstNext := firstClick.Add(5 * time.Minute)
	secondNext := secondClick.Add(5 * time.Minute)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(
			user_id,email,display_name,password_hash,status,created_at,updated_at
		) VALUES($1,$2,'Model Sync Owner','test','active',$3,$3)
	`, userID, userID+"@test.local", firstClick); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,created_at,updated_at)
		VALUES($1,'Model sync integration',$2,$3,$3)
	`, projectID, userID, firstClick); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO model_sources(
			source_id,project_id,notion_root_page_id,notion_root_page_url,
			auto_sync_enabled,auto_sync_interval_seconds,next_sync_at,
			created_by,updated_by,created_at,updated_at
		) VALUES($1,$2,$3,'https://notion.site/model-root',TRUE,300,$4,$5,$5,$6,$6)
	`, sourceID, projectID, generator.MustNew(), firstNext, userID, firstClick); err != nil {
		t.Fatalf("insert model source: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := db.ExecContext(context.Background(), `DELETE FROM projects WHERE project_id=$1`, projectID); cleanupErr != nil {
			t.Errorf("delete project: %v", cleanupErr)
		}
		if _, cleanupErr := db.ExecContext(context.Background(), `DELETE FROM auth_users WHERE user_id=$1`, userID); cleanupErr != nil {
			t.Errorf("delete user: %v", cleanupErr)
		}
	})

	transactionManager := transaction.Manager{DB: transaction.SQLBeginner{DB: db}}
	firstClock := clock.Fixed{Time: firstClick}
	jobStore := jobs.PostgresStore{
		Clock:       firstClock,
		DB:          db,
		Generator:   generator,
		Outbox:      outbox.Writer{Clock: firstClock, Generator: generator},
		Transaction: transactionManager,
	}
	store := PostgresStore{
		DB:          db,
		Generator:   generator,
		Jobs:        jobStore,
		Outbox:      outbox.Writer{Clock: firstClock, Generator: generator},
		Transaction: transactionManager,
	}
	first := Sync{
		ID: firstSyncID, ProjectID: projectID, SourceID: sourceID,
		Scope: SyncScopeSource, Trigger: SyncTriggerManual, Status: SyncQueued,
		RequestedBy: userID, RequestedAt: firstClick, UpdatedAt: firstClick,
	}
	firstJob := jobs.CreateInput{
		ProjectID: projectID, JobType: JobTypeDiscover,
		Payload:        map[string]interface{}{"mode": "discover", "sync_id": firstSyncID},
		IdempotencyKey: "model-sync-" + firstSyncID, MaxAttempts: 3, TimeoutSeconds: 900,
	}
	created, err := store.CreateSync(ctx, first, firstJob, &firstNext)
	if err != nil {
		t.Fatalf("create first manual sync: %v", err)
	}
	if created.ID != firstSyncID || created.JobID == "" {
		t.Fatalf("created sync = %#v", created)
	}

	second := first
	second.ID = secondSyncID
	second.RequestedAt = secondClick
	second.UpdatedAt = secondClick
	secondJob := firstJob
	secondJob.IdempotencyKey = "model-sync-" + secondSyncID
	secondJob.Payload = map[string]interface{}{"mode": "discover", "sync_id": secondSyncID}
	reused, err := store.CreateSync(ctx, second, secondJob, &secondNext)
	if err != nil {
		t.Fatalf("reuse active manual sync: %v", err)
	}
	if reused.ID != created.ID || reused.JobID != created.JobID {
		t.Fatalf("active sync was not reused: first=%#v second=%#v", created, reused)
	}

	var storedNext time.Time
	if err := db.QueryRowContext(ctx, `SELECT next_sync_at FROM model_sources WHERE source_id=$1`, sourceID).Scan(&storedNext); err != nil {
		t.Fatalf("read reset countdown: %v", err)
	}
	if !storedNext.Equal(secondNext) {
		t.Fatalf("next_sync_at = %s, want %s", storedNext, secondNext)
	}
	var syncCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM model_syncs WHERE source_id=$1`, sourceID).Scan(&syncCount); err != nil {
		t.Fatalf("count syncs: %v", err)
	}
	if syncCount != 1 {
		t.Fatalf("sync count = %d, want 1 active sync", syncCount)
	}
}
