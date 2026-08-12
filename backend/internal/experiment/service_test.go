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
	item       Experiment
	queuedTask boxcontrol.Task
	result     boxcontrol.Result
}

func (store *experimentTestStore) Create(_ context.Context, item Experiment) (Experiment, bool, error) {
	store.item = item
	return item, true, nil
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
func (store *experimentTestStore) Queue(context.Context, string, string, time.Time) (Experiment, error) {
	store.item.Status, store.item.TaskID = StatusQueued, store.queuedTask.ID
	return store.item, nil
}
func (store *experimentTestStore) QueueWithTask(_ context.Context, item Experiment, task boxcontrol.Task, _ time.Time) (Experiment, error) {
	store.queuedTask = task
	store.item = item
	store.item.Status, store.item.TaskID = StatusQueued, task.ID
	return store.item, nil
}
func (store *experimentTestStore) Cancel(context.Context, string, string, time.Time) (Experiment, error) {
	store.item.Status = StatusCanceled
	return store.item, nil
}
func (store *experimentTestStore) Archive(context.Context, string, string, time.Time) (Experiment, error) {
	store.item.Status = StatusArchived
	return store.item, nil
}
func (store *experimentTestStore) ApplyTaskStatus(context.Context, boxcontrol.Task, time.Time) (Experiment, error) {
	return store.item, nil
}
func (store *experimentTestStore) ApplyResult(_ context.Context, _ boxcontrol.Task, result boxcontrol.Result, _ time.Time) (Experiment, error) {
	store.result = result
	return store.item, nil
}
func (store *experimentTestStore) Compare(context.Context, string, []string) (Comparison, error) {
	return Comparison{Items: []Experiment{store.item}}, nil
}

type experimentTestArtifact struct{}

func (experimentTestArtifact) ArchiveExperimentResult(context.Context, string, string, string, string, int64, io.Reader) (artifact.Detail, error) {
	return artifact.Detail{Artifact: artifact.Artifact{ID: "artifact-1"}, CurrentVersion: &artifact.Version{ID: "version-1", Filename: "artifact.zip", SHA256: strings.Repeat("a", 64), SizeBytes: 10}}, nil
}

func TestCreateRejectsMutableEntrypointsAndCommits(t *testing.T) {
	service := Service{Access: experimentTestAccess{}, Clock: fixedExperimentClock{}, Generator: &experimentTestGenerator{}, Store: &experimentTestStore{}}
	identity := auth.Identity{Kind: "session", ProjectID: "project-1", User: auth.User{ID: "user-1"}}
	base := Experiment{
		Name: "run", SourceCommit: strings.Repeat("a", 40), Entrypoint: "python:run.py", Parameters: map[string]interface{}{}, Environment: map[string]string{}, Inputs: map[string]interface{}{}, Runtime: "local-docker",
		Limits: ResourceLimits{CPUMillis: 500, MemoryBytes: 1 << 20, TimeoutSecond: 60, DiskBytes: 1 << 20, PIDs: 32, Network: "disabled"}, IdempotencyKey: "run-1", MaxAttempts: 1,
	}
	created, err := service.Create(context.Background(), identity, "project-1", base)
	if err != nil || created.Status != StatusCreated {
		t.Fatalf("create: %#v %v", created, err)
	}
	for _, entrypoint := range []string{"python:run.py;rm -rf /", "sh:run.sh", "python:../run.py"} {
		input := base
		input.Entrypoint = entrypoint
		if _, err := service.Create(context.Background(), identity, "project-1", input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("entrypoint %q was accepted: %v", entrypoint, err)
		}
	}
}

func TestRunQueuesOnlyOnceAndFreezesTheRunSpec(t *testing.T) {
	store := &experimentTestStore{item: Experiment{ID: "experiment-1", ProjectID: "project-1", SourceCommit: strings.Repeat("a", 40), Entrypoint: "python:run.py", Parameters: map[string]interface{}{"alpha": 1}, Environment: map[string]string{}, Inputs: map[string]interface{}{}, Runtime: "local-docker", Limits: ResourceLimits{CPUMillis: 1, MemoryBytes: 1 << 20, TimeoutSecond: 1, DiskBytes: 1 << 20, PIDs: 1, Network: "disabled"}, Status: StatusCreated, MaxAttempts: 1}}
	service := Service{Access: experimentTestAccess{}, Boxes: &boxcontrol.Service{}, Clock: fixedExperimentClock{}, Generator: &experimentTestGenerator{}, Store: store}
	identity := auth.Identity{Kind: "session", ProjectID: "project-1", User: auth.User{ID: "user-1"}}
	queued, err := service.Run(context.Background(), identity, "project-1", "experiment-1")
	if err != nil || queued.Status != StatusQueued {
		t.Fatalf("run: %#v %v", queued, err)
	}
	if store.queuedTask.RunSpec["source_commit"] != strings.Repeat("a", 40) || store.queuedTask.RunSpec["entrypoint"] != "python:run.py" {
		t.Fatalf("run spec was not frozen: %#v", store.queuedTask.RunSpec)
	}
	second, err := service.Run(context.Background(), identity, "project-1", "experiment-1")
	if err != nil || second.TaskID != queued.TaskID {
		t.Fatalf("second run was not idempotent: %#v %v", second, err)
	}
}

func TestTaskResultAndArchiveArtifactRequireValidatedPointers(t *testing.T) {
	store := &experimentTestStore{item: Experiment{ID: "experiment-1", ProjectID: "project-1", CreatedBy: "user-1"}}
	service := Service{Store: store, Artifacts: experimentTestArtifact{}}
	task := boxcontrol.Task{ID: "task-1", ExperimentID: "experiment-1", ProjectID: "project-1"}
	if err := service.TaskResult(context.Background(), task, boxcontrol.Result{Manifest: map[string]interface{}{"experiment_id": "other", "status": "succeeded"}, Artifact: map[string]interface{}{}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign manifest was accepted: %v", err)
	}
	if err := service.TaskResult(context.Background(), task, boxcontrol.Result{Manifest: map[string]interface{}{"schema_version": "1", "experiment_id": "experiment-1", "status": "succeeded", "files": []interface{}{}}, Artifact: map[string]interface{}{
		"artifact_id": "00000000-0000-4000-8000-000000000001", "version_id": "00000000-0000-4000-8000-000000000002", "filename": "artifact.zip", "sha256": strings.Repeat("a", 64), "size_bytes": int64(1),
	}}); err != nil {
		t.Fatalf("valid task result rejected: %v", err)
	}
	pointer, err := service.ArchiveArtifact(context.Background(), task, strings.Repeat("a", 64), 10, strings.NewReader("artifact"))
	if err != nil || pointer["filename"] != "artifact.zip" {
		t.Fatalf("artifact archive: %#v %v", pointer, err)
	}
}

func TestExperimentTransitionsIncludeLeaseRecovery(t *testing.T) {
	valid := [][2]string{
		{StatusCreated, StatusQueued}, {StatusQueued, StatusPreparing},
		{StatusQueued, StatusFailed}, {StatusPreparing, StatusQueued},
		{StatusPreparing, StatusRunning}, {StatusRunning, StatusQueued},
		{StatusRunning, StatusSucceeded}, {StatusRunning, StatusFailed},
	}
	for _, pair := range valid {
		if !validTransition(pair[0], pair[1]) {
			t.Fatalf("expected transition %s -> %s", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{{StatusCreated, StatusRunning}, {StatusSucceeded, StatusRunning}, {StatusFailed, StatusSucceeded}, {StatusArchived, StatusSucceeded}} {
		if validTransition(pair[0], pair[1]) {
			t.Fatalf("unexpected transition %s -> %s", pair[0], pair[1])
		}
	}
}

type fixedExperimentClock struct{}

func (fixedExperimentClock) Now() time.Time { return time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC) }
