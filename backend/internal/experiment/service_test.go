package experiment

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/artifact"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/boxcontrol"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/repo"
)

type experimentTestAccess struct{ err error }

func (access experimentTestAccess) Authenticate(context.Context, string) (auth.Identity, error) {
	return auth.Identity{Kind: "session", ProjectID: "project-1", User: auth.User{ID: "user-1"}}, nil
}
func (access experimentTestAccess) Authorize(context.Context, auth.Identity, string, project.Permission) error {
	return access.err
}

type experimentTestGenerator struct{ next int }

func (generator *experimentTestGenerator) New() (string, error) {
	generator.next++
	return "00000000-0000-4000-8000-00000000000" + string(rune('0'+generator.next)), nil
}

type experimentTestStore struct {
	Store
	item       Experiment
	queuedTask boxcontrol.Task
	result     boxcontrol.Result
	settings   Settings
}

func (store *experimentTestStore) GetSettings(context.Context, string) (Settings, error) {
	if store.settings.ProjectID == "" {
		store.settings = Settings{
			ProjectID: "project-1", Timezone: "Asia/Shanghai",
			DefaultRuntimePolicy: "auto",
			DefaultLimits: ResourceLimits{
				CPUMillis: 1000, MemoryBytes: 1 << 30, TimeoutSecond: 3600,
				DiskBytes: 10 << 30, PIDs: 256, Network: "disabled",
			},
			GitLargeFileThresholdBytes: 50 << 20,
		}
	}
	return store.settings, nil
}

func (store *experimentTestStore) Create(_ context.Context, item Experiment) (Experiment, bool, error) {
	store.item = item
	return item, true, nil
}

func (store *experimentTestStore) CreateRerun(_ context.Context, previous, next Experiment, _ time.Time) (Experiment, bool, error) {
	previous.Retry.SupersededByExperimentID = next.ID
	previous.Retry.LatestExperimentID = next.ID
	store.item = next
	return next, true, nil
}

func (store *experimentTestStore) Get(context.Context, string, string) (Experiment, error) {
	if store.item.ID == "" {
		return Experiment{}, ErrNotFound
	}
	return store.item, nil
}

func (store *experimentTestStore) List(context.Context, string, string, int, int) (Page, error) {
	return Page{Items: []Experiment{store.item}}, nil
}

func (store *experimentTestStore) QueueWithTask(_ context.Context, item Experiment, task boxcontrol.Task, key string, _ time.Time) (Experiment, error) {
	if item.ExecutionStatus != StatusCreated {
		if item.RunIdempotencyKey == key {
			return item, nil
		}
		return Experiment{}, ErrConflict
	}
	store.queuedTask = task
	store.item = item
	store.item.ExecutionStatus, store.item.TaskID = StatusQueued, task.ID
	store.item.RunIdempotencyKey = key
	return store.item, nil
}

func (store *experimentTestStore) Cancel(context.Context, string, string, time.Time) (Experiment, error) {
	store.item.ExecutionStatus = StatusCanceled
	return store.item, nil
}

func (store *experimentTestStore) Archive(context.Context, string, string, time.Time) (Experiment, error) {
	store.item.ExecutionStatus = StatusArchived
	return store.item, nil
}

func (store *experimentTestStore) ApplyTaskStatus(context.Context, boxcontrol.Task, time.Time) (Experiment, error) {
	return store.item, nil
}

func (store *experimentTestStore) ApplyResult(_ context.Context, _ boxcontrol.Task, result boxcontrol.Result, _ time.Time) (Experiment, error) {
	store.result = result
	store.item.ExecutionStatus = StatusProcessingResult
	return store.item, nil
}

func (store *experimentTestStore) Compare(context.Context, string, []string) (Comparison, error) {
	return Comparison{Items: []Experiment{store.item}}, nil
}

type experimentTestArtifact struct{}

type resultRepoStub struct {
	reverted repo.ResultRevertRequest
}

func (stub *resultRepoStub) ResolveHead(context.Context, string) (repo.Revision, error) {
	return repo.Revision{}, nil
}
func (stub *resultRepoStub) Commit(context.Context, repo.ResultCommitRequest) (repo.CommitResult, error) {
	return repo.CommitResult{}, nil
}
func (stub *resultRepoStub) Revert(_ context.Context, input repo.ResultRevertRequest) (repo.CommitResult, error) {
	stub.reverted = input
	return repo.CommitResult{CommitSHA: strings.Repeat("c", 40)}, nil
}

func (experimentTestArtifact) ArchiveExperimentResult(context.Context, string, string, string, string, int64, io.Reader) (artifact.Detail, error) {
	return artifact.Detail{
		Artifact: artifact.Artifact{ID: "artifact-1"},
		CurrentVersion: &artifact.Version{
			ID: "version-1", Filename: "execution-bundle.zip",
			SHA256: strings.Repeat("a", 64), SizeBytes: 10,
		},
	}, nil
}

func TestCreateFreezesDefaultsTimezoneAndSelfInstructions(t *testing.T) {
	store := &experimentTestStore{}
	service := Service{
		Access: experimentTestAccess{}, Clock: fixedExperimentClock{},
		Generator: &experimentTestGenerator{}, Store: store,
	}
	identity := auth.Identity{Kind: "session", ProjectID: "project-1", User: auth.User{ID: "user-1"}}
	base := Experiment{
		Name: "run", Type: TypeSelf, SourceCommit: strings.Repeat("a", 40),
		Entrypoint: "python:run.py", Parameters: map[string]interface{}{},
		Environment: map[string]string{}, Inputs: map[string]interface{}{},
		IdempotencyKey: "run-1",
	}
	created, err := service.Create(context.Background(), identity, "project-1", base)
	if err != nil || created.ExecutionStatus != StatusAwaitingResult {
		t.Fatalf("create: %#v %v", created, err)
	}
	if created.ResultDirectory != "experiments/00000000-0000-4000-8000-000000000001_20260811_0800/" ||
		created.ProjectTimezone != "Asia/Shanghai" || created.ResultContract == nil ||
		!strings.Contains(created.ResultContract.Instructions, "commit 并 push") {
		t.Fatalf("self result contract was not frozen: %#v", created)
	}
	for _, entrypoint := range []string{"python:run.py;rm -rf /", "sh:run.sh", "python:../run.py"} {
		input := base
		input.Entrypoint = entrypoint
		if _, err := service.Create(context.Background(), identity, "project-1", input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("entrypoint %q was accepted: %v", entrypoint, err)
		}
	}
}

func TestRunQueuesOnceAndFreezesRuntimePolicy(t *testing.T) {
	store := &experimentTestStore{item: Experiment{
		ID: "experiment-1", ProjectID: "project-1", Type: TypeBox,
		ExecutionStatus: StatusCreated, SourceCommit: strings.Repeat("a", 40),
		Entrypoint: "python:run.py", Parameters: map[string]interface{}{"alpha": 1},
		Environment: map[string]string{}, Inputs: map[string]interface{}{},
		RequestedRuntimePolicy: "auto", RequestedBoxID: "00000000-0000-4000-8000-000000000099",
		Limits:          ResourceLimits{CPUMillis: 1, MemoryBytes: 1 << 20, TimeoutSecond: 1, DiskBytes: 1 << 20, PIDs: 1, Network: "disabled"},
		ResultDirectory: "experiments/experiment-1_20260811_0800/", MaxAttempts: 1,
	}}
	service := Service{
		Access: experimentTestAccess{}, Boxes: &boxcontrol.Service{},
		Clock: fixedExperimentClock{}, Generator: &experimentTestGenerator{}, Store: store,
	}
	identity := auth.Identity{Kind: "session", ProjectID: "project-1", User: auth.User{ID: "user-1"}}
	queued, err := service.Run(context.Background(), identity, "project-1", "experiment-1", "run-once")
	if err != nil || queued.ExecutionStatus != StatusQueued {
		t.Fatalf("run: %#v %v", queued, err)
	}
	if store.queuedTask.RunSpec["runtime_policy"] != "auto" ||
		store.queuedTask.RunSpec["requested_box_id"] == "" ||
		store.queuedTask.RunSpec["source_commit"] != strings.Repeat("a", 40) {
		t.Fatalf("run spec was not frozen: %#v", store.queuedTask.RunSpec)
	}
	second, err := service.Run(context.Background(), identity, "project-1", "experiment-1", "run-once")
	if err != nil || second.TaskID != queued.TaskID {
		t.Fatalf("same run key was not idempotent: %#v %v", second, err)
	}
	if _, err := service.Run(context.Background(), identity, "project-1", "experiment-1", "different"); !errors.Is(err, ErrConflict) {
		t.Fatalf("different run key did not conflict: %v", err)
	}
}

func TestForceRevokeCompensatesPushedUnboundResult(t *testing.T) {
	store := &experimentTestStore{item: Experiment{
		ID: "experiment-1", ProjectID: "project-1", CreatedBy: "user-1",
		ExecutionStatus: StatusFailed, ResultDirectory: "experiments/experiment-1_20260811_0800/",
		StagingCommitSHA: strings.Repeat("a", 40),
		StagingPaths: []string{
			"experiments/experiment-1_20260811_0800/manifest.json",
			"experiments/experiment-1_20260811_0800/result.csv",
		},
	}}
	repository := &resultRepoStub{}
	service := Service{Clock: fixedExperimentClock{}, ResultRepo: repository, Store: store}
	task := boxcontrol.Task{Status: boxcontrol.TaskFailed, Failure: &boxcontrol.Failure{Code: "BOX_FORCE_REVOKED"}}

	if err := service.TaskStatus(context.Background(), task); err != nil {
		t.Fatalf("compensate revoked result: %v", err)
	}
	if repository.reverted.ExperimentID != "experiment-1" || len(repository.reverted.Paths) != 2 {
		t.Fatalf("compensating revert was not issued: %#v", repository.reverted)
	}
}

func TestRerunCreatesNewBoxReIdentityAndLineage(t *testing.T) {
	store := &experimentTestStore{item: Experiment{
		ID: "00000000-0000-4000-8000-000000000010", ProjectID: "project-1",
		Type: TypeBox, ExecutionStatus: StatusFailed, Name: "old",
		SourceCommit: strings.Repeat("a", 40), Entrypoint: "python:run.py",
		Parameters: map[string]interface{}{}, Environment: map[string]string{},
		Inputs: map[string]interface{}{}, RequestedRuntimePolicy: "e2b",
		Limits:          ResourceLimits{CPUMillis: 1, MemoryBytes: 1 << 20, TimeoutSecond: 1, DiskBytes: 1 << 20, PIDs: 1, Network: "disabled"},
		ProjectTimezone: "Asia/Shanghai", GitLargeFileThreshold: 50 << 20,
		Retry: Retry{RootExperimentID: "00000000-0000-4000-8000-000000000010", LatestExperimentID: "00000000-0000-4000-8000-000000000010"},
	}}
	service := Service{Access: experimentTestAccess{}, Clock: fixedExperimentClock{}, Generator: &experimentTestGenerator{}, Store: store}
	identity := auth.Identity{Kind: "session", ProjectID: "project-1", User: auth.User{ID: "user-1"}}
	name := "retry"
	created, err := service.Rerun(context.Background(), identity, "project-1", store.item.ID, RerunOverrides{Name: &name, IdempotencyKey: "retry-1"})
	if err != nil || created.Type != TypeBoxRe || created.ID == "00000000-0000-4000-8000-000000000010" || created.Retry.RetrySequence != 1 || created.Retry.RetryOfExperimentID == "" {
		t.Fatalf("rerun lineage: %#v %v", created, err)
	}
}

func TestTaskResultAndBundleArchiveUseImmutableExecutionBundle(t *testing.T) {
	store := &experimentTestStore{item: Experiment{ID: "experiment-1", ProjectID: "project-1", CreatedBy: "user-1"}}
	service := Service{Store: store, Artifacts: experimentTestArtifact{}}
	task := boxcontrol.Task{ID: "task-1", ExperimentID: "experiment-1", ProjectID: "project-1"}
	if err := service.TaskResult(context.Background(), task, boxcontrol.Result{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty result was accepted: %v", err)
	}
	result := boxcontrol.Result{
		ExecutionEpoch: "00000000-0000-4000-8000-000000000003",
		ManifestSHA256: strings.Repeat("b", 64),
		ExecutionBundle: boxcontrol.ArtifactPointer{
			ArtifactID: "00000000-0000-4000-8000-000000000001",
			VersionID:  "00000000-0000-4000-8000-000000000002",
			Filename:   "execution-bundle.zip", SHA256: strings.Repeat("a", 64), SizeBytes: 1,
		},
	}
	if err := service.TaskResult(context.Background(), task, result); err != nil {
		t.Fatalf("valid task result rejected: %v", err)
	}
	pointer, err := service.ArchiveArtifact(context.Background(), task, strings.Repeat("a", 64), 10, strings.NewReader("artifact"))
	if err != nil || pointer["filename"] != "execution-bundle.zip" {
		t.Fatalf("bundle archive: %#v %v", pointer, err)
	}
}

func TestTransitionsNeverRequeueAfterRuntimeStarts(t *testing.T) {
	for _, pair := range [][2]string{
		{StatusQueued, StatusPreparing}, {StatusPreparing, StatusRunning},
		{StatusRunning, StatusUploading}, {StatusRunning, StatusFailed},
		{StatusUploading, StatusTimedOut},
	} {
		if !validTransition(pair[0], pair[1]) {
			t.Fatalf("expected transition %s -> %s", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{
		{StatusPreparing, StatusQueued}, {StatusRunning, StatusQueued},
		{StatusRunning, StatusSucceeded}, {StatusFailed, StatusSucceeded},
	} {
		if validTransition(pair[0], pair[1]) {
			t.Fatalf("unexpected transition %s -> %s", pair[0], pair[1])
		}
	}
}

type fixedExperimentClock struct{}

func (fixedExperimentClock) Now() time.Time {
	return time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
}
