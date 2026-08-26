package article

import (
	"context"
	"strings"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/jobs"
)

type previewSnapshotTestStore struct {
	*articleTestStore
	jobInput jobs.CreateInput
}

func (store *previewSnapshotTestStore) CreateBuild(
	ctx context.Context,
	item Build,
	jobInput jobs.CreateInput,
	writer jobs.TransactionalWriter,
) (Build, bool, error) {
	store.jobInput = jobInput
	return store.articleTestStore.CreateBuild(ctx, item, jobInput, writer)
}

func TestPreviewWorkerInputUsesFrozenSnapshotAfterDraftAdvances(t *testing.T) {
	frozenDraft := draftAt(4)
	frozenDraft.Markdown = "# Frozen preview\n"
	frozenDraft.Manifest = map[string]interface{}{
		"schema_version": "1.0",
		"draft_revision": int64(4),
	}
	frozenReference := Reference{
		CitationKey:     "frozen",
		ReferenceType:   "artifact",
		SourceObjectID:  "artifact-frozen",
		SourceVersionID: "version-frozen",
		Title:           "Frozen figure",
	}
	base := &articleTestStore{
		draft:      frozenDraft,
		references: []Reference{frozenReference},
		template: Template{
			TemplateID: "template-1",
			VersionID:  "template-version",
			ArtifactID: "template-artifact",
			Status:     "ready",
			Manifest:   TemplateManifest{Name: "Template"},
		},
	}
	store := &previewSnapshotTestStore{articleTestStore: base}
	service := testService(store, &articleTestWorkspace{})
	artifacts := &articleTestArtifacts{}
	service.Artifacts = artifacts

	preview, created, err := service.CreatePreview(
		context.Background(),
		human(),
		"project-1",
		4,
		"template-1",
		"auto",
		"auto",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("preview build was not created")
	}
	if store.jobInput.Payload["preview_snapshot"] == nil {
		t.Fatalf("preview job did not freeze a snapshot: %#v", store.jobInput.Payload)
	}
	if len(base.builds) != 1 {
		t.Fatalf("expected one preview build, got %#v", base.builds)
	}

	changedDraft := draftAt(5)
	changedDraft.Markdown = "# Changed after queue\n"
	changedDraft.Manifest = map[string]interface{}{
		"schema_version": "1.0",
		"draft_revision": int64(5),
	}
	base.draft = changedDraft
	base.references = []Reference{{
		ReferenceType:   "artifact",
		SourceObjectID:  "artifact-new",
		SourceVersionID: "version-new",
		Title:           "New figure",
	}}
	base.builds[0].JobID = "job-1"

	service.JobAccess = articleTestJobAccess{job: jobs.Job{
		ID:        "job-1",
		JobType:   JobTypeBuild,
		ProjectID: "project-1",
		Payload:   store.jobInput.Payload,
	}}

	input, err := service.WorkerInput(
		context.Background(),
		human(),
		"job-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if input.BuildID != preview.BuildID {
		t.Fatalf("worker received wrong build: %#v", input)
	}
	if input.Manuscript != "# Frozen preview\n" {
		t.Fatalf("worker did not use frozen manuscript: %q", input.Manuscript)
	}
	if !strings.Contains(input.ReferencesBIB, "@misc{frozen") {
		t.Fatalf("worker did not use frozen bibliography: %q", input.ReferencesBIB)
	}
	if input.ArticleManifest["draft_revision"] != float64(4) {
		t.Fatalf("worker did not use frozen manifest: %#v", input.ArticleManifest)
	}
	if len(artifacts.resourceCalls) != 1 ||
		artifacts.resourceCalls[0] != [2]string{"artifact-frozen", "version-frozen"} {
		t.Fatalf(
			"worker did not use frozen artifact version: %#v",
			artifacts.resourceCalls,
		)
	}
}
