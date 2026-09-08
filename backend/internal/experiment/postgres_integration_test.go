package experiment

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
			token_id,user_id,kind,name,token_hash,created_at
		) VALUES($1,$2,'box','Experiment Lifecycle Box',$3,$4)
	`, tokenID, userID, strings.ReplaceAll(generator.MustNew(), "-", "")+strings.Repeat("b", 10), now); err != nil {
		t.Fatalf("insert Box token: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO box_nodes(
			box_id,legacy_project_id,owner_user_id,installation_id,name,status,version,
			capabilities,runtimes,limits,load,token_id,idempotency_key,
			last_heartbeat_at,created_by,created_at,updated_at
		) VALUES(
			$1,$2,$3,$4,'Experiment Lifecycle Box','online','test',
			'[{"name":"sandbox","version":"1"}]'::jsonb,
			'[{"name":"local-docker","version":"1"}]'::jsonb,
			'{"cpu_millis":1000,"memory_bytes":268435456,"timeout_seconds":90,"disk_bytes":1073741824,"pids":64,"network":"disabled"}'::jsonb,
			'{"running_tasks":1,"capacity":1,"cpu_millis":0,"memory_bytes":0}'::jsonb,
			$5,'experiment-lifecycle-box',$6,$7,$8,$8
		)
	`, boxID, projectID, userID, generator.MustNew(), tokenID, now, userID, now); err != nil {
		t.Fatalf("insert Box: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO experiments(
			experiment_id,project_id,created_by,name,execution_status,source_commit,entrypoint,
			parameters,environment,inputs,requested_runtime_policy,limits,idempotency_key,
			max_attempts,task_id,resource_usage,result_directory,created_at,updated_at
		) VALUES(
			$1,$2,$3,'Experiment Lifecycle','queued',$4,'python:run.py',
			'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'local-docker',
			'{"cpu_millis":1000,"memory_bytes":268435456,"timeout_seconds":90,"disk_bytes":1073741824,"pids":64,"network":"disabled"}'::jsonb,
			'experiment-lifecycle',1,$5,'{}'::jsonb,$6,$7,$7
		)
	`, experimentID, projectID, userID, strings.Repeat("a", 40), taskID,
		fmt.Sprintf("experiments/%s_%s/", experimentID, now.Format("20060102_1504")), now); err != nil {
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
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM box_tasks WHERE task_id=$1`, taskID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM experiments WHERE experiment_id=$1`, experimentID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM box_nodes WHERE box_id=$1`, boxID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_tokens WHERE token_id=$1`, tokenID)
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
	// Phase transitions are machine-driven (Box callbacks), so they emit
	// outbox events only; audit trails are reserved for human mutations.
	var phaseCount, startedCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_outbox WHERE project_id=$1 AND event_type='experiment.phase_changed'`, projectID).Scan(&phaseCount); err != nil {
		t.Fatalf("count phase Outbox event: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_outbox WHERE project_id=$1 AND event_type='experiment.started'`, projectID).Scan(&startedCount); err != nil {
		t.Fatalf("count started Outbox event: %v", err)
	}
	if phaseCount != 1 || startedCount != 1 {
		t.Fatalf("lifecycle side effects: phase_changed=%d started=%d", phaseCount, startedCount)
	}
}

func TestPostgresUpdateSettingsEmitsEventAndAudit(t *testing.T) {
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
	now := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(
			user_id,email,display_name,password_hash,status,created_at,updated_at
		) VALUES($1,$2,'Experiment Settings Integration','test','active',$3,$3)
	`, userID, userID+"@experiment-settings.test", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,created_at,updated_at)
		VALUES($1,'Experiment Settings Integration',$2,$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM experiment_project_settings WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})

	fixedClock := clock.Fixed{Time: now}
	auditRecorder := audit.Recorder{
		Clock: fixedClock, Metrics: metrics.New("core-test", "test"),
		Store: audit.PostgresStore{Clock: fixedClock, DB: db, Generator: generator},
	}
	manager := transaction.Manager{DB: transaction.SQLBeginner{DB: db}}
	store := PostgresStore{
		Audit: auditRecorder, DB: db, Generator: generator,
		Outbox: outbox.Writer{Clock: fixedClock, Generator: generator}, Transaction: manager,
	}

	timezone := "Asia/Shanghai"
	policy := "local-docker"
	threshold := int64(104857600)
	requestContext := requestctx.WithValues(ctx, requestctx.Values{RequestID: generator.MustNew()})
	updated, err := store.UpdateSettings(requestContext, projectID, userID, SettingsPatch{
		Timezone: &timezone, DefaultRuntimePolicy: &policy,
		DefaultLimits: &ResourceLimits{
			CPUMillis: 2000, MemoryBytes: 1 << 30, TimeoutSecond: 1800,
			DiskBytes: 10 << 30, PIDs: 128, Network: "disabled",
		},
		GitLargeFileThresholdBytes: &threshold,
	}, now)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if updated.Timezone != timezone || updated.DefaultRuntimePolicy != policy ||
		updated.DefaultLimits.CPUMillis != 2000 || updated.GitLargeFileThresholdBytes != threshold ||
		updated.UpdatedBy != userID {
		t.Fatalf("unexpected updated settings: %#v", updated)
	}

	var auditCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE project_id=$1 AND action='experiment.settings.updated' AND resource_type='experiment_settings'`, projectID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("settings audit: count=%d err=%v", auditCount, err)
	}
	var payload string
	if err := db.QueryRowContext(ctx, `SELECT payload::text FROM system_outbox WHERE project_id=$1 AND event_type='experiment.settings.updated'`, projectID).Scan(&payload); err != nil {
		t.Fatalf("settings outbox event: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("decode outbox payload: %v", err)
	}
	limits, _ := decoded["default_limits"].(map[string]interface{})
	if decoded["resource_type"] != "experiment_settings" ||
		decoded["timezone"] != "Asia/Shanghai" ||
		decoded["default_runtime_policy"] != "local-docker" ||
		decoded["git_large_file_threshold_bytes"] != float64(104857600) ||
		limits["cpu_millis"] != float64(2000) || limits["pids"] != float64(128) {
		t.Fatalf("unexpected outbox payload: %s", payload)
	}
}
