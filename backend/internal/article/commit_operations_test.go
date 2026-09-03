package article

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/repo"
)

type commitOperationStoreStub struct {
	Store
	bindErr     error
	claims      []CommitOperation
	completed   *Commit
	failureCode string
	finished    bool
	failed      *CommitOperation
	publication *Publication
	requestID   string
	terminal    bool
}

func (store *commitOperationStoreStub) ClaimCommitOperations(
	context.Context, string, time.Time, time.Duration, int,
) ([]CommitOperation, error) {
	claims := store.claims
	store.claims = nil
	return claims, nil
}

func (*commitOperationStoreStub) RenewCommitOperationLease(
	context.Context, string, string, time.Time,
) error {
	return nil
}

func (store *commitOperationStoreStub) BindCommitOperation(
	ctx context.Context, _ CommitOperation, item Commit, _ time.Time,
) (Commit, error) {
	store.requestID = requestctx.RequestID(ctx)
	if store.bindErr != nil {
		return Commit{}, store.bindErr
	}
	store.completed = &item
	return item, nil
}

func (store *commitOperationStoreStub) GetCommit(
	_ context.Context, _, _ string,
) (Commit, error) {
	if store.completed == nil {
		return Commit{}, ErrNotFound
	}
	return *store.completed, nil
}

func (*commitOperationStoreStub) GetTemplate(
	context.Context, string, string,
) (Template, error) {
	return Template{
		TemplateID: "template-1", ArtifactID: "artifact-1",
		VersionID: "version-1", Status: "ready",
	}, nil
}

func (store *commitOperationStoreStub) CreatePublicationBuild(
	_ context.Context, publication Publication, _ Build, _ jobs.CreateInput,
	_ jobs.TransactionalWriter,
) (Publication, bool, error) {
	store.publication = &publication
	return publication, true, nil
}

func (store *commitOperationStoreStub) CompleteCommitOperation(
	_ context.Context, _ CommitOperation, _ time.Time,
) error {
	store.finished = true
	return nil
}

func (store *commitOperationStoreStub) FailCommitOperation(
	_ context.Context,
	operation CommitOperation,
	code string,
	terminal bool,
	_ time.Time,
	_ time.Time,
) error {
	store.failed = &operation
	store.failureCode = code
	store.terminal = terminal
	return nil
}

type commitOperationWorkspaceStub struct {
	err     error
	request repo.WorkspaceCommitRequest
}

func (*commitOperationWorkspaceStub) ResolveHead(context.Context, string) (repo.Revision, error) {
	return repo.Revision{}, nil
}
func (*commitOperationWorkspaceStub) ListTree(context.Context, string, string, string) ([]repo.TreeEntry, error) {
	return nil, nil
}
func (*commitOperationWorkspaceStub) ReadFile(context.Context, string, string, string) (repo.FileContent, error) {
	return repo.FileContent{}, nil
}
func (workspace *commitOperationWorkspaceStub) Commit(
	_ context.Context,
	request repo.WorkspaceCommitRequest,
) (repo.CommitResult, error) {
	workspace.request = request
	if workspace.err != nil {
		return repo.CommitResult{}, workspace.err
	}
	return repo.CommitResult{
		CommitSHA:         strings.Repeat("b", 40),
		PreviousCommitSHA: request.ExpectedHeadSHA,
		Workspace:         repo.WorkspaceArticle,
	}, nil
}

func TestCommitOperationCoordinatorCommitsFrozenSnapshotAndBindsResult(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	operation := CommitOperation{
		OperationID: "operation-1", CommitID: "commit-1", ProjectID: "project-1",
		OperationKind: "commit",
		DraftRevision: 7, ExpectedHeadSHA: strings.Repeat("a", 40),
		StateVector: "vector", YjsUpdate: "update",
		TiptapJSON: map[string]interface{}{"type": "doc"},
		Manuscript: "# Frozen\n", ReferencesBIB: "@book{x}\n",
		ManifestBytes: []byte("{}\n"), Message: "checkpoint",
		RequestSHA256:    strings.Repeat("c", 64),
		ManuscriptSHA256: strings.Repeat("d", 64),
		ReferencesSHA256: strings.Repeat("e", 64),
		ManifestSHA256:   strings.Repeat("f", 64), Attempts: 1,
		LockedBy: "worker-1", CreatedBy: "user-1", CreatedAt: now,
	}
	store := &commitOperationStoreStub{claims: []CommitOperation{operation}}
	workspace := &commitOperationWorkspaceStub{}
	coordinator := CommitOperationCoordinator{
		Clock: clock.Fixed{Time: now}, Lease: 90 * time.Second, Limit: 1,
		Owner: "worker-1", Service: &Service{Workspace: workspace}, Store: store,
	}

	if err := coordinator.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.completed == nil || !store.finished || store.completed.CommitSHA != strings.Repeat("b", 40) ||
		store.completed.DraftRevision != 7 || store.failed != nil {
		t.Fatalf("operation was not completed: %#v %#v", store.completed, store.failed)
	}
	if store.requestID != "article-operation:operation-1" {
		t.Fatalf("background operation lost its audit request ID: %q", store.requestID)
	}
	if len(workspace.request.Changes) != 3 ||
		string(workspace.request.Changes[0].Content) != "# Frozen\n" ||
		workspace.request.ExpectedHeadSHA != operation.ExpectedHeadSHA {
		t.Fatalf("Repo did not receive the frozen snapshot: %#v", workspace.request)
	}
}

func TestCommitOperationCoordinatorPersistsBindFailureState(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	operation := CommitOperation{
		OperationID: "operation-1", CommitID: "commit-1", ProjectID: "project-1",
		OperationKind: "commit", ExpectedHeadSHA: strings.Repeat("a", 40),
		RequestSHA256: strings.Repeat("c", 64), Manuscript: "# Frozen\n",
		ManifestBytes: []byte("{}\n"), Message: "checkpoint",
		LockedBy: "worker-1", CreatedBy: "user-1", CreatedAt: now, Attempts: 10,
	}
	store := &commitOperationStoreStub{
		bindErr: errors.New("audit unavailable"),
		claims:  []CommitOperation{operation},
	}
	coordinator := CommitOperationCoordinator{
		Clock: clock.Fixed{Time: now}, Lease: 90 * time.Second, Limit: 1,
		Owner: "worker-1", Service: &Service{Workspace: &commitOperationWorkspaceStub{}}, Store: store,
	}

	if err := coordinator.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.failed == nil || store.failureCode != "ARTICLE_COMMIT_PERSIST_FAILED" {
		t.Fatalf("bind failure was not persisted: %#v %q", store.failed, store.failureCode)
	}
}

func TestScanCommitOperationStatusIncludesDraftRevision(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	finished := now.Add(time.Minute)
	operation, err := scanCommitOperationStatus(func(destinations ...interface{}) error {
		*destinations[0].(*string) = "operation-1"
		*destinations[1].(*string) = "commit-1"
		*destinations[2].(*string) = "project-1"
		*destinations[3].(*string) = "commit"
		*destinations[4].(*string) = ""
		*destinations[5].(*int64) = 42
		*destinations[6].(*string) = "succeeded"
		*destinations[7].(*string) = "completed"
		*destinations[8].(*string) = strings.Repeat("b", 40)
		*destinations[9].(*string) = ""
		*destinations[10].(*int) = 1
		*destinations[11].(*int) = 10
		*destinations[12].(*time.Time) = now
		*destinations[13].(*time.Time) = now
		*destinations[14].(*time.Time) = now
		*destinations[15].(**time.Time) = &finished
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if operation.DraftRevision != 42 || operation.Status != "succeeded" || operation.CommitSHA == "" {
		t.Fatalf("operation status projection lost persisted fields: %#v", operation)
	}
}

func TestCommitOperationCoordinatorRetriesTransientRepoLock(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	operation := CommitOperation{
		OperationID: "operation-1", ProjectID: "project-1", Attempts: 1,
		OperationKind: "commit",
		LockedBy:      "worker-1", CreatedAt: now,
	}
	store := &commitOperationStoreStub{claims: []CommitOperation{operation}}
	workspace := &commitOperationWorkspaceStub{err: repo.ErrLocked}
	coordinator := CommitOperationCoordinator{
		Clock: clock.Fixed{Time: now}, Lease: 90 * time.Second, Limit: 1,
		Owner: "worker-1", Service: &Service{Workspace: workspace}, Store: store,
	}

	if err := coordinator.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.failed == nil || store.terminal {
		t.Fatalf("transient Repo lock was not scheduled for retry: %#v", store.failed)
	}
	if !errors.Is(workspace.err, repo.ErrLocked) {
		t.Fatal("test setup lost the Repo lock error")
	}
}

func TestPublicationOperationContinuesAfterConfirmedCommit(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	operation := CommitOperation{
		OperationID: "operation-1", CommitID: "commit-1", ProjectID: "project-1",
		OperationKind: "publication", PublicationID: "publication-1",
		PublicationKey: "publish-1", TemplateID: "template-1", Engine: "auto",
		BibliographyTool: "auto", Tag: "v1", Title: "Paper", Notes: "accepted",
		DraftRevision: 7, ExpectedHeadSHA: strings.Repeat("a", 40),
		RequestSHA256: strings.Repeat("c", 64), Manuscript: "# Frozen\n",
		ReferencesBIB: "", ManifestBytes: []byte("{}\n"), Message: "publish",
		LockedBy: "worker-1", CreatedBy: "user-1", CreatedAt: now, Attempts: 1,
	}
	store := &commitOperationStoreStub{claims: []CommitOperation{operation}}
	workspace := &commitOperationWorkspaceStub{}
	seed := make([]byte, 16*4)
	service := &Service{
		Clock: clock.Fixed{Time: now}, Generator: identity.Generator{Reader: bytes.NewReader(seed)},
		Store: store, Workspace: workspace,
	}
	coordinator := CommitOperationCoordinator{
		Clock: clock.Fixed{Time: now}, Lease: 90 * time.Second, Limit: 1,
		Owner: "worker-1", Service: service, Store: store,
	}

	if err := coordinator.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.publication == nil || store.publication.PublicationID != "publication-1" ||
		store.publication.CommitID != "commit-1" || !store.finished {
		t.Fatalf("publication continuation was not completed: %#v", store.publication)
	}
}
