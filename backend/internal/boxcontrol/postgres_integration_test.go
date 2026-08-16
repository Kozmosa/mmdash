package boxcontrol

import (
	"context"
	"database/sql"
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
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresRevokeBoxUnbindsAndRecordsLifecycle(t *testing.T) {
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
	userID, projectID, tokenID, boxID := generator.MustNew(), generator.MustNew(), generator.MustNew(), generator.MustNew()
	now := time.Now().UTC().Truncate(time.Microsecond)
	heartbeatAt := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES($1,$2,'Box Revoke Integration','test','active',$3,$3)`, userID, userID+"@box-revoke.test", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Box Revoke Integration',$2,$3,$3)`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_tokens(token_id,user_id,project_id,kind,name,token_hash,created_at) VALUES($1,$2,$3,'box','Box Revoke Integration',$4,$5)`, tokenID, userID, projectID, strings.Repeat("c", 64), now); err != nil {
		t.Fatalf("insert Box token: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO box_nodes(box_id,project_id,name,status,version,capabilities,runtimes,limits,load,token_id,idempotency_key,last_heartbeat_at,created_by,created_at,updated_at)
		VALUES($1,$2,'Box Revoke Integration','online','test','[{"name":"sandbox","version":"1"}]'::jsonb,'[{"name":"local-docker","version":"1"}]'::jsonb,'{"cpu_millis":1000,"memory_bytes":268435456,"timeout_seconds":90,"disk_bytes":1073741824,"pids":64,"network":"disabled"}'::jsonb,'{"running_tasks":0,"capacity":1,"cpu_millis":0,"memory_bytes":0}'::jsonb,$3,'box-revoke-integration',$4,$5,$4,$4)
	`, boxID, projectID, tokenID, heartbeatAt, userID); err != nil {
		t.Fatalf("insert Box: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO box_project_bindings(project_id,box_id,created_at,updated_at) VALUES($1,$2,$3,$3)`, projectID, boxID, now); err != nil {
		t.Fatalf("insert binding: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})

	fixedClock := clock.Fixed{Time: now}
	auditRecorder := audit.Recorder{Clock: fixedClock, Metrics: metrics.New("core-test", "test"), Store: audit.PostgresStore{Clock: fixedClock, DB: db, Generator: generator}}
	store := PostgresStore{Audit: auditRecorder, DB: db, Generator: generator, Outbox: outbox.Writer{Clock: fixedClock, Generator: generator}, Transaction: transaction.Manager{DB: transaction.SQLBeginner{DB: db}}}
	requestContext := requestctx.WithValues(ctx, requestctx.Values{RequestID: generator.MustNew()})
	requestctx.SetActor(requestContext, userID, "user")
	requestctx.SetProject(requestContext, projectID)

	offline, err := store.MarkOffline(requestContext, now, heartbeatAt.Add(time.Second), 10)
	if err != nil {
		t.Fatalf("mark Box offline: %v", err)
	}
	if len(offline) != 1 || offline[0].ID != boxID || offline[0].Status != StatusOffline {
		t.Fatalf("unexpected offline transition: %#v", offline)
	}
	revoked, _, err := store.ForceRevoke(requestContext, boxID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("revoke Box: %v", err)
	}
	if revoked.Status != StatusRevoked || revoked.OfflineSince == nil {
		t.Fatalf("unexpected revoked Box: %#v", revoked)
	}
	if err := (auth.PostgresStore{DB: db}).RevokeBoxToken(ctx, tokenID, userID, now); err != nil {
		t.Fatalf("revoke Box token: %v", err)
	}

	var bindingCount, auditCount, outboxCount int
	var revokedAt sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM box_project_bindings WHERE box_id=$1`, boxID).Scan(&bindingCount); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE project_id=$1 AND action='box.revoked'`, projectID).Scan(&auditCount); err != nil {
		t.Fatalf("count Audit: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_outbox WHERE project_id=$1 AND event_type='box.unassigned'`, projectID).Scan(&outboxCount); err != nil {
		t.Fatalf("count Outbox: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT revoked_at FROM auth_tokens WHERE token_id=$1`, tokenID).Scan(&revokedAt); err != nil {
		t.Fatalf("read token lifecycle: %v", err)
	}
	if bindingCount != 0 || auditCount != 1 || outboxCount != 1 || !revokedAt.Valid {
		t.Fatalf("incomplete revoke lifecycle: bindings=%d audit=%d outbox=%d token_revoked=%v", bindingCount, auditCount, outboxCount, revokedAt.Valid)
	}
}
