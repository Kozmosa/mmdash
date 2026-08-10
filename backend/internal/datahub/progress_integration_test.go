package datahub

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestProgressTaskDeletionHidesDataHubProjection(t *testing.T) {
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	generator := identity.Generator{}
	userID := generator.MustNew()
	projectID := generator.MustNew()
	taskID := generator.MustNew()
	deleteFirstTaskID := generator.MustNew()
	milestoneID := generator.MustNew()
	now := time.Date(2026, time.August, 6, 7, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES($1,$2,'Data Hub Progress Test','test','active',$3,$3)`, userID, userID+"@datahub-progress.test", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Data Hub Progress Test',$2,$3,$3)`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES($1,$2,'owner',$3,$3)`, projectID, userID, now); err != nil {
		t.Fatalf("insert membership: %v", err)
	}

	store := PostgresStore{
		Clock:       clock.Fixed{Time: now.Add(10 * time.Minute)},
		DB:          db,
		Generator:   generator,
		Transaction: transaction.Manager{DB: transaction.SQLBeginner{DB: db}},
	}
	created := progressProjectionEvent(generator.MustNew(), "progress.task.created", projectID, taskID, "Task before deletion", "todo", now)
	if err := store.ProjectProgress(ctx, created); err != nil {
		t.Fatalf("project created task: %v", err)
	}
	page, err := store.ListObjects(ctx, projectID, "task", pagination.Request{Limit: 20})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list visible task: page=%#v err=%v", page, err)
	}
	objectID := page.Items[0].ID
	if _, err := store.GetObject(ctx, projectID, objectID); err != nil {
		t.Fatalf("read visible task projection: %v", err)
	}

	deleted := progressProjectionEvent(generator.MustNew(), "progress.task.deleted", projectID, taskID, "Task before deletion", "deleted", now.Add(2*time.Minute))
	if err := store.ProjectProgress(ctx, deleted); err != nil {
		t.Fatalf("project deleted task: %v", err)
	}
	page, err = store.ListObjects(ctx, projectID, "task", pagination.Request{Limit: 20})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("deleted task remained visible in list: page=%#v err=%v", page, err)
	}
	if _, err := store.GetObject(ctx, projectID, objectID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted task direct read: got %v, want not found", err)
	}
	assertProgressProjection(t, ctx, db, taskID, "hidden", "Task before deletion", "progress.task.deleted", 2, deleted.OccurredAt)

	if err := store.ProjectProgress(ctx, deleted); err != nil {
		t.Fatalf("replay deleted task: %v", err)
	}
	assertProgressProjection(t, ctx, db, taskID, "hidden", "Task before deletion", "progress.task.deleted", 2, deleted.OccurredAt)
	assertProgressActivityCount(t, ctx, db, deleted.EventID, 1)

	deleteFirst := progressProjectionEvent(generator.MustNew(), "progress.task.deleted", projectID, deleteFirstTaskID, "Delete-first task", "deleted", now.Add(6*time.Minute))
	if err := store.ProjectProgress(ctx, deleteFirst); err != nil {
		t.Fatalf("project delete-first task: %v", err)
	}
	deleteFirstObjectID := progressProjectionObjectID(t, ctx, db, deleteFirstTaskID)
	if _, err := store.GetObject(ctx, projectID, deleteFirstObjectID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete-first task direct read: got %v, want not found", err)
	}
	staleCreated := progressProjectionEvent(generator.MustNew(), "progress.task.created", projectID, deleteFirstTaskID, "Stale create", "todo", now.Add(3*time.Minute))
	staleUpdated := progressProjectionEvent(generator.MustNew(), "progress.task.updated", projectID, deleteFirstTaskID, "Stale update", "in_progress", now.Add(4*time.Minute))
	if err := store.ProjectProgress(ctx, staleCreated); err != nil {
		t.Fatalf("project stale task create: %v", err)
	}
	if err := store.ProjectProgress(ctx, staleUpdated); err != nil {
		t.Fatalf("project stale task update: %v", err)
	}
	assertProgressProjection(t, ctx, db, deleteFirstTaskID, "hidden", "Delete-first task", "progress.task.deleted", 3, deleteFirst.OccurredAt)
	page, err = store.ListObjects(ctx, projectID, "task", pagination.Request{Limit: 20})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("stale task event revived tombstone: page=%#v err=%v", page, err)
	}

	milestone := progressProjectionEvent(generator.MustNew(), "progress.milestone.created", projectID, milestoneID, "Visible milestone", "planned", now.Add(7*time.Minute))
	milestone.Payload["resource_type"] = "milestone"
	if err := store.ProjectProgress(ctx, milestone); err != nil {
		t.Fatalf("project milestone: %v", err)
	}
	page, err = store.ListObjects(ctx, projectID, "", pagination.Request{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].SourceID != milestoneID || page.Items[0].Status != "planned" {
		t.Fatalf("other Progress projection changed: page=%#v err=%v", page, err)
	}
	if _, err := store.GetObject(ctx, projectID, page.Items[0].ID); err != nil {
		t.Fatalf("read visible milestone projection: %v", err)
	}

	evaluationID := generator.MustNew()
	evaluation := progressProjectionEvent(generator.MustNew(), "progress.evaluation.completed", projectID, evaluationID, "Progress evaluation", "succeeded", now.Add(8*time.Minute))
	evaluation.Payload["resource_type"] = "progress_evaluation"
	evaluation.Payload["stage"] = "execution"
	if err := store.ProjectProgress(ctx, evaluation); err != nil {
		t.Fatalf("project Progress evaluation: %v", err)
	}
	riskID := generator.MustNew()
	risk := progressProjectionEvent(generator.MustNew(), "progress.risk.detected", projectID, riskID, "Deadline risk", "open", now.Add(9*time.Minute))
	risk.Payload["resource_type"] = "progress_risk"
	risk.Payload["evaluation_id"] = evaluationID
	risk.Payload["severity"] = "high"
	if err := store.ProjectProgress(ctx, risk); err != nil {
		t.Fatalf("project Progress risk: %v", err)
	}
	for objectType, sourceID := range map[string]string{"progress_evaluation": evaluationID, "progress_risk": riskID} {
		projected, listErr := store.ListObjects(ctx, projectID, objectType, pagination.Request{Limit: 20})
		if listErr != nil || len(projected.Items) != 1 || projected.Items[0].SourceID != sourceID {
			t.Fatalf("list %s projection: page=%#v err=%v", objectType, projected, listErr)
		}
	}
}

func progressProjectionEvent(eventID, eventType, projectID, resourceID, title, status string, occurredAt time.Time) contract.EventEnvelope {
	return contract.EventEnvelope{
		Actor:         map[string]string{"user_id": "projection-test"},
		EventID:       eventID,
		EventType:     eventType,
		OccurredAt:    occurredAt,
		Payload:       map[string]interface{}{"resource_id": resourceID, "resource_type": "task", "source": "human", "status": status, "title": title},
		Producer:      "progress",
		ProjectID:     &projectID,
		SchemaVersion: 1,
	}
}

func assertProgressProjection(t *testing.T, ctx context.Context, db *sql.DB, sourceID, wantStatus, wantTitle, wantSummary string, wantVersion int64, wantOccurredAt time.Time) {
	t.Helper()
	var status, title, summary string
	var version int64
	var occurredAt time.Time
	if err := db.QueryRowContext(ctx, `SELECT status,title,summary,version,occurred_at FROM data_objects WHERE source_module='progress' AND object_type='task' AND source_id=$1`, sourceID).Scan(&status, &title, &summary, &version, &occurredAt); err != nil {
		t.Fatalf("read raw Progress projection: %v", err)
	}
	if status != wantStatus || title != wantTitle || summary != wantSummary || version != wantVersion || !occurredAt.Equal(wantOccurredAt) {
		t.Fatalf("Progress projection: status=%q title=%q summary=%q version=%d occurred_at=%s", status, title, summary, version, occurredAt)
	}
}

func assertProgressActivityCount(t *testing.T, ctx context.Context, db *sql.DB, eventID string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM data_activity WHERE event_id=$1`, eventID).Scan(&count); err != nil {
		t.Fatalf("count Progress activity: %v", err)
	}
	if count != want {
		t.Fatalf("Progress activity count: got %d, want %d", count, want)
	}
}

func progressProjectionObjectID(t *testing.T, ctx context.Context, db *sql.DB, sourceID string) string {
	t.Helper()
	var objectID string
	if err := db.QueryRowContext(ctx, `SELECT object_id FROM data_objects WHERE source_module='progress' AND object_type='task' AND source_id=$1`, sourceID).Scan(&objectID); err != nil {
		t.Fatalf("read Progress projection object ID: %v", err)
	}
	return objectID
}
