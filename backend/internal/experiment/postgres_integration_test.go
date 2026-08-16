package experiment

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/boxcontrol"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresApplyTaskStatusPersistsExperimentLifecycle(t *testing.T) {
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
	tokenID := generator.MustNew()
	boxID := generator.MustNew()
	experimentID := generator.MustNew()
	taskID := generator.MustNew()
	now := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(
			user_id,email,display_name,password_hash,status,created_at,updated_at
		) VALUES($1,$2,'Experiment Lifecycle Integration','test','active',$3,$3)
	`, userID, userID+"@experiment-lifecycle.test", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,created_at,updated_at)
		VALUES($1,'Experiment Lifecycle Integration',$2,$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_tokens(
			token_id,user_id,project_id,kind,name,token_hash,created_at
		) VALUES($1,$2,$3,'box','Experiment Lifecycle Box',$4,$5)
	`, tokenID, userID, projectID, strings.Repeat("b", 64), now); err != nil {
		t.Fatalf("insert Box token: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO box_nodes(
			box_id,project_id,name,status,version,capabilities,runtimes,limits,
			load,token_id,idempotency_key,last_heartbeat_at,created_by,created_at,updated_at
		) VALUES(
			$1,$2,'Experiment Lifecycle Box','online','test',
			'[{"name":"sandbox","version":"1"}]'::jsonb,
			'[{"name":"local-docker","version":"1"}]'::jsonb,
			'{"cpu_millis":1000,"memory_bytes":268435456,"timeout_seconds":90,"disk_bytes":1073741824,"pids":64,"network":"disabled"}'::jsonb,
			'{"running_tasks":1,"capacity":1,"cpu_millis":0,"memory_bytes":0}'::jsonb,
			$3,'experiment-lifecycle-box',$4,$5,$4,$4
		)
	`, boxID, projectID, tokenID, now, userID); err != nil {
		t.Fatalf("insert Box: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO experiments(
			experiment_id,project_id,created_by,name,status,source_commit,entrypoint,
			parameters,environment,inputs,runtime,limits,idempotency_key,max_attempts,
			task_id,resource_usage,created_at,updated_at
		) VALUES(
			$1,$2,$3,'Experiment Lifecycle','queued',$4,'python:run.py',
			'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'local-docker',
			'{"cpu_millis":1000,"memory_bytes":268435456,"timeout_seconds":90,"disk_bytes":1073741824,"pids":64,"network":"disabled"}'::jsonb,
			'experiment-lifecycle',1,$5,'{}'::jsonb,$6,$6
		)
	`, experimentID, projectID, userID, strings.Repeat("a", 40), taskID, now); err != nil {
		t.Fatalf("insert Experiment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO box_tasks(
			task_id,experiment_id,project_id,box_id,status,attempt,max_attempts,
			run_spec,lease_expires_at,resource_usage,created_at,started_at,updated_at
		) VALUES($1,$2,$3,$4,'preparing',1,1,'{}'::jsonb,$5,'{}'::jsonb,$6,$6,$6)
	`, taskID, experimentID, projectID, boxID, now.Add(time.Minute), now); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})

	fixedClock := clock.Fixed{Time: now}
	auditStore := audit.PostgresStore{Clock: fixedClock, DB: db, Generator: generator}
	auditRecorder := audit.Recorder{
		Clock: fixedClock, Metrics: metrics.New("core-test", "test"), Store: auditStore,
	}
	manager := transaction.Manager{DB: transaction.SQLBeginner{DB: db}}
	store := PostgresStore{
		Audit: auditRecorder, DB: db, Generator: generator,
		Outbox: outbox.Writer{Clock: fixedClock, Generator: generator}, Transaction: manager,
	}
	requestContext := requestctx.WithValues(ctx, requestctx.Values{RequestID: generator.MustNew()})
	requestctx.SetActor(requestContext, userID, "box")
	requestctx.SetProject(requestContext, projectID)
	task := boxcontrol.Task{
		ID: taskID, ExperimentID: experimentID, ProjectID: projectID, BoxID: boxID,
		Status: boxcontrol.TaskPreparing, ResourceUsage: map[string]interface{}{},
	}

	updated, err := store.ApplyTaskStatus(requestContext, task, now)
	if err != nil {
		t.Fatalf("apply preparing task status: %v", err)
	}
	if updated.ExecutionStatus != StatusPreparing || updated.BoxID != boxID || updated.StartedAt == nil {
		t.Fatalf("unexpected preparing Experiment: %#v", updated)
	}
	var auditCount, outboxCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE project_id=$1 AND action='experiment.preparing'`, projectID).Scan(&auditCount); err != nil {
		t.Fatalf("count lifecycle Audit: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_outbox WHERE project_id=$1 AND event_type='experiment.started'`, projectID).Scan(&outboxCount); err != nil {
		t.Fatalf("count lifecycle Outbox event: %v", err)
	}
	if auditCount != 1 || outboxCount != 1 {
		t.Fatalf("lifecycle side effects: audit=%d outbox=%d", auditCount, outboxCount)
	}
}
