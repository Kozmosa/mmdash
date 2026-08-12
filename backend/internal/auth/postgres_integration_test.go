package auth

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresAgentTokenVerificationAndAtomicActivation(t *testing.T) {
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
	otherProjectID := generator.MustNew()
	agentInstanceID := generator.MustNew()
	grantID := generator.MustNew()
	oldTokenID := generator.MustNew()
	pendingTokenID := generator.MustNew()
	now := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(
			user_id,email,display_name,password_hash,status,system_role,
			created_at,updated_at
		) VALUES($1,$2,'Agent Token Integration','test','active','admin',$3,$3)
	`, userID, userID+"@agent-token.test", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,created_at,updated_at)
		VALUES
			($1,'Agent Token Integration',$3,$4,$4),
			($2,'Other Agent Token Project',$3,$4,$4)
	`, projectID, otherProjectID, userID, now); err != nil {
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
			grant_id,agent_instance_id,project_id,status,allowed_tools,
			created_by,created_at,updated_at
		) VALUES($1,$2,$3,'active','["project.get"]'::jsonb,$4,$5,$5)
	`, grantID, agentInstanceID, projectID, userID, now); err != nil {
		t.Fatalf("insert agent grant: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_agent_tokens WHERE grant_id=$1`, grantID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM agent_project_grants WHERE grant_id=$1`, grantID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM agent_instances WHERE agent_instance_id=$1`, agentInstanceID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM projects WHERE project_id IN ($1,$2)`, projectID, otherProjectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})

	store := PostgresStore{
		DB: db,
		Transaction: transaction.Manager{
			DB: transaction.SQLBeginner{DB: db},
		},
	}
	oldToken := AgentToken{
		AgentInstanceID: agentInstanceID,
		AllowedTools:    []string{"project.get"},
		CreatedAt:       now.Add(-time.Hour),
		GrantID:         grantID,
		ID:              oldTokenID,
		IssuedBy:        userID,
		Name:            "Old Agent Token",
		ProjectID:       projectID,
		Status:          "active",
		TokenHash:       strings.Repeat("b", 64),
	}
	pendingToken := AgentToken{
		AgentInstanceID:           agentInstanceID,
		AllowedTools:              []string{"project.get"},
		CreatedAt:                 now,
		GrantID:                   grantID,
		ID:                        pendingTokenID,
		IssuedBy:                  userID,
		Name:                      "Pending Agent Token",
		ProjectID:                 projectID,
		ReplacesTokenID:           oldTokenID,
		Status:                    "pending",
		TokenHash:                 strings.Repeat("c", 64),
		VerificationChallengeHash: strings.Repeat("e", 64),
	}
	if err := store.CreateAgentToken(ctx, oldToken); err != nil {
		t.Fatalf("create old token: %v", err)
	}
	if err := store.CreateAgentToken(ctx, pendingToken); err != nil {
		t.Fatalf("create pending token: %v", err)
	}
	concurrentPending := pendingToken
	concurrentPending.ID = generator.MustNew()
	concurrentPending.Name = "Concurrent Pending Agent Token"
	concurrentPending.TokenHash = strings.Repeat("d", 64)
	if err := store.CreateAgentToken(ctx, concurrentPending); !errors.Is(err, ErrConflict) {
		t.Fatalf("create concurrent pending token: got %v", err)
	}

	if _, err := store.ActivateAgentToken(
		ctx, pendingTokenID, oldTokenID, "", now.Add(time.Minute),
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("activate without verification: got %v", err)
	}
	assertPostgresAgentTokenStatus(t, ctx, db, oldTokenID, "active")
	assertPostgresAgentTokenStatus(t, ctx, db, pendingTokenID, "pending")

	evidence := AgentTokenVerificationEvidence{
		AgentInstanceID: agentInstanceID,
		ChallengeHash:   pendingToken.VerificationChallengeHash,
		EvidenceID:      generator.MustNew(),
		MCPMethod:       AgentTokenVerificationMethod,
		MCPSessionID:    "mcp-session-integration",
		ProjectID:       projectID,
		RequestID:       "request-integration",
		TokenID:         pendingTokenID,
		VerifiedAt:      now.Add(30 * time.Second),
	}
	storedEvidence, err := store.MarkAgentTokenVerified(ctx, evidence)
	if err != nil {
		t.Fatalf("mark token verified: %v", err)
	}
	assertAgentTokenVerificationEvidence(t, storedEvidence, evidence)
	loaded, err := store.GetAgentToken(ctx, pendingTokenID)
	if err != nil {
		t.Fatalf("get verified token: %v", err)
	}
	if loaded.Verification == nil {
		t.Fatalf("loaded evidence: got %#v want %#v", loaded.Verification, evidence)
	}
	assertAgentTokenVerificationEvidence(t, *loaded.Verification, evidence)

	secondEvidence := evidence
	secondEvidence.EvidenceID = generator.MustNew()
	secondEvidence.MCPSessionID = "different-session"
	secondEvidence.RequestID = "different-request"
	secondEvidence.VerifiedAt = evidence.VerifiedAt.Add(time.Second)
	idempotentEvidence, err := store.MarkAgentTokenVerified(ctx, secondEvidence)
	if err != nil {
		t.Fatalf("repeat verification: %v", err)
	}
	assertAgentTokenVerificationEvidence(t, idempotentEvidence, evidence)

	activationFailure := errors.New("credential lifecycle failed")
	store.AgentCredentials = failingAgentCredentialLifecycle{err: activationFailure}
	if _, err := store.ActivateAgentToken(
		ctx, pendingTokenID, oldTokenID, "remote-access-new", now.Add(time.Minute),
	); !errors.Is(err, activationFailure) {
		t.Fatalf("failed lifecycle activation: got %v", err)
	}
	assertPostgresAgentTokenStatus(t, ctx, db, oldTokenID, "active")
	assertPostgresAgentTokenStatus(t, ctx, db, pendingTokenID, "pending")

	store.AgentCredentials = nil
	activated, err := store.ActivateAgentToken(
		ctx, pendingTokenID, oldTokenID, "remote-access-new", now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatalf("activate verified token: %v", err)
	}
	if activated.Status != "active" || activated.Verification == nil {
		t.Fatalf("activated token: %#v", activated)
	}
	assertPostgresAgentTokenStatus(t, ctx, db, oldTokenID, "revoked")
	assertPostgresAgentTokenStatus(t, ctx, db, pendingTokenID, "active")
	if _, err := store.MarkAgentTokenVerified(ctx, secondEvidence); !errors.Is(err, ErrConflict) {
		t.Fatalf("verified active token again: got %v", err)
	}
	concurrentA := pendingToken
	concurrentA.ID = generator.MustNew()
	concurrentA.Name = "Concurrent Pending A"
	concurrentA.ReplacesTokenID = pendingTokenID
	concurrentA.TokenHash = strings.Repeat("7", 64)
	concurrentB := concurrentA
	concurrentB.ID = generator.MustNew()
	concurrentB.Name = "Concurrent Pending B"
	concurrentB.TokenHash = strings.Repeat("8", 64)
	concurrentResults := make(chan error, 2)
	go func() { concurrentResults <- store.CreateAgentToken(ctx, concurrentA) }()
	go func() { concurrentResults <- store.CreateAgentToken(ctx, concurrentB) }()
	firstResult, secondResult := <-concurrentResults, <-concurrentResults
	if (firstResult == nil) == (secondResult == nil) ||
		(firstResult != nil && !errors.Is(firstResult, ErrConflict)) ||
		(secondResult != nil && !errors.Is(secondResult, ErrConflict)) {
		t.Fatalf("concurrent pending issuance: first=%v second=%v", firstResult, secondResult)
	}
	var concurrentWinnerID string
	for _, candidate := range []string{concurrentA.ID, concurrentB.ID} {
		created, getErr := store.GetAgentToken(ctx, candidate)
		if getErr == nil && created.Status == "pending" {
			concurrentWinnerID = candidate
		}
	}
	if concurrentWinnerID == "" {
		t.Fatal("concurrent pending issuance had no persisted winner")
	}
	assertPostgresAgentTokenStatus(t, ctx, db, pendingTokenID, "active")
	if err := store.RevokeAgentToken(ctx, concurrentWinnerID, now); err != nil {
		t.Fatalf("revoke concurrent pending winner: %v", err)
	}
	mismatchedBinding := pendingToken
	mismatchedBinding.ID = generator.MustNew()
	mismatchedBinding.Name = "Mismatched Project Binding"
	mismatchedBinding.ProjectID = otherProjectID
	mismatchedBinding.TokenHash = strings.Repeat("9", 64)
	if err := store.CreateAgentToken(ctx, mismatchedBinding); err == nil {
		t.Fatal("created Agent Token with a Grant from another Project")
	}

	expiredCreatedAt := now.Add(-2 * time.Minute)
	expiredAt := now.Add(-time.Minute)
	expiredToken := pendingToken
	expiredToken.ID = generator.MustNew()
	expiredToken.Name = "Expired Pending Agent Token"
	expiredToken.TokenHash = strings.Repeat("e", 64)
	expiredToken.CreatedAt = expiredCreatedAt
	expiredToken.ExpiresAt = &expiredAt
	expiredToken.ReplacesTokenID = pendingTokenID
	if err := store.CreateAgentToken(ctx, expiredToken); err != nil {
		t.Fatalf("create expired pending token: %v", err)
	}
	expiredEvidence := evidence
	expiredEvidence.EvidenceID = generator.MustNew()
	expiredEvidence.TokenID = expiredToken.ID
	expiredEvidence.VerifiedAt = now
	if _, err := store.MarkAgentTokenVerified(ctx, expiredEvidence); !errors.Is(err, ErrConflict) {
		t.Fatalf("verify expired pending token: got %v", err)
	}
	expiresAt := now.Add(10 * time.Minute)
	expiringToken := pendingToken
	expiringToken.ID = generator.MustNew()
	expiringToken.Name = "Expiring Pending Agent Token"
	expiringToken.TokenHash = strings.Repeat("f", 64)
	expiringToken.ExpiresAt = &expiresAt
	expiringToken.ReplacesTokenID = pendingTokenID
	if err := store.CreateAgentToken(ctx, expiringToken); err != nil {
		t.Fatalf("replace expired pending token: %v", err)
	}
	assertPostgresAgentTokenStatus(t, ctx, db, expiredToken.ID, "revoked")
	assertPostgresAgentTokenStatus(t, ctx, db, pendingTokenID, "active")
	assertPostgresAgentTokenStatus(t, ctx, db, expiringToken.ID, "pending")
	expiringEvidence := evidence
	expiringEvidence.EvidenceID = generator.MustNew()
	expiringEvidence.TokenID = expiringToken.ID
	expiringEvidence.VerifiedAt = now.Add(5 * time.Minute)
	if _, err := store.MarkAgentTokenVerified(ctx, expiringEvidence); err != nil {
		t.Fatalf("verify unexpired pending token: %v", err)
	}
	if _, err := store.ActivateAgentToken(
		ctx, expiringToken.ID, pendingTokenID, "", now.Add(11*time.Minute),
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("activate token after expiry: got %v", err)
	}
	assertPostgresAgentTokenStatus(t, ctx, db, pendingTokenID, "active")
	assertPostgresAgentTokenStatus(t, ctx, db, expiringToken.ID, "pending")
}

type failingAgentCredentialLifecycle struct {
	err error
}

func (lifecycle failingAgentCredentialLifecycle) ActivateAgentCredential(
	context.Context,
	transaction.Tx,
	AgentToken,
	string,
	string,
	time.Time,
) error {
	return lifecycle.err
}

func (lifecycle failingAgentCredentialLifecycle) RevokeAgentCredential(
	context.Context,
	transaction.Tx,
	AgentToken,
	time.Time,
) error {
	return lifecycle.err
}

func assertPostgresAgentTokenStatus(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	tokenID string,
	want string,
) {
	t.Helper()
	var status string
	if err := db.QueryRowContext(
		ctx, `SELECT status FROM auth_agent_tokens WHERE token_id=$1`, tokenID,
	).Scan(&status); err != nil {
		t.Fatalf("read token %s status: %v", tokenID, err)
	}
	if status != want {
		t.Fatalf("token %s status: got %s want %s", tokenID, status, want)
	}
}

func assertAgentTokenVerificationEvidence(
	t *testing.T,
	got AgentTokenVerificationEvidence,
	want AgentTokenVerificationEvidence,
) {
	t.Helper()
	gotTime := got.VerifiedAt
	wantTime := want.VerifiedAt
	got.VerifiedAt = time.Time{}
	want.VerifiedAt = time.Time{}
	if got != want || !gotTime.Equal(wantTime) {
		t.Fatalf("Agent Token verification evidence: got %#v at %s want %#v at %s",
			got, gotTime, want, wantTime)
	}
}
