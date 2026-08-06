package datahub

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestCreateContextProposalWritesProposalObjectAndActivity(t *testing.T) {
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
	now := time.Date(2026, time.August, 6, 7, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES($1,$2,'Context Proposal Test','test','active',$3,$3)`, userID, userID+"@context-proposal.test", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Context Proposal Test',$2,$3,$3)`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES($1,$2,'owner',$3,$3)`, projectID, userID, now); err != nil {
		t.Fatalf("insert membership: %v", err)
	}

	agentInstanceID := generator.MustNew()
	grantID := generator.MustNew()
	sessionID := generator.MustNew()
	runID := generator.MustNew()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_instances(agent_instance_id,adapter_type,display_name,
			management_mode,runtime_url,status,management_path,created_by,created_at,updated_at)
		VALUES($1,'hermes','Context Proposal Test','manual','http://hermes.test','active','direct',$2,$3,$3)
	`, agentInstanceID, userID, now); err != nil {
		t.Fatalf("insert agent instance: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_project_grants(grant_id,agent_instance_id,project_id,role,status,
			allowed_tools,created_by,created_at,updated_at)
		VALUES($1,$2,$3,'agent','active','["project.get"]'::jsonb,$4,$5,$5)
	`, grantID, agentInstanceID, projectID, userID, now); err != nil {
		t.Fatalf("insert agent grant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_sessions(session_id,grant_id,agent_instance_id,project_id,
			remote_session_id,session_type,title,status,created_by,created_at,updated_at)
		VALUES($1,$2,$3,$4,'remote-session','main','Main','active',$5,$6,$6)
	`, sessionID, grantID, agentInstanceID, projectID, userID, now); err != nil {
		t.Fatalf("insert agent session: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_runs(run_id,session_id,remote_run_id,status,source,
			created_by,created_at,updated_at)
		VALUES($1,$2,'remote-run','completed','message',$3,$4,$4)
	`, runID, sessionID, userID, now); err != nil {
		t.Fatalf("insert agent run: %v", err)
	}

	store := PostgresStore{
		Clock:       clock.Fixed{Time: now.Add(10 * time.Minute)},
		DB:          db,
		Generator:   generator,
		Outbox:      outbox.Writer{Clock: clock.Fixed{Time: now.Add(10 * time.Minute)}, Generator: generator},
		Transaction: transaction.Manager{DB: transaction.SQLBeginner{DB: db}},
	}
	proposal, err := store.CreateProposal(ctx, projectID, ProposalActor{
		AgentRunID:     runID,
		AgentSessionID: sessionID,
		ID:             agentInstanceID,
		Kind:           "agent",
		UserID:         "",
	}, CreateProposalInput{
		AgentRunID:     runID,
		AgentSessionID: sessionID,
		Content:        "proposal content",
		ContextType:    "finding",
		Rationale:      "proposal rationale",
		Title:          "Proposal title",
	})
	if err != nil {
		t.Fatalf("create context proposal: %v", err)
	}
	if proposal.ID == "" || proposal.Status != "pending" {
		t.Fatalf("unexpected proposal: %#v", proposal)
	}

	var objectCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM data_objects WHERE source_module='datahub' AND object_type='context-proposal' AND source_id=$1 AND object_id=$2`, proposal.ID, proposal.ID).Scan(&objectCount); err != nil {
		t.Fatalf("read proposal object: %v", err)
	}
	if objectCount != 1 {
		t.Fatalf("proposal object count: got %d, want 1", objectCount)
	}

	var activityCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM data_activity WHERE object_id=$1 AND activity_type='context.proposal.created'`, proposal.ID).Scan(&activityCount); err != nil {
		t.Fatalf("read proposal activity: %v", err)
	}
	if activityCount != 1 {
		t.Fatalf("proposal activity count: got %d, want 1", activityCount)
	}
}
