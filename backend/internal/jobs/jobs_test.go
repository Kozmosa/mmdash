package jobs

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type recordingTx struct {
	args    [][]interface{}
	queries []string
}

func (*recordingTx) Commit() error { return nil }

func (tx *recordingTx) ExecContext(
	_ context.Context,
	query string,
	args ...interface{},
) (sql.Result, error) {
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, args)
	return fakeResult(1), nil
}

func (*recordingTx) QueryContext(
	context.Context,
	string,
	...interface{},
) (*sql.Rows, error) {
	panic("unexpected QueryContext")
}

func (*recordingTx) QueryRowContext(
	context.Context,
	string,
	...interface{},
) *sql.Row {
	panic("unexpected QueryRowContext")
}

func (*recordingTx) Rollback() error { return nil }

type fakeResult int64

func (result fakeResult) LastInsertId() (int64, error) { return int64(result), nil }
func (result fakeResult) RowsAffected() (int64, error) { return int64(result), nil }

type authStub struct {
	identity auth.Identity
}

func (stub authStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return stub.identity, nil
}

type projectAccessStub struct {
	err        error
	permission project.Permission
}

func (stub *projectAccessStub) Authorize(
	_ context.Context,
	_ auth.Identity,
	_ string,
	permission project.Permission,
) error {
	stub.permission = permission
	return stub.err
}

type memoryStore struct {
	claimInput ClaimInput
	created    CreateInput
	job        Job
}

func (store *memoryStore) AppendLog(
	context.Context,
	string,
	string,
	string,
	string,
	map[string]interface{},
) (Log, error) {
	return Log{ID: "log-1"}, nil
}

func (store *memoryStore) Cancel(context.Context, string, string) (Job, error) {
	store.job.Status = StatusCancelled
	return store.job, nil
}

func (store *memoryStore) Claim(_ context.Context, input ClaimInput) (*Job, error) {
	store.claimInput = input
	return &store.job, nil
}

func (store *memoryStore) Complete(
	context.Context,
	string,
	string,
	map[string]interface{},
) (Job, error) {
	store.job.Status = StatusSucceeded
	return store.job, nil
}

func (store *memoryStore) Create(
	_ context.Context,
	_ string,
	input CreateInput,
) (Job, bool, error) {
	store.created = input
	return store.job, true, nil
}

func (store *memoryStore) Fail(context.Context, string, Failure) (Job, error) {
	store.job.Status = StatusFailed
	return store.job, nil
}

func (store *memoryStore) Get(context.Context, string) (Job, error) {
	if store.job.ID == "" {
		return Job{}, ErrNotFound
	}
	return store.job, nil
}

func (store *memoryStore) HeartbeatWorker(context.Context, WorkerHeartbeat) error {
	return nil
}

func (store *memoryStore) ListLogs(context.Context, string) ([]Log, error) {
	return []Log{{ID: "log-1"}}, nil
}

func (store *memoryStore) Renew(context.Context, string, string, int) (Job, error) {
	return store.job, nil
}

func TestCreateAppliesDefaultsAndProjectPermission(t *testing.T) {
	access := &projectAccessStub{}
	store := &memoryStore{job: Job{ID: "job-1", ProjectID: "project-1"}}
	service := Service{
		Clock:    clock.Fixed{Time: time.Now()},
		Projects: access,
		Store:    store,
	}
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}
	job, created, err := service.Create(context.Background(), identity, CreateInput{
		JobType:   "system.test",
		Payload:   map[string]interface{}{"value": true},
		ProjectID: "project-1",
	})
	if err != nil || !created || job.ID != "job-1" {
		t.Fatalf("unexpected create result: %#v %v %v", job, created, err)
	}
	if access.permission != project.PermissionJobsCreate {
		t.Fatalf("unexpected permission: %s", access.permission)
	}
	if store.created.MaxAttempts != 3 || store.created.TimeoutSeconds != 900 {
		t.Fatalf("defaults were not applied: %#v", store.created)
	}
}

func TestCreateRejectsInvalidJobTypeBeforePersistence(t *testing.T) {
	service := Service{Projects: &projectAccessStub{}, Store: &memoryStore{}}
	_, _, err := service.Create(
		context.Background(),
		auth.Identity{Kind: "session"},
		CreateInput{
			JobType:   "not dotted",
			Payload:   map[string]interface{}{},
			ProjectID: "project-1",
		},
	)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestWorkerOperationsRequireAPITokenAndAdminCanClaimGlobally(t *testing.T) {
	store := &memoryStore{job: Job{ID: "job-1", ProjectID: "project-1"}}
	service := Service{Projects: &projectAccessStub{}, Store: store}
	_, err := service.Claim(context.Background(), auth.Identity{Kind: "session"}, ClaimInput{
		JobTypes: []string{"system.test"},
		WorkerID: "worker-1",
	})
	if !errors.Is(err, ErrWorkerToken) {
		t.Fatalf("expected API token requirement, got %v", err)
	}
	identity := auth.Identity{
		Kind: "api",
		User: auth.User{ID: "user-1", SystemRole: "admin"},
	}
	_, err = service.Claim(context.Background(), identity, ClaimInput{
		JobTypes: []string{"system.test"},
		WorkerID: "worker-1",
	})
	if err != nil {
		t.Fatalf("admin claim: %v", err)
	}
	if !store.claimInput.Admin || store.claimInput.UserID != "user-1" ||
		store.claimInput.LeaseSeconds != 60 {
		t.Fatalf("claim identity/defaults not propagated: %#v", store.claimInput)
	}
}

func TestClaimedWorkerJobRequiresLiveUncancelledLease(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	lease := now.Add(time.Minute)
	store := &memoryStore{job: Job{
		ID: "job-1", ProjectID: "project-1", Status: StatusRunning,
		LockedBy: "worker-1", LeaseExpiresAt: &lease,
	}}
	service := Service{
		Clock: clock.Fixed{Time: now}, Projects: &projectAccessStub{}, Store: store,
	}
	identity := auth.Identity{Kind: "api", User: auth.User{ID: "user-1"}}
	if _, err := service.ClaimedWorkerJob(
		context.Background(), identity, "job-1",
	); err != nil {
		t.Fatalf("live claimed Job: %v", err)
	}

	expired := now
	store.job.LeaseExpiresAt = &expired
	if _, err := service.ClaimedWorkerJob(
		context.Background(), identity, "job-1",
	); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired lease must be rejected: %v", err)
	}

	store.job.LeaseExpiresAt = &lease
	store.job.CancelRequestedAt = &now
	if _, err := service.ClaimedWorkerJob(
		context.Background(), identity, "job-1",
	); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("cancelled lease must be rejected: %v", err)
	}
}

func TestClaimQueryUsesSkipLockedAndDeterministicTypeParameters(t *testing.T) {
	query, args := buildClaimQuery(time.Unix(0, 0), ClaimInput{
		JobTypes: []string{"system.test", "article.build"},
		UserID:   "user-1",
	})
	if !strings.Contains(query, "FOR UPDATE SKIP LOCKED") {
		t.Fatal("claim query must use FOR UPDATE SKIP LOCKED")
	}
	if !strings.Contains(query, "job.job_type IN ($5, $6)") || len(args) != 6 {
		t.Fatalf("unexpected job type filter: %q %#v", query, args)
	}
	if !strings.Contains(query, "ORDER BY job.priority DESC") {
		t.Fatal("claim ordering must be deterministic")
	}
}

func TestProjectScopedAdminTokenDoesNotBypassScope(t *testing.T) {
	store := &memoryStore{job: Job{ID: "job-1", ProjectID: "project-1"}}
	service := Service{Projects: &projectAccessStub{}, Store: store}
	identity := auth.Identity{
		Kind:      "api",
		ProjectID: "project-1",
		User:      auth.User{ID: "user-1", SystemRole: "admin"},
	}
	_, err := service.Claim(context.Background(), identity, ClaimInput{
		JobTypes: []string{"system.test"},
		WorkerID: "worker-1",
	})
	if err != nil {
		t.Fatalf("scoped admin claim: %v", err)
	}
	if store.claimInput.Admin || store.claimInput.ProjectID != "project-1" {
		t.Fatalf("token project scope was not preserved: %#v", store.claimInput)
	}
}

func TestRecoveryCoversCancellationTimeoutAndLeaseWithTargetScope(t *testing.T) {
	tx := &recordingTx{}
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	if err := recoverExpired(context.Background(), tx, now, "job-1"); err != nil {
		t.Fatalf("recover expired: %v", err)
	}
	if len(tx.queries) != 3 {
		t.Fatalf("expected three recovery transitions, got %d", len(tx.queries))
	}
	if !strings.Contains(tx.queries[0], "cancel_requested_at IS NOT NULL") {
		t.Fatal("cancellation must be reconciled first")
	}
	if !strings.Contains(tx.queries[1], "status IN ('queued', 'running')") {
		t.Fatal("timeout must cover running work and delayed retries")
	}
	if !strings.Contains(tx.queries[2], "attempts < max_attempts") {
		t.Fatal("lease recovery must apply max-attempt policy")
	}
	for _, args := range tx.args {
		if len(args) != 2 || args[0] != now || args[1] != "job-1" {
			t.Fatalf("targeted recovery arguments were not propagated: %#v", args)
		}
	}
}

func TestViewerCannotCancelJob(t *testing.T) {
	access := &projectAccessStub{err: project.ErrForbidden}
	store := &memoryStore{job: Job{ID: "job-1", ProjectID: "project-1"}}
	service := Service{Projects: access, Store: store}
	_, err := service.Cancel(
		context.Background(),
		auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}},
		"job-1",
	)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
	if access.permission != project.PermissionJobsCancel {
		t.Fatalf("unexpected permission: %s", access.permission)
	}
}
