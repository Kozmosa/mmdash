package progress

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresProgressTrackingDebounceDedupAndLeaseRecovery(t *testing.T) {
	fixture := newTrackingPostgresFixture(t)
	firstEventID := fixture.generator.MustNew()
	first, err := fixture.store.ScheduleRequest(fixture.ctx, fixture.projectID, fixture.userID, "system", "event", false, EvaluationTrigger{
		TriggerType: "repo.commit.created", SourceEventID: firstEventID, OccurredAt: fixture.clock.Now(), Payload: map[string]interface{}{"commit_sha": "a"},
	})
	if err != nil || first.Merged || !first.ScheduledFor.Equal(fixture.clock.Now().Add(time.Minute)) {
		t.Fatalf("schedule first event: result=%#v err=%v", first, err)
	}
	fixture.clock.Advance(10 * time.Second)
	second, err := fixture.store.ScheduleRequest(fixture.ctx, fixture.projectID, fixture.userID, "system", "event", false, EvaluationTrigger{
		TriggerType: "model.snapshot.created", SourceEventID: fixture.generator.MustNew(), OccurredAt: fixture.clock.Now(), Payload: map[string]interface{}{"content_hash": "b"},
	})
	if err != nil || !second.Merged || second.RequestID != first.RequestID || !second.ScheduledFor.Equal(fixture.clock.Now().Add(time.Minute)) {
		t.Fatalf("merge debounced event: result=%#v err=%v", second, err)
	}
	replayed, err := fixture.store.ScheduleRequest(fixture.ctx, fixture.projectID, fixture.userID, "system", "event", false, EvaluationTrigger{
		TriggerType: "repo.commit.created", SourceEventID: firstEventID, OccurredAt: fixture.clock.Now(), Payload: map[string]interface{}{"commit_sha": "a"},
	})
	if err != nil || !replayed.Merged || replayed.RequestID != first.RequestID || !replayed.ScheduledFor.Equal(second.ScheduledFor) {
		t.Fatalf("idempotent source event replay: result=%#v err=%v", replayed, err)
	}
	claim, err := fixture.store.ClaimRequest(fixture.ctx, "core-a", time.Second)
	if err != nil || claim != nil {
		t.Fatalf("request claimed before debounce: claim=%#v err=%v", claim, err)
	}
	fixture.clock.Advance(time.Minute)
	claim, err = fixture.store.ClaimRequest(fixture.ctx, "core-a", time.Second)
	if err != nil || claim == nil || claim.ID != first.RequestID {
		t.Fatalf("claim due request: claim=%#v err=%v", claim, err)
	}
	fixture.clock.Advance(2 * time.Second)
	recovered, err := fixture.store.ClaimRequest(fixture.ctx, "core-b", time.Minute)
	if err != nil || recovered == nil || recovered.ID != claim.ID || recovered.LeaseOwner != "core-b" {
		t.Fatalf("recover expired assembly lease: claim=%#v err=%v", recovered, err)
	}

	input := map[string]interface{}{"facts_schema_version": 1, "project": map[string]interface{}{"name": "stable"}}
	version, err := canonicalInputVersion(input)
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := fixture.store.FinalizeRequest(fixture.ctx, *recovered, input, version)
	if err != nil || evaluation == nil || evaluation.JobID == "" {
		t.Fatalf("finalize request: evaluation=%#v err=%v", evaluation, err)
	}
	job, err := fixture.jobs.Get(fixture.ctx, evaluation.JobID)
	if err != nil || job.JobType != EvaluationJobType || job.Status != jobs.StatusQueued {
		t.Fatalf("read queued evaluation Job: job=%#v err=%v", job, err)
	}

	activeManual, err := fixture.store.ScheduleRequest(fixture.ctx, fixture.projectID, fixture.userID, "session", "manual", true, EvaluationTrigger{TriggerType: "manual", OccurredAt: fixture.clock.Now(), Payload: map[string]interface{}{}})
	if err != nil || !activeManual.Merged || activeManual.RequestID != evaluation.RequestID {
		t.Fatalf("reuse active manual evaluation: result=%#v err=%v", activeManual, err)
	}
	if _, err := fixture.db.ExecContext(fixture.ctx, `UPDATE progress_evaluations SET status='succeeded' WHERE evaluation_id=$1`, evaluation.ID); err != nil {
		t.Fatal(err)
	}

	duplicate, err := fixture.store.ScheduleRequest(fixture.ctx, fixture.projectID, fixture.userID, "session", "manual", false, EvaluationTrigger{TriggerType: "manual", OccurredAt: fixture.clock.Now(), Payload: map[string]interface{}{}})
	if err != nil || duplicate.Merged {
		t.Fatalf("schedule unchanged non-forced input request: result=%#v err=%v", duplicate, err)
	}
	duplicateClaim, err := fixture.store.ClaimRequest(fixture.ctx, "core-c", time.Minute)
	if err != nil || duplicateClaim == nil {
		t.Fatalf("claim duplicate request: claim=%#v err=%v", duplicateClaim, err)
	}
	merged, err := fixture.store.FinalizeRequest(fixture.ctx, *duplicateClaim, input, version)
	if err != nil || merged != nil {
		t.Fatalf("deduplicate identical input: evaluation=%#v err=%v", merged, err)
	}
	var status, mergedInto string
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT status,COALESCE(merged_into_evaluation_id::text,'') FROM progress_evaluation_requests WHERE request_id=$1`, duplicate.RequestID).Scan(&status, &mergedInto); err != nil || status != "merged" || mergedInto != evaluation.ID {
		t.Fatalf("deduplicated request state: status=%q merged_into=%q err=%v", status, mergedInto, err)
	}

	forced, err := fixture.store.ScheduleRequest(fixture.ctx, fixture.projectID, fixture.userID, "session", "manual", true, EvaluationTrigger{TriggerType: "manual", OccurredAt: fixture.clock.Now(), Payload: map[string]interface{}{}})
	if err != nil || forced.Merged {
		t.Fatalf("schedule forced manual evaluation: result=%#v err=%v", forced, err)
	}
	forcedClaim, err := fixture.store.ClaimRequest(fixture.ctx, "core-d", time.Minute)
	if err != nil || forcedClaim == nil {
		t.Fatalf("claim forced manual evaluation: claim=%#v err=%v", forcedClaim, err)
	}
	forcedEvaluation, err := fixture.store.FinalizeRequest(fixture.ctx, *forcedClaim, input, version)
	if err != nil || forcedEvaluation == nil || forcedEvaluation.ID == evaluation.ID {
		t.Fatalf("force identical input into a new evaluation: evaluation=%#v err=%v", forcedEvaluation, err)
	}
	if _, err := fixture.db.ExecContext(fixture.ctx, `UPDATE jobs SET status='timed_out',finished_at=$2,updated_at=$2 WHERE job_id=$1`, forcedEvaluation.JobID, fixture.clock.Now()); err != nil {
		t.Fatal(err)
	}
	afterTimeout, err := fixture.store.ScheduleRequest(fixture.ctx, fixture.projectID, fixture.userID, "session", "manual", true, EvaluationTrigger{TriggerType: "manual", OccurredAt: fixture.clock.Now(), Payload: map[string]interface{}{}})
	if err != nil || afterTimeout.Merged {
		t.Fatalf("terminal Job must not block a new manual evaluation: result=%#v err=%v", afterTimeout, err)
	}
	var reconciledStatus, reconciledCode string
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT status,error_code FROM progress_evaluations WHERE evaluation_id=$1`, forcedEvaluation.ID).Scan(&reconciledStatus, &reconciledCode); err != nil || reconciledStatus != "failed" || reconciledCode != "PROGRESS_EVALUATION_JOB_TIMED_OUT" {
		t.Fatalf("terminal Job reconciliation: status=%q code=%q err=%v", reconciledStatus, reconciledCode, err)
	}
}

func TestPostgresProgressLocalCronSchedulesEvaluationRequest(t *testing.T) {
	fixture := newTrackingPostgresFixture(t)
	if _, err := fixture.db.ExecContext(fixture.ctx, `
		UPDATE progress_settings
		SET auto_tracking_enabled=true,cron_enabled=true,
		    cron_schedule='*/15 * * * *',cron_next_run_at=$2
		WHERE project_id=$1
	`, fixture.projectID, fixture.clock.Now()); err != nil {
		t.Fatal(err)
	}
	processor := TrackingProcessor{
		Owner: "core-local-cron", Store: fixture.store,
		Lease: time.Minute, RetryDelay: time.Minute,
	}
	if err := processor.ScheduleCronOnce(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	settings, err := fixture.store.GetSettings(fixture.ctx, fixture.projectID)
	if err != nil {
		t.Fatal(err)
	}
	wantNext := time.Date(2026, time.August, 10, 2, 15, 0, 0, time.UTC)
	if settings.CronLastScheduledAt == nil || !settings.CronLastScheduledAt.Equal(fixture.clock.Now()) ||
		settings.CronNextRunAt == nil || !settings.CronNextRunAt.Equal(wantNext) {
		t.Fatalf("local cron state not advanced: %#v", settings)
	}
	var requestCount, triggerCount int
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM progress_evaluation_requests WHERE project_id=$1 AND trigger_kind='cron'`, fixture.projectID).Scan(&requestCount); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM progress_evaluation_triggers WHERE project_id=$1 AND trigger_type='cron' AND payload->>'scheduler'='mmdash'`, fixture.projectID).Scan(&triggerCount); err != nil {
		t.Fatal(err)
	}
	if requestCount != 1 || triggerCount != 1 {
		t.Fatalf("local cron request evidence missing: requests=%d triggers=%d", requestCount, triggerCount)
	}
}

func TestPostgresProgressTrackingCompletionBoundariesOverridesAndRetry(t *testing.T) {
	fixture := newTrackingPostgresFixture(t)
	task, err := fixture.store.CreateTask(fixture.ctx, fixture.projectID, fixture.userID, CreateTaskInput{Title: "Human title", Status: TaskTodo}, "human")
	if err != nil {
		t.Fatal(err)
	}
	humanTitle := "Human corrected title"
	if _, err := fixture.store.UpdateTask(fixture.ctx, fixture.projectID, task.ID, fixture.userID, UpdateTaskInput{Title: &humanTitle}, "human"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.SetStageOverride(fixture.ctx, fixture.projectID, fixture.userID, "review", "Human review stage", "manual judgment"); err != nil {
		t.Fatal(err)
	}

	evaluation := fixture.queueEvaluation(t, map[string]interface{}{"version": "completion"})
	job, err := fixture.jobs.Get(fixture.ctx, evaluation.JobID)
	if err != nil {
		t.Fatal(err)
	}
	job.Attempts = 1
	job.Status = jobs.StatusRunning
	result := map[string]interface{}{
		"evaluator_mode": "mock",
		"output": map[string]interface{}{
			"stage": "execution", "summary": "Agent execution stage", "changes_since_last": []string{"commit"},
			"completed_items": []string{}, "in_progress_items": []string{"task"}, "blockers": []string{},
			"pending_questions": []string{"Confirm deadline"},
			"risks":             []interface{}{map[string]interface{}{"key": "deadline", "title": "Deadline risk", "severity": "high", "detail": "Schedule is tight"}},
			"work_state_updates": []interface{}{
				map[string]interface{}{"task_id": task.ID, "state": TaskInProgress},
			},
			"suggestions": []interface{}{
				map[string]interface{}{"key": "milestone", "proposal_type": "milestone.create", "title": "Final review", "rationale": "Needed", "changes": map[string]interface{}{"title": "Final review", "critical": true}},
				map[string]interface{}{"key": "task-create", "proposal_type": "task.create", "title": "Generated task", "rationale": "Observed work", "changes": map[string]interface{}{"title": "Generated task", "status": TaskTodo}},
				map[string]interface{}{"key": "task", "proposal_type": "task.update", "target_id": task.ID, "title": "Update task", "rationale": "Progress observed", "changes": map[string]interface{}{"title": "Agent title"}},
				map[string]interface{}{"key": "task-complete", "proposal_type": "task.complete", "target_id": task.ID, "title": "Mark task complete", "rationale": "Completion evidence observed", "changes": map[string]interface{}{}},
			},
		},
	}
	if err := fixture.store.Transaction.Within(fixture.ctx, nil, func(tx transaction.Tx) error {
		if err := fixture.store.MarkEvaluationRunning(fixture.ctx, tx, job); err != nil {
			return err
		}
		return fixture.store.CompleteEvaluation(fixture.ctx, tx, job, result)
	}); err != nil {
		t.Fatalf("complete evaluation: %v", err)
	}
	updated, err := fixture.store.GetTask(fixture.ctx, fixture.projectID, task.ID)
	if err != nil || updated.Title != humanTitle || updated.Status != TaskInProgress || updated.SourceEvaluationID != evaluation.ID || len(updated.ManualOverrideFields) != 1 || updated.ManualOverrideFields[0] != "title" {
		t.Fatalf("automatic work-state boundary: task=%#v err=%v", updated, err)
	}
	proposals, err := fixture.store.ListProposals(fixture.ctx, fixture.projectID)
	if err != nil || len(proposals) != 4 {
		t.Fatalf("review proposal boundary: proposals=%#v err=%v", proposals, err)
	}
	for _, proposal := range proposals {
		if proposal.SourceEvaluationID != evaluation.ID || proposal.Status != "pending" {
			t.Fatalf("evaluation suggestion bypassed review: proposal=%#v", proposal)
		}
	}
	state, err := fixture.store.GetState(fixture.ctx, fixture.projectID)
	if err != nil || state.DetectedStage != "execution" || state.EffectiveStage != "review" || !state.StageOverridden || state.Summary != "Human review stage" {
		t.Fatalf("stage override state: state=%#v err=%v", state, err)
	}
	completed, err := fixture.store.GetEvaluation(fixture.ctx, fixture.projectID, evaluation.ID)
	if err != nil || completed.Status != "succeeded" || completed.EvaluatorMode != "mock" || len(completed.Risks) != 1 {
		t.Fatalf("completed evaluation history: evaluation=%#v err=%v", completed, err)
	}
	cleared, err := fixture.store.ClearStageOverride(fixture.ctx, fixture.projectID, fixture.userID)
	if err != nil || cleared.Active || cleared.ClearedBy != fixture.userID || cleared.ClearedAt == nil {
		t.Fatalf("clear stage override: override=%#v err=%v", cleared, err)
	}
	state, err = fixture.store.GetState(fixture.ctx, fixture.projectID)
	if err != nil || state.EffectiveStage != "execution" || state.StageOverridden || state.Summary != "Agent execution stage" {
		t.Fatalf("cleared stage override state: state=%#v err=%v", state, err)
	}
	var clearedAuditCount, clearedEventCount int
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM audit_events WHERE project_id=$1 AND action='progress.stage.override.cleared'`, fixture.projectID).Scan(&clearedAuditCount); err != nil || clearedAuditCount != 1 {
		t.Fatalf("cleared stage override audit: count=%d err=%v", clearedAuditCount, err)
	}
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM system_outbox WHERE project_id=$1 AND event_type='progress.stage.override_cleared' AND payload->>'resource_id'=$2`, fixture.projectID, cleared.ID).Scan(&clearedEventCount); err != nil || clearedEventCount != 1 {
		t.Fatalf("cleared stage override outbox: count=%d err=%v", clearedEventCount, err)
	}
	for _, expected := range []struct {
		action     string
		payloadKey string
		count      int
	}{
		{action: "progress.evaluation.started", payloadKey: "resource_id", count: 1},
		{action: "progress.proposal.created", payloadKey: "source_evaluation_id", count: 4},
		{action: "progress.risk.detected", payloadKey: "evaluation_id", count: 1},
		{action: "progress.evaluation.completed", payloadKey: "resource_id", count: 1},
	} {
		var count int
		if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM audit_events WHERE project_id=$1 AND action=$2`, fixture.projectID, expected.action).Scan(&count); err != nil || count != expected.count {
			t.Fatalf("automatic mutation audit %s: count=%d err=%v", expected.action, count, err)
		}
		if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM system_outbox WHERE project_id=$1 AND event_type=$2 AND payload->>$3=$4`, fixture.projectID, expected.action, expected.payloadKey, evaluation.ID).Scan(&count); err != nil || count != expected.count {
			t.Fatalf("automatic mutation outbox %s: count=%d err=%v", expected.action, count, err)
		}
	}
	var workStateAudit, workStateEvent int
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM audit_events WHERE project_id=$1 AND action='progress.task.updated'`, fixture.projectID).Scan(&workStateAudit); err != nil || workStateAudit != 1 {
		t.Fatalf("automatic work-state audit: count=%d err=%v", workStateAudit, err)
	}
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM system_outbox WHERE project_id=$1 AND event_type='progress.task.updated' AND payload->>'source_evaluation_id'=$2`, fixture.projectID, evaluation.ID).Scan(&workStateEvent); err != nil || workStateEvent != 1 {
		t.Fatalf("automatic work-state event: count=%d err=%v", workStateEvent, err)
	}
	converged := fixture.queueEvaluation(t, map[string]interface{}{"version": "convergence"})
	convergedJob, err := fixture.jobs.Get(fixture.ctx, converged.JobID)
	if err != nil {
		t.Fatal(err)
	}
	convergedJob.Attempts, convergedJob.Status = 1, jobs.StatusRunning
	if err := fixture.store.Transaction.Within(fixture.ctx, nil, func(tx transaction.Tx) error {
		if err := fixture.store.MarkEvaluationRunning(fixture.ctx, tx, convergedJob); err != nil {
			return err
		}
		return fixture.store.CompleteEvaluation(fixture.ctx, tx, convergedJob, result)
	}); err != nil {
		t.Fatalf("complete converged evaluation: %v", err)
	}
	var automaticTasks, repeatedMutations int
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM progress_tasks WHERE project_id=$1 AND source_key='task-create'`, fixture.projectID).Scan(&automaticTasks); err != nil || automaticTasks != 0 {
		t.Fatalf("evaluation created a task without review: count=%d err=%v", automaticTasks, err)
	}
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM progress_proposals WHERE project_id=$1 AND source_key='milestone' AND status='pending'`, fixture.projectID).Scan(&automaticTasks); err != nil || automaticTasks != 1 {
		t.Fatalf("automatic proposal convergence: count=%d err=%v", automaticTasks, err)
	}
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM system_outbox WHERE project_id=$1 AND event_type IN ('progress.task.created','progress.task.updated','progress.proposal.created') AND payload->>'source_evaluation_id'=$2`, fixture.projectID, converged.ID).Scan(&repeatedMutations); err != nil || repeatedMutations != 0 {
		t.Fatalf("converged evaluation repeated Progress mutations: count=%d err=%v", repeatedMutations, err)
	}

	invalid := EvaluationSuggestion{Key: "cross-project", ProposalType: "task.create", Title: "Invalid", Changes: map[string]interface{}{"title": "Invalid", "assignee_id": fixture.generator.MustNew()}}
	err = fixture.store.Transaction.Within(fixture.ctx, nil, func(tx transaction.Tx) error {
		return fixture.store.applyEvaluationSuggestion(fixture.ctx, tx, fixture.projectID, evaluation.ID, fixture.userID, "", invalid, fixture.clock.Now())
	})
	if !errors.Is(err, ErrReferenceInvalid) {
		t.Fatalf("invalid automatic task reference returned %v", err)
	}

	failed := fixture.queueEvaluation(t, map[string]interface{}{"version": "failure"})
	failedJob, err := fixture.jobs.Get(fixture.ctx, failed.JobID)
	if err != nil {
		t.Fatal(err)
	}
	failedJob.Attempts, failedJob.Status = 3, jobs.StatusFailed
	if err := fixture.store.Transaction.Within(fixture.ctx, nil, func(tx transaction.Tx) error {
		if err := fixture.store.MarkEvaluationRunning(fixture.ctx, tx, failedJob); err != nil {
			return err
		}
		return fixture.store.FailEvaluation(fixture.ctx, tx, failedJob, jobs.Failure{Code: "PROVIDER_FAILED", Message: "safe failure"})
	}); err != nil {
		t.Fatalf("fail evaluation: %v", err)
	}
	failedHistory, err := fixture.store.GetEvaluation(fixture.ctx, fixture.projectID, failed.ID)
	if err != nil || failedHistory.Status != "failed" || failedHistory.ErrorCode != "PROVIDER_FAILED" || failedHistory.Attempts != 3 {
		t.Fatalf("failed evaluation history: evaluation=%#v err=%v", failedHistory, err)
	}
	retry, err := fixture.store.ScheduleRequest(fixture.ctx, fixture.projectID, fixture.userID, "session", "retry", true, EvaluationTrigger{TriggerType: "retry", SourceResourceID: failed.ID, OccurredAt: fixture.clock.Now(), Payload: map[string]interface{}{"evaluation_id": failed.ID}})
	if err != nil || retry.Merged || retry.Status != "pending" {
		t.Fatalf("schedule failed evaluation retry: result=%#v err=%v", retry, err)
	}
}

func TestPostgresProgressTrackingContextHashExcludesVolatileProvenance(t *testing.T) {
	fixture := newTrackingPostgresFixture(t)
	task, err := fixture.store.CreateTask(fixture.ctx, fixture.projectID, fixture.userID, CreateTaskInput{Title: "Stable task", Status: TaskTodo}, "human")
	if err != nil {
		t.Fatal(err)
	}
	before, err := fixture.store.EvaluationContext(fixture.ctx, fixture.projectID)
	if err != nil {
		t.Fatal(err)
	}
	beforeVersion, err := canonicalInputVersion(before)
	if err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(time.Hour)
	if _, err := fixture.db.ExecContext(fixture.ctx, `UPDATE progress_tasks SET source_run_id=$3,updated_at=$4 WHERE project_id=$1 AND task_id=$2`, fixture.projectID, task.ID, fixture.generator.MustNew(), fixture.clock.Now()); err != nil {
		t.Fatal(err)
	}
	after, err := fixture.store.EvaluationContext(fixture.ctx, fixture.projectID)
	if err != nil {
		t.Fatal(err)
	}
	afterVersion, err := canonicalInputVersion(after)
	if err != nil {
		t.Fatal(err)
	}
	if beforeVersion != afterVersion {
		t.Fatalf("volatile provenance changed semantic input hash: before=%s after=%s", beforeVersion, afterVersion)
	}
}

type trackingPostgresClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (clock *trackingPostgresClock) Now() time.Time {
	clock.mu.RLock()
	defer clock.mu.RUnlock()
	return clock.now
}

func (clock *trackingPostgresClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

type trackingPostgresFixture struct {
	clock     *trackingPostgresClock
	ctx       context.Context
	db        *sql.DB
	generator identity.Generator
	jobs      jobs.PostgresStore
	projectID string
	store     PostgresStore
	userID    string
}

func newTrackingPostgresFixture(t *testing.T) trackingPostgresFixture {
	t.Helper()
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(requestctx.WithValues(context.Background(), requestctx.Values{RequestID: "stage6-progress-tracking-test"}), 30*time.Second)
	t.Cleanup(cancel)
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	generator := identity.Generator{}
	userID, projectID := generator.MustNew(), generator.MustNew()
	now := time.Date(2026, time.August, 10, 2, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES($1,$2,'Progress Tracking Test','test','active',$3,$3)`, userID, userID+"@progress-tracking.test", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Progress Tracking Test',$2,$3,$3)`, projectID, userID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES($1,$2,'owner',$3,$3)`, projectID, userID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO progress_settings(project_id,auto_task_changes,auto_tracking_enabled,event_triggers_enabled,cron_enabled,cron_schedule,debounce_seconds,min_interval_seconds,updated_by,updated_at) VALUES($1,true,true,true,false,'0 */6 * * *',60,0,$2,$3)`, projectID, userID, now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})
	clock := &trackingPostgresClock{now: now}
	manager := transaction.Manager{DB: transaction.SQLBeginner{DB: db}}
	writer := outbox.Writer{Clock: clock, Generator: generator}
	jobStore := jobs.PostgresStore{Clock: clock, DB: db, Generator: generator, Outbox: writer, Transaction: manager}
	store := PostgresStore{
		Audit: audit.Recorder{Clock: clock, Store: audit.PostgresStore{Clock: clock, DB: db, Generator: generator}},
		Clock: clock, DB: db, Generator: generator, Jobs: jobStore, Outbox: writer, Transaction: manager,
	}
	return trackingPostgresFixture{clock: clock, ctx: ctx, db: db, generator: generator, jobs: jobStore, projectID: projectID, store: store, userID: userID}
}

func (fixture trackingPostgresFixture) queueEvaluation(t *testing.T, input map[string]interface{}) Evaluation {
	t.Helper()
	request, err := fixture.store.ScheduleRequest(fixture.ctx, fixture.projectID, fixture.userID, "session", "manual", true, EvaluationTrigger{TriggerType: "manual", OccurredAt: fixture.clock.Now(), Payload: map[string]interface{}{}})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := fixture.store.ClaimRequest(fixture.ctx, "test-core", time.Minute)
	if err != nil || claim == nil || claim.ID != request.RequestID {
		t.Fatalf("claim evaluation request: claim=%#v err=%v", claim, err)
	}
	version, err := canonicalInputVersion(input)
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := fixture.store.FinalizeRequest(fixture.ctx, *claim, input, version)
	if err != nil || evaluation == nil {
		t.Fatalf("queue evaluation: evaluation=%#v err=%v", evaluation, err)
	}
	return *evaluation
}
