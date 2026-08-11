package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresRunReservationActivationFailureAndAudit(t *testing.T) {
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx := requestctx.WithValues(context.Background(), requestctx.Values{
		RequestID: "agent-run-postgres-integration",
	})
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
	agentInstanceID := generator.MustNew()
	grantID := generator.MustNew()
	removableInstanceID := generator.MustNew()
	removableGrantID := generator.MustNew()
	sessionID := generator.MustNew()
	oldTokenID := generator.MustNew()
	newTokenID := generator.MustNew()
	rotationID := generator.MustNew()
	evaluationRequestID := generator.MustNew()
	evaluationID := generator.MustNew()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(
			user_id,email,display_name,password_hash,status,created_at,updated_at
		) VALUES($1,$2,'Agent Run Integration','test','active',$3,$3)
	`, userID, userID+"@agent-run.test", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,created_at,updated_at)
		VALUES($1,'Agent Run Integration',$2,$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_instances(
			agent_instance_id,adapter_type,display_name,management_mode,
			runtime_url,status,created_by,created_at,updated_at
		) VALUES($1,'hermes','Integration Hermes','manual',
			'https://hermes.integration.test','active',$2,$3,$3)
	`, agentInstanceID, userID, now); err != nil {
		t.Fatalf("insert agent instance: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_project_grants(
			grant_id,agent_instance_id,project_id,status,allowed_tools,remote_access_id,
			created_by,created_at,updated_at
		) VALUES($1,$2,$3,'active','["project.get"]'::jsonb,'managed-access-old',$4,$5,$5)
	`, grantID, agentInstanceID, projectID, userID, now); err != nil {
		t.Fatalf("insert agent grant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_instances(
			agent_instance_id,adapter_type,display_name,management_mode,
			runtime_url,status,disabled_at,created_by,created_at,updated_at
		) VALUES($1,'hermes','Disabled Integration Hermes','manual',
			'https://disabled-hermes.integration.test','disabled',$3,$2,$3,$3)
	`, removableInstanceID, userID, now); err != nil {
		t.Fatalf("insert removable disabled agent instance: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_project_grants(
			grant_id,agent_instance_id,project_id,status,allowed_tools,
			created_by,created_at,updated_at
		) VALUES($1,$2,$3,'active','["project.get"]'::jsonb,$4,$5,$5)
	`, removableGrantID, removableInstanceID, projectID, userID, now); err != nil {
		t.Fatalf("insert removable disabled agent grant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_agent_tokens(
			token_id,agent_instance_id,grant_id,project_id,issued_by,name,
			token_hash,allowed_tools,status,replaces_token_id,created_at
		) VALUES
			($1,$3,$4,$5,$6,'Old integration token',$7,
			 '["project.get"]'::jsonb,'active',NULL,$9),
			($2,$3,$4,$5,$6,'New integration token',$8,
			 '["data.read","project.get"]'::jsonb,'pending',$1,$9)
	`, oldTokenID, newTokenID, agentInstanceID, grantID, projectID, userID,
		strings.Repeat("a", 64), strings.Repeat("b", 64), now); err != nil {
		t.Fatalf("insert agent tokens: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_token_rotations(
			rotation_id,grant_id,old_token_id,new_token_id,management_mode,
			status,created_by,created_at,updated_at
		) VALUES($1,$2,$3,$4,'auto','verifying',$5,$6,$6)
	`, rotationID, grantID, oldTokenID, newTokenID, userID, now); err != nil {
		t.Fatalf("insert agent token rotation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_sessions(
			session_id,grant_id,agent_instance_id,project_id,remote_session_id,
			session_type,title,status,created_by,created_at,updated_at
		) VALUES($1,$2,$3,$4,'remote-session-integration','main',
			'Integration Session','active',$5,$6,$6)
	`, sessionID, grantID, agentInstanceID, projectID, userID, now); err != nil {
		t.Fatalf("insert agent session: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO progress_evaluation_requests(
			request_id,project_id,trigger_kind,status,scheduled_for,actor_id,
			requested_by_kind,force,created_at,updated_at
		) VALUES($1,$2,'manual','queued',$3,$4,'session',true,$3,$3)
	`, evaluationRequestID, projectID, now, userID); err != nil {
		t.Fatalf("insert progress evaluation request: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO progress_evaluations(
			evaluation_id,request_id,project_id,status,input_version,input_snapshot,
			trigger_kind,agent_instance_id,evaluator_mode,requested_by,created_at,
			started_at,updated_at
		) VALUES($1,$2,$3,'running',$4,'{}'::jsonb,'manual',$5,'core_agent',$6,$7,$7,$7)
	`, evaluationID, evaluationRequestID, projectID, strings.Repeat("0", 64),
		agentInstanceID, userID, now); err != nil {
		t.Fatalf("insert progress evaluation: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM system_outbox WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM agent_tool_calls WHERE run_id IN (SELECT run_id FROM agent_runs WHERE session_id=$1)`, sessionID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM agent_runs WHERE session_id=$1`, sessionID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM progress_evaluations WHERE evaluation_id=$1`, evaluationID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM progress_evaluation_requests WHERE request_id=$1`, evaluationRequestID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM agent_sessions WHERE session_id=$1`, sessionID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM agent_token_rotations WHERE rotation_id=$1`, rotationID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_agent_tokens WHERE token_id IN ($1,$2)`, oldTokenID, newTokenID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM agent_project_grants WHERE grant_id IN ($1,$2)`, grantID, removableGrantID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM agent_instances WHERE agent_instance_id IN ($1,$2)`, agentInstanceID, removableInstanceID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})

	fixedClock := clock.Fixed{Time: now}
	auditStore := &capturingAgentAuditStore{}
	store := PostgresStore{
		Audit: audit.Recorder{
			Clock: fixedClock, Metrics: metrics.New("core-test", "0"),
			Store: auditStore,
		},
		Clock:     fixedClock,
		DB:        db,
		Generator: generator,
		Outbox: outbox.Writer{
			Clock: fixedClock, Generator: generator,
		},
		Transaction: transaction.Manager{
			DB: transaction.SQLBeginner{DB: db},
		},
	}
	removable, err := store.GetInstance(ctx, projectID, removableInstanceID)
	if err != nil || removable.Status != InstanceDisabled {
		t.Fatalf("disabled instance must remain visible until removed: %#v %v", removable, err)
	}
	removedAt := now.Add(time.Second)
	if err := store.DisableInstance(
		ctx, userID, "user", projectID, removableInstanceID, removedAt,
	); err != nil {
		t.Fatalf("remove an already-disabled instance: %v", err)
	}
	if _, err := store.GetInstance(ctx, projectID, removableInstanceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed instance remained visible: %v", err)
	}
	var storedRemovedAt time.Time
	var removableGrantStatus string
	if err := db.QueryRowContext(ctx, `
		SELECT instance.removed_at, grant_row.status
		FROM agent_instances instance
		JOIN agent_project_grants grant_row ON grant_row.agent_instance_id=instance.agent_instance_id
		WHERE instance.agent_instance_id=$1 AND grant_row.grant_id=$2
	`, removableInstanceID, removableGrantID).Scan(&storedRemovedAt, &removableGrantStatus); err != nil {
		t.Fatalf("read retained removed instance: %v", err)
	}
	if !storedRemovedAt.Equal(removedAt) || removableGrantStatus != "revoked" {
		t.Fatalf("removed instance retention: removed_at=%v grant=%q", storedRemovedAt, removableGrantStatus)
	}
	progressRunID := generator.MustNew()
	progressReservation := RunRecord{
		CreatedAt: now, CreatedBy: userID, ID: progressRunID,
		RemoteRunID: "pending:" + progressRunID, SessionID: sessionID,
		Source: "progress_evaluation", SourceEvaluationID: evaluationID,
		UpdatedAt: now, Version: 1,
	}
	if _, err := store.ReserveRun(ctx, progressReservation); err != nil {
		t.Fatalf("reserve progress evaluation run: %v", err)
	}
	storedProgressRun, err := store.GetRun(ctx, sessionID, progressRunID)
	if err != nil || storedProgressRun.SourceEvaluationID != evaluationID || storedProgressRun.SourceRunID != "" {
		t.Fatalf("progress evaluation provenance: %#v %v", storedProgressRun, err)
	}

	replacement := auth.AgentToken{
		AgentInstanceID: agentInstanceID,
		AllowedTools:    []string{"data.read", "project.get"},
		GrantID:         grantID,
		ID:              newTokenID,
		IssuedBy:        userID,
		ProjectID:       projectID,
	}
	assertGrantAndRotation := func(wantRemoteID, wantRotationStatus string, wantTools []string) {
		t.Helper()
		var remoteID, toolsJSON, rotationStatus string
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(remote_access_id,''), allowed_tools::text
			FROM agent_project_grants WHERE grant_id=$1
		`, grantID).Scan(&remoteID, &toolsJSON); err != nil {
			t.Fatalf("read agent grant: %v", err)
		}
		var tools []string
		if err := json.Unmarshal([]byte(toolsJSON), &tools); err != nil {
			t.Fatalf("decode grant tools: %v", err)
		}
		if err := db.QueryRowContext(ctx, `
			SELECT status FROM agent_token_rotations WHERE rotation_id=$1
		`, rotationID).Scan(&rotationStatus); err != nil {
			t.Fatalf("read token rotation: %v", err)
		}
		if remoteID != wantRemoteID || !sameTools(tools, wantTools) ||
			rotationStatus != wantRotationStatus {
			t.Fatalf("unexpected atomic lifecycle state: remote=%q tools=%#v rotation=%q",
				remoteID, tools, rotationStatus)
		}
	}
	rollbackSentinel := errors.New("rollback after agent credential lifecycle")
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := store.ActivateAgentCredential(
			ctx, tx, replacement, oldTokenID, "managed-access-new", now.Add(time.Second),
		); err != nil {
			return err
		}
		return rollbackSentinel
	})
	if !errors.Is(err, rollbackSentinel) {
		t.Fatalf("rollback lifecycle transaction: %v", err)
	}
	assertGrantAndRotation("managed-access-old", "verifying", []string{"project.get"})
	if err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		return store.ActivateAgentCredential(
			ctx, tx, replacement, oldTokenID, "managed-access-new", now.Add(2*time.Second),
		)
	}); err != nil {
		t.Fatalf("activate agent credential lifecycle: %v", err)
	}
	assertGrantAndRotation("managed-access-new", "completed", replacement.AllowedTools)

	rollbackRunID := generator.MustNew()
	rollbackReservation := RunRecord{
		CreatedAt: now, CreatedBy: userID, ID: rollbackRunID,
		RemoteRunID: "pending:" + rollbackRunID, SessionID: sessionID,
		Source: "message", UpdatedAt: now, Version: 1,
	}
	if _, err := store.ReserveRun(ctx, rollbackReservation); err != nil {
		t.Fatalf("reserve rollback run: %v", err)
	}
	store.Outbox.Generator = identity.Generator{Reader: failingIdentityReader{}}
	rollbackCandidate := rollbackReservation
	rollbackCandidate.RemoteRunID = "remote-run-rollback"
	rollbackCandidate.Status = RunRecordRunning
	if _, err := store.ActivateRun(
		ctx, userID, rollbackCandidate, now.Add(time.Second),
	); err == nil {
		t.Fatal("activation unexpectedly succeeded when Outbox evidence failed")
	}
	rolledBack, err := store.GetRun(ctx, sessionID, rollbackRunID)
	if err != nil {
		t.Fatalf("get rolled-back run: %v", err)
	}
	if rolledBack.Status != RunRecordQueued ||
		rolledBack.RemoteRunID != rollbackReservation.RemoteRunID ||
		rolledBack.StartedAt != nil {
		t.Fatalf("activation state did not roll back: %#v", rolledBack)
	}
	assertAgentSessionRunTimes(t, ctx, db, sessionID, false)

	store.Outbox.Generator = generator
	startedAt := now.Add(2 * time.Second)
	rollbackCandidate.RemoteRunID = "remote-run-active"
	activated, err := store.ActivateRun(ctx, userID, rollbackCandidate, startedAt)
	if err != nil {
		t.Fatalf("activate reserved run: %v", err)
	}
	if activated.Status != RunRecordRunning ||
		activated.RemoteRunID != rollbackCandidate.RemoteRunID ||
		activated.StartedAt == nil || !activated.StartedAt.Equal(startedAt) {
		t.Fatalf("activated run: %#v", activated)
	}
	storedSession, err := store.GetSession(ctx, projectID, agentInstanceID, sessionID)
	if err != nil || storedSession.LastRunID == "" {
		t.Fatalf("session did not expose its latest run: %#v %v", storedSession, err)
	}
	assertAgentSessionRunTimes(t, ctx, db, sessionID, true)
	assertAgentOutboxEvent(t, ctx, db, projectID, rollbackRunID,
		"agent.run.started", RunRecordRunning)
	assertCapturedAgentAudit(t, auditStore.items, rollbackRunID,
		"agent.run.start", "success", "")
	if _, err := store.ActivateRun(
		ctx, userID, rollbackCandidate, startedAt.Add(time.Second),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("activate an already active run: got %v", err)
	}

	approvalOne := "approval-1"
	approvalTwo := "approval-2"
	approvalAt := startedAt.Add(time.Second)
	if _, err := store.RecordRunApproval(ctx, rollbackRunID, approvalOne, approvalAt); err != nil {
		t.Fatalf("record first approval: %v", err)
	}
	if _, err := store.RecordRunApproval(ctx, rollbackRunID, approvalTwo, approvalAt.Add(time.Second)); err != nil {
		t.Fatalf("record second approval: %v", err)
	}
	pending, err := store.GetRun(ctx, sessionID, rollbackRunID)
	if err != nil || pending.Status != RunRecordWaitingForApproval ||
		len(pending.PendingApprovalIDs) != 2 {
		t.Fatalf("persist pending approvals: %#v %v", pending, err)
	}
	claimOne := generator.MustNew()
	if _, err := store.ClaimRunApproval(
		ctx, rollbackRunID, approvalTwo, generator.MustNew(), approvalAt.Add(2*time.Second),
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("non-head approval was claimable: %v", err)
	}
	if _, err := store.ClaimRunApproval(
		ctx, rollbackRunID, approvalOne, claimOne, approvalAt.Add(2*time.Second),
	); err != nil {
		t.Fatalf("claim first approval: %v", err)
	}
	if _, err := store.ClaimRunApproval(
		ctx, rollbackRunID, approvalOne, generator.MustNew(), approvalAt.Add(3*time.Second),
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("live approval claim was stolen: %v", err)
	}

	// A new Core process can reclaim a response abandoned by a crashed process
	// after the bounded Hermes request lease, while the old claim can no longer
	// release or complete it.
	restartedStore := store
	claimTwo := generator.MustNew()
	reclaimedAt := approvalAt.Add(runApprovalClaimLease + 3*time.Second)
	if _, err := restartedStore.ClaimRunApproval(
		ctx, rollbackRunID, approvalOne, claimTwo, reclaimedAt,
	); err != nil {
		t.Fatalf("reclaim approval after restart: %v", err)
	}
	if _, err := store.ReleaseRunApprovalClaim(
		ctx, rollbackRunID, approvalOne, claimOne, reclaimedAt.Add(time.Second),
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale claim released the replacement: %v", err)
	}
	resolved, err := restartedStore.CompleteRunApproval(
		ctx, rollbackRunID, approvalOne, claimTwo, reclaimedAt.Add(2*time.Second),
	)
	if err != nil || resolved.Status != RunRecordWaitingForApproval ||
		len(resolved.PendingApprovalIDs) != 1 ||
		resolved.PendingApprovalIDs[0] != approvalTwo {
		t.Fatalf("resolving one approval cleared another: %#v %v", resolved, err)
	}
	if _, err := restartedStore.ApplyRunApprovalResponse(
		ctx, rollbackRunID, approvalOne, reclaimedAt.Add(3*time.Second),
	); err != nil {
		t.Fatalf("idempotent SSE response after Core completion: %v", err)
	}
	if _, err := restartedStore.ClaimRunApproval(
		ctx, rollbackRunID, "forged-approval", generator.MustNew(), reclaimedAt.Add(4*time.Second),
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("forged approval was claimable: %v", err)
	}

	claimThree := generator.MustNew()
	if _, err := restartedStore.ClaimRunApproval(
		ctx, rollbackRunID, approvalTwo, claimThree, reclaimedAt.Add(5*time.Second),
	); err != nil {
		t.Fatalf("claim second approval: %v", err)
	}
	if _, err := restartedStore.ReleaseRunApprovalClaim(
		ctx, rollbackRunID, approvalTwo, claimThree, reclaimedAt.Add(6*time.Second),
	); err != nil {
		t.Fatalf("release failed Hermes response: %v", err)
	}
	claimFour := generator.MustNew()
	fullyResolved, err := restartedStore.ClaimRunApproval(
		ctx, rollbackRunID, approvalTwo, claimFour, reclaimedAt.Add(7*time.Second),
	)
	if err != nil || fullyResolved.Status != RunRecordWaitingForApproval {
		t.Fatalf("retry released approval: %#v %v", fullyResolved, err)
	}
	fullyResolved, err = restartedStore.CompleteRunApproval(
		ctx, rollbackRunID, approvalTwo, claimFour, reclaimedAt.Add(8*time.Second),
	)
	if err != nil || fullyResolved.Status != RunRecordRunning ||
		len(fullyResolved.PendingApprovalIDs) != 0 {
		t.Fatalf("complete final approval: %#v %v", fullyResolved, err)
	}

	if _, err := restartedStore.RecordRunApproval(
		ctx, rollbackRunID, "approval-terminal", reclaimedAt.Add(9*time.Second),
	); err != nil {
		t.Fatalf("record terminal approval fixture: %v", err)
	}
	if _, err := restartedStore.RecordRunApproval(
		ctx, rollbackRunID, "approval-terminal-next", reclaimedAt.Add(10*time.Second),
	); err != nil {
		t.Fatalf("record unidentified response fixture: %v", err)
	}
	mapped, mappedID, err := restartedStore.ApplyNextRunApprovalResponse(
		ctx, rollbackRunID, reclaimedAt.Add(11*time.Second),
	)
	if err != nil || mappedID != "approval-terminal" ||
		len(mapped.PendingApprovalIDs) != 1 ||
		mapped.PendingApprovalIDs[0] != "approval-terminal-next" {
		t.Fatalf("map unidentified response FIFO: id=%q run=%#v err=%v", mappedID, mapped, err)
	}
	terminalAt := reclaimedAt.Add(12 * time.Second)
	terminal, err := restartedStore.UpdateRun(
		ctx, rollbackRunID, RunRecordCompleted, "", terminalAt,
	)
	if err != nil || terminal.Status != RunRecordCompleted ||
		len(terminal.PendingApprovalIDs) != 0 {
		t.Fatalf("terminal run did not expire approvals: %#v %v", terminal, err)
	}
	if _, err := restartedStore.ClaimRunApproval(
		ctx, rollbackRunID, "approval-terminal-next", generator.MustNew(), terminalAt.Add(time.Second),
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired approval was claimable: %v", err)
	}

	failedRunID := generator.MustNew()
	failedReservation := RunRecord{
		CreatedAt: now.Add(3 * time.Second), CreatedBy: userID, ID: failedRunID,
		RemoteRunID: "pending:" + failedRunID, SessionID: sessionID,
		Source: "message", UpdatedAt: now.Add(3 * time.Second), Version: 1,
	}
	if _, err := store.ReserveRun(ctx, failedReservation); err != nil {
		t.Fatalf("reserve failed run: %v", err)
	}
	failedAt := now.Add(4 * time.Second)
	if err := store.FailRunReservation(
		ctx, userID, failedRunID, "runtime_unavailable", failedAt,
	); err != nil {
		t.Fatalf("fail reserved run: %v", err)
	}
	failed, err := store.GetRun(ctx, sessionID, failedRunID)
	if err != nil {
		t.Fatalf("get failed run: %v", err)
	}
	if failed.Status != RunRecordFailed ||
		failed.SafeErrorCode != "runtime_unavailable" ||
		failed.CompletedAt == nil || !failed.CompletedAt.Equal(failedAt) {
		t.Fatalf("failed run: %#v", failed)
	}
	assertAgentOutboxEvent(t, ctx, db, projectID, failedRunID,
		"agent.run.failed", RunRecordFailed)
	assertCapturedAgentAudit(t, auditStore.items, failedRunID,
		"agent.run.start", "error", "runtime_unavailable")
	if err := store.FailRunReservation(
		ctx, userID, failedRunID, "runtime_unavailable", failedAt,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("fail an already failed reservation: got %v", err)
	}
}

type failingIdentityReader struct{}

func (failingIdentityReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

type capturingAgentAuditStore struct {
	items []audit.Event
}

func (store *capturingAgentAuditStore) Record(
	_ context.Context,
	event audit.Event,
) (audit.Event, error) {
	store.items = append(store.items, event)
	return event, nil
}

func (store *capturingAgentAuditStore) RecordInTransaction(
	_ context.Context,
	_ transaction.Tx,
	event audit.Event,
) (audit.Event, error) {
	store.items = append(store.items, event)
	return event, nil
}

func (*capturingAgentAuditStore) List(
	context.Context,
	audit.Filter,
	pagination.Request,
) (audit.Page, error) {
	return audit.Page{}, nil
}

func assertAgentSessionRunTimes(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	sessionID string,
	wantSet bool,
) {
	t.Helper()
	var lastRunAt, lastMessageAt sql.NullTime
	if err := db.QueryRowContext(ctx, `
		SELECT last_run_at,last_message_at FROM agent_sessions WHERE session_id=$1
	`, sessionID).Scan(&lastRunAt, &lastMessageAt); err != nil {
		t.Fatalf("read session run times: %v", err)
	}
	if lastRunAt.Valid != wantSet || lastMessageAt.Valid != wantSet {
		t.Fatalf("session run times: last_run=%v last_message=%v want_set=%v",
			lastRunAt, lastMessageAt, wantSet)
	}
}

func assertAgentOutboxEvent(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	projectID string,
	runID string,
	eventType string,
	wantStatus string,
) {
	t.Helper()
	var payloadBytes []byte
	if err := db.QueryRowContext(ctx, `
		SELECT payload FROM system_outbox
		WHERE project_id=$1 AND event_type=$2 AND payload->>'resource_id'=$3
		ORDER BY occurred_at DESC LIMIT 1
	`, projectID, eventType, runID).Scan(&payloadBytes); err != nil {
		t.Fatalf("read %s Outbox event: %v", eventType, err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("decode %s Outbox payload: %v", eventType, err)
	}
	if eventType == "agent.run.failed" && payload["status"] != wantStatus {
		t.Fatalf("%s Outbox payload: %#v", eventType, payload)
	}
}

func assertCapturedAgentAudit(
	t *testing.T,
	items []audit.Event,
	runID string,
	action string,
	outcome string,
	errorCode string,
) {
	t.Helper()
	for _, item := range items {
		if item.ResourceID == runID && item.Action == action {
			if item.Outcome != outcome || item.ErrorCode != errorCode ||
				item.Category != "agent" || item.ResourceType != "agent-run" {
				t.Fatalf("captured Agent Audit: %#v", item)
			}
			return
		}
	}
	t.Fatalf("missing Agent Audit action=%s run_id=%s in %#v", action, runID, items)
}
