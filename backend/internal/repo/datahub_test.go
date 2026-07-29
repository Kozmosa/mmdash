package repo

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type dataHubSinkStub struct {
	commits      []Commit
	deletedHeads []string
	fileBatches  [][]DataHubFile
	repositories []Repository
}

func (sink *dataHubSinkStub) DeleteStaleRepoFiles(
	_ context.Context,
	_ string,
	commitSHA string,
) error {
	sink.deletedHeads = append(sink.deletedHeads, commitSHA)
	return nil
}

func (sink *dataHubSinkStub) ProjectRepoCommit(
	_ context.Context,
	_ contract.EventEnvelope,
	_ Repository,
	_ Workspace,
	commit Commit,
	_ bool,
) error {
	sink.commits = append(sink.commits, commit)
	return nil
}

func (sink *dataHubSinkStub) ProjectRepository(
	_ context.Context,
	_ contract.EventEnvelope,
	repository Repository,
) error {
	sink.repositories = append(sink.repositories, repository)
	return nil
}

func (sink *dataHubSinkStub) UpsertRepoFiles(
	_ context.Context,
	_ Repository,
	_ string,
	files []DataHubFile,
	_ time.Time,
) error {
	sink.fileBatches = append(
		sink.fileBatches, append([]DataHubFile(nil), files...),
	)
	return nil
}

func TestDataHubProjectorIndexesCurrentCodeHeadInBatches(t *testing.T) {
	reader, repository, head := readerFixture(t)
	repository.ProjectID = "00000000-0000-4000-8000-000000000010"
	repository.DisplayName = "acme/research"
	store := &serviceStore{value: repository}
	sink := &dataHubSinkStub{}
	projector := DataHubProjector{
		BatchSize: 100, Reader: &reader,
		Repositories: store, Sink: sink,
	}
	event := repoProjectionEvent(
		"repo.connected", repository.ProjectID, repository.ID,
	)
	if err := projector.Project(context.Background(), event); err != nil {
		t.Fatalf("project repository: %v", err)
	}
	if len(sink.repositories) != 1 ||
		len(sink.commits) != 3 ||
		len(sink.fileBatches) < 2 ||
		len(sink.deletedHeads) != 1 ||
		sink.deletedHeads[0] != head {
		t.Fatalf("unexpected connected projection: %#v", sink)
	}
	fileCount := 0
	foundSpecialPath := false
	for _, batch := range sink.fileBatches {
		if len(batch) > 100 {
			t.Fatalf("file projection batch exceeded limit: %d", len(batch))
		}
		fileCount += len(batch)
		for _, file := range batch {
			if file.Path == "dir/中文 #.txt" {
				foundSpecialPath = true
			}
		}
	}
	if fileCount < 205 || !foundSpecialPath {
		t.Fatalf(
			"current code tree was incomplete: %d special=%v",
			fileCount, foundSpecialPath,
		)
	}
}

func TestDataHubProjectorProjectsOldCommitWithoutReplacingCurrentFiles(
	t *testing.T,
) {
	reader, repository, head := readerFixture(t)
	repository.ProjectID = "00000000-0000-4000-8000-000000000010"
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git is not installed")
	}
	layout, err := reader.Storage.Layout(repository.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	oldHead := runTestGit(
		t, gitPath, layout.Repository,
		"--git-dir="+layout.Bare, "rev-parse", head+"~1",
	)
	sink := &dataHubSinkStub{}
	projector := DataHubProjector{
		BatchSize: 100, Reader: &reader,
		Repositories: &serviceStore{value: repository}, Sink: sink,
	}
	event := repoProjectionEvent(
		"repo.commit.detected", repository.ProjectID, repository.ID,
	)
	event.Payload["workspace"] = "code"
	event.Payload["branch"] = "main"
	event.Payload["commit_sha"] = oldHead
	event.Payload["previous_commit_sha"] = nil
	event.Payload["history_rewritten"] = true
	event.Payload["source"] = "webhook"
	if err := projector.Project(context.Background(), event); err != nil {
		t.Fatalf("project historical commit: %v", err)
	}
	if len(sink.commits) != 1 ||
		sink.commits[0].CommitSHA != oldHead ||
		len(sink.fileBatches) != 0 ||
		len(sink.deletedHeads) != 0 {
		t.Fatalf("historical event replaced current index: %#v", sink)
	}
}

func TestDataHubReaderAdapterUsesAuthorizedImmutableRepoReads(t *testing.T) {
	reader, repository, head := readerFixture(t)
	repository.ProjectID = "project-1"
	access := &serviceAccess{}
	service := &Service{
		Access: access, Reads: &reader,
		Store: &serviceStore{value: repository},
	}
	adapter := DataHubReaderAdapter{Service: service}
	caller := auth.Identity{User: auth.User{ID: "user-1"}}
	commit, err := adapter.Commit(
		context.Background(), caller, repository.ProjectID,
		map[string]interface{}{"commit_sha": head},
	)
	if err != nil || commit.CommitSHA != head {
		t.Fatalf("read projected commit: %#v %v", commit, err)
	}
	file, err := adapter.File(
		context.Background(), caller, repository.ProjectID,
		map[string]interface{}{
			"commit_sha": head,
			"path":       "README.md",
		},
	)
	if err != nil || file.ResolvedRevision != head ||
		file.Content == nil {
		t.Fatalf("read projected file: %#v %v", file, err)
	}
	if access.permission != project.PermissionRepoRead {
		t.Fatalf("unexpected Reader permission: %s", access.permission)
	}
}

func repoProjectionEvent(
	eventType string,
	projectID string,
	repositoryID string,
) contract.EventEnvelope {
	return contract.EventEnvelope{
		Actor:     map[string]string{},
		EventID:   "00000000-0000-4000-8000-000000000091",
		EventType: eventType,
		OccurredAt: time.Date(
			2026, time.July, 29, 17, 0, 0, 0, time.UTC,
		),
		Payload: map[string]interface{}{
			"repository_id": repositoryID,
		},
		Producer: "repo", ProjectID: &projectID, SchemaVersion: 1,
	}
}
