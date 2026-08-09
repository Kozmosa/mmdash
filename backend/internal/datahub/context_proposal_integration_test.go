package datahub

import (
	"context"
	"database/sql"
	"encoding/json"
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
	if proposal.ProposedBy != agentInstanceID ||
		proposal.ProposedByActorID != agentInstanceID ||
		proposal.ProposedByActorKind != "agent" {
		t.Fatalf("created proposal lost Agent provenance: %#v", proposal)
	}

	persisted, err := store.GetProposal(ctx, projectID, proposal.ID)
	if err != nil {
		t.Fatalf("get context proposal: %v", err)
	}
	if persisted.ProposedBy != agentInstanceID ||
		persisted.ProposedByActorID != agentInstanceID ||
		persisted.ProposedByActorKind != "agent" {
		t.Fatalf("persisted proposal lost Agent provenance: %#v", persisted)
	}
	listed, err := store.ListProposals(ctx, projectID)
	if err != nil {
		t.Fatalf("list context proposals: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != proposal.ID ||
		listed[0].ProposedByActorID != agentInstanceID ||
		listed[0].ProposedByActorKind != "agent" {
		t.Fatalf("listed proposal lost Agent provenance: %#v", listed)
	}
	proposalJSON, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("marshal context proposal: %v", err)
	}
	var proposalView map[string]interface{}
	if err := json.Unmarshal(proposalJSON, &proposalView); err != nil {
		t.Fatalf("decode context proposal: %v", err)
	}
	assertAgentProposalJSON(t, proposalView, agentInstanceID)

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

	reviewed, err := store.ReviewProposal(ctx, projectID, proposal.ID, userID,
		ReviewProposalInput{Decision: "accepted", Note: "Reviewed evidence"})
	if err != nil {
		t.Fatalf("review context proposal: %v", err)
	}
	if reviewed.Status != "accepted" || reviewed.PromotedContext == "" ||
		reviewed.ProposedByActorID != agentInstanceID ||
		reviewed.ProposedByActorKind != "agent" {
		t.Fatalf("reviewed proposal lost Agent provenance: %#v", reviewed)
	}

	contexts, err := store.ListContext(ctx, projectID)
	if err != nil {
		t.Fatalf("list promoted context: %v", err)
	}
	if len(contexts) != 1 || contexts[0].ID != reviewed.PromotedContext ||
		contexts[0].ProposedBy != agentInstanceID ||
		contexts[0].ProposedByActorKind != "agent" {
		t.Fatalf("promoted context lost Agent provenance: %#v", contexts)
	}
	contextJSON, err := json.Marshal(contexts[0])
	if err != nil {
		t.Fatalf("marshal promoted context: %v", err)
	}
	var contextView map[string]interface{}
	if err := json.Unmarshal(contextJSON, &contextView); err != nil {
		t.Fatalf("decode promoted context: %v", err)
	}
	if contextView["proposed_by"] != agentInstanceID {
		t.Fatalf("promoted context response lost proposer: %#v", contextView)
	}
	if _, leaked := contextView["proposed_by_kind"]; leaked {
		t.Fatalf("promoted context leaked undocumented actor kind: %#v", contextView)
	}

	var legacyProposer sql.NullString
	var promotedActorID, promotedActorKind string
	if err := db.QueryRowContext(ctx, `
		SELECT proposed_by::text, proposed_by_actor_id::text, proposed_by_actor_kind
		FROM data_context_entries
		WHERE context_id = $1
	`, reviewed.PromotedContext).Scan(
		&legacyProposer, &promotedActorID, &promotedActorKind,
	); err != nil {
		t.Fatalf("read promoted context provenance: %v", err)
	}
	if legacyProposer.Valid || promotedActorID != agentInstanceID ||
		promotedActorKind != "agent" {
		t.Fatalf("unexpected promoted provenance: legacy=%#v actor=%q kind=%q",
			legacyProposer, promotedActorID, promotedActorKind)
	}
}
