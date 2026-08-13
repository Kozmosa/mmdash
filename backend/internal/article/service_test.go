package article

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/repo"
)

type articleTestAccess struct{ denied project.Permission }

func (articleTestAccess) Authenticate(context.Context, string) (auth.Identity, error) {
	return human(), nil
}
func (access articleTestAccess) Authorize(_ context.Context, _ auth.Identity, _ string, permission project.Permission) error {
	if permission == access.denied {
		return errors.New("denied")
	}
	return nil
}

type articleTestStore struct {
	Store
	accepted      bool
	builds        []Build
	commit        Commit
	createdCommit Commit
	draft         Draft
	outputs       []BuildOutput
	persisted     PersistDraftInput
	publications  []Publication
	references    []Reference
	releases      []Release
	template      Template
	templateErr   error
}

func (store *articleTestStore) GetDraft(context.Context, string) (Draft, error) {
	return store.draft, nil
}
func (store *articleTestStore) ListReferences(context.Context, string) ([]Reference, error) {
	return store.references, nil
}
func (store *articleTestStore) PersistDraft(_ context.Context, projectID, _ string, input PersistDraftInput, markdown string, blocks []Block, manifest map[string]interface{}, references string) (Draft, error) {
	store.persisted = input
	store.draft = Draft{ProjectID: projectID, DraftRevision: input.ExpectedRevision + 1, StateVector: input.StateVector, YjsUpdate: input.YjsUpdate, TiptapJSON: input.TiptapJSON, Markdown: markdown, Blocks: blocks, Manifest: manifest, ReferencesBIB: references}
	return store.draft, nil
}
func (store *articleTestStore) CreateCommit(_ context.Context, item Commit) (Commit, bool, error) {
	store.createdCommit = item
	store.commit = item
	return item, true, nil
}
func (store *articleTestStore) GetCommit(context.Context, string, string) (Commit, error) {
	if store.commit.CommitID == "" {
		return Commit{}, ErrNotFound
	}
	return store.commit, nil
}
func (store *articleTestStore) GetPublication(_ context.Context, _ string, id string) (Publication, error) {
	for _, publication := range store.publications {
		if publication.PublicationID == id || publication.IdempotencyKey == id {
			return publication, nil
		}
	}
	return Publication{}, ErrNotFound
}
func (store *articleTestStore) GetTemplate(context.Context, string, string) (Template, error) {
	if store.templateErr != nil {
		return Template{}, store.templateErr
	}
	return store.template, nil
}
func (store *articleTestStore) GetBuild(_ context.Context, _ string, buildID string) (Build, error) {
	for _, build := range store.builds {
		if build.BuildID == buildID {
			return build, nil
		}
	}
	return Build{}, ErrNotFound
}
func (store *articleTestStore) CreateBuild(_ context.Context, item Build, _ jobs.CreateInput, _ jobs.TransactionalWriter) (Build, bool, error) {
	if item.BuildKind == BuildPreview {
		for index := range store.builds {
			if store.builds[index].BuildKind == BuildPreview && (store.builds[index].Status == BuildQueued || store.builds[index].Status == BuildRunning) {
				store.builds[index].Status = BuildSuperseded
			}
		}
	}
	store.builds = append(store.builds, item)
	return item, true, nil
}
func (store *articleTestStore) AddBuildOutput(_ context.Context, _ string, item BuildOutput) error {
	store.outputs = append(store.outputs, item)
	return nil
}
func (store *articleTestStore) CreatePublicationBuild(_ context.Context, publication Publication, build Build, _ jobs.CreateInput, _ jobs.TransactionalWriter) (Publication, bool, error) {
	store.publications = append(store.publications, publication)
	store.builds = append(store.builds, build)
	return publication, true, nil
}
func (store *articleTestStore) RetryPublicationBuild(_ context.Context, publication Publication, _ string, build Build, _ jobs.CreateInput, _ jobs.TransactionalWriter) (Publication, error) {
	publication.Status = "building"
	publication.BuildID = build.BuildID
	for index := range store.publications {
		if store.publications[index].PublicationID == publication.PublicationID {
			store.publications[index] = publication
		}
	}
	store.builds = append(store.builds, build)
	return publication, nil
}
func (store *articleTestStore) CreateRelease(_ context.Context, item Release) (Release, bool, error) {
	store.releases = append(store.releases, item)
	return item, true, nil
}
func (store *articleTestStore) AcceptPatch(_ context.Context, _ string, _ string, _ string, input PersistDraftInput, _ string, _ []Block, _ map[string]interface{}, _ string) (Patch, error) {
	store.accepted = true
	store.persisted = input
	revision := input.ExpectedRevision + 1
	return Patch{PatchID: "patch-1", Status: "accepted", AcceptedRevision: &revision}, nil
}

type articleTestWorkspace struct {
	commits []repo.WorkspaceCommitRequest
	files   map[string]string
}

func (*articleTestWorkspace) ResolveHead(context.Context, string) (repo.Revision, error) {
	return repo.Revision{CommitSHA: strings.Repeat("a", 40), Workspace: repo.WorkspaceArticle}, nil
}
func (*articleTestWorkspace) ListTree(context.Context, string, string, string) ([]repo.TreeEntry, error) {
	return nil, nil
}
func (workspace *articleTestWorkspace) ReadFile(_ context.Context, _, _, path string) (repo.FileContent, error) {
	value, exists := workspace.files[path]
	if !exists {
		return repo.FileContent{}, errors.New("file not found")
	}
	return repo.FileContent{Content: &value}, nil
}

type articleTestJobAccess struct{ job jobs.Job }

func (access articleTestJobAccess) ClaimedWorkerJob(context.Context, auth.Identity, string) (jobs.Job, error) {
	return access.job, nil
}

type articleTestArtifacts struct {
	archived      []BuildOutput
	resourceCalls [][2]string
}

func (*articleTestArtifacts) ArticleTemplateGrant(context.Context, string, string, string) (map[string]interface{}, error) {
	return map[string]interface{}{"method": "GET", "url": "https://grant.test/template"}, nil
}
func (artifacts *articleTestArtifacts) ArticleResourceGrant(_ context.Context, _, artifactID, versionID string) (map[string]interface{}, error) {
	artifacts.resourceCalls = append(artifacts.resourceCalls, [2]string{artifactID, versionID})
	return map[string]interface{}{"method": "GET", "url": "https://grant.test/resource", "headers": map[string]string{"x-job": "scoped"}, "expires_at": "2026-08-13T01:00:00Z", "filename": "figure.png", "mime_type": "image/png", "size_bytes": int64(12), "sha256": strings.Repeat("a", 64)}, nil
}
func (artifacts *articleTestArtifacts) ArchiveArticleBuildOutput(_ context.Context, _, buildID, _, role, filename, mimeType, expectedSHA string, expectedSize int64, input io.Reader) (string, string, error) {
	contents, err := io.ReadAll(input)
	if err != nil {
		return "", "", err
	}
	if int64(len(contents)) != expectedSize || hashBytes(contents) != expectedSHA {
		return "", "", errors.New("output integrity mismatch")
	}
	artifacts.archived = append(artifacts.archived, BuildOutput{Role: role, Filename: filename, MIMEType: mimeType, SHA256: expectedSHA, SizeBytes: expectedSize})
	return "artifact-" + buildID + "-" + role, "version-" + role, nil
}
func (workspace *articleTestWorkspace) Commit(_ context.Context, input repo.WorkspaceCommitRequest) (repo.CommitResult, error) {
	workspace.commits = append(workspace.commits, input)
	return repo.CommitResult{CommitSHA: strings.Repeat("b", 40), PreviousCommitSHA: input.ExpectedHeadSHA, Workspace: repo.WorkspaceArticle}, nil
}

func TestCommitPinsOneDraftRevisionAndOnlyThreeEditableFiles(t *testing.T) {
	store := &articleTestStore{draft: draftAt(4), references: []Reference{{CitationKey: "ref", ReferenceType: "model_snapshot", SourceObjectID: "model", SourceVersionID: "v3", Title: "Model"}}}
	workspace := &articleTestWorkspace{}
	service := testService(store, workspace)

	commit, err := service.Commit(context.Background(), human(), "project-1", 4, "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.commits) != 1 || len(workspace.commits[0].Changes) != 3 {
		t.Fatalf("Repo ArticleWorkspace did not receive exactly three files: %#v", workspace.commits)
	}
	wantPaths := []string{"manuscript.md", "references.bib", ".mmdash/article.json"}
	for index, change := range workspace.commits[0].Changes {
		if change.Path != wantPaths[index] || change.Operation != "upsert" {
			t.Fatalf("unexpected change: %#v", change)
		}
	}
	if commit.DraftRevision != 4 || commit.TiptapJSON["type"] != "doc" || commit.YjsUpdate != "AQID" || len(commit.FrozenReferences) != 1 {
		t.Fatalf("commit snapshot is incomplete: %#v", commit)
	}
	store.draft.TiptapJSON["type"] = "changed-after-commit"
	if store.createdCommit.TiptapJSON["type"] == "changed-after-commit" {
		t.Fatal("commit snapshot aliased the mutable draft")
	}
	if _, err := service.Commit(context.Background(), human(), "project-1", 3, "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision was accepted: %v", err)
	}
}

func TestRestoreCommitCreatesNewDraftFromFrozenYjsSnapshot(t *testing.T) {
	snapshot := draftAt(2)
	store := &articleTestStore{draft: draftAt(9), commit: Commit{CommitID: "commit-1", CommitSHA: strings.Repeat("c", 40), StateVector: snapshot.StateVector, TiptapJSON: snapshot.TiptapJSON, YjsUpdate: snapshot.YjsUpdate}}
	service := testService(store, &articleTestWorkspace{})

	restored, err := service.RestoreCommit(context.Background(), human(), "project-1", "commit-1")
	if err != nil {
		t.Fatal(err)
	}
	if restored.DraftRevision != 10 || store.persisted.ActorKind != "restore" || store.persisted.YjsUpdate != snapshot.YjsUpdate || store.persisted.Provenance["commit_id"] != "commit-1" {
		t.Fatalf("restore did not create a provenance-bearing new draft: %#v %#v", restored, store.persisted)
	}
}

func TestAcceptedPatchPersistsDraftAndReviewAtomicallyThroughStore(t *testing.T) {
	store := &articleTestStore{draft: draftAt(5)}
	service := testService(store, &articleTestWorkspace{})
	input := PersistDraftInput{ExpectedRevision: 5, StateVector: "BAUG", YjsUpdate: "AQID", TiptapJSON: draftAt(5).TiptapJSON, Provenance: map[string]interface{}{}}

	patch, err := service.ReviewPatch(context.Background(), human(), "project-1", "patch-1", "accepted", &input)
	if err != nil {
		t.Fatal(err)
	}
	if !store.accepted || patch.Status != "accepted" || store.persisted.ActorKind != "ai" || store.persisted.Provenance["patch_id"] != "patch-1" || store.persisted.Provenance["reviewed_by"] != "user-1" {
		t.Fatalf("patch acceptance lost atomicity/provenance: %#v %#v", patch, store.persisted)
	}
}

func TestDraftFlushAndManualPreviewNeverCreateACommit(t *testing.T) {
	store := &articleTestStore{draft: draftAt(2), template: Template{TemplateID: "template-1", VersionID: "version-1", ArtifactID: "artifact-1", Status: "ready"}}
	workspace := &articleTestWorkspace{}
	service := testService(store, workspace)

	flushed, err := service.PersistDraft(context.Background(), human(), "project-1", PersistDraftInput{ActorKind: "human", ExpectedRevision: 2, Provenance: map[string]interface{}{}, StateVector: "Bw==", YjsUpdate: "CA==", TiptapJSON: draftAt(2).TiptapJSON})
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := service.CreatePreview(context.Background(), human(), "project-1", flushed.DraftRevision, "template-1", "auto", "auto")
	if err != nil {
		t.Fatal(err)
	}
	store.draft = draftAt(flushed.DraftRevision + 1)
	second, _, err := service.CreatePreview(context.Background(), human(), "project-1", store.draft.DraftRevision, "template-1", "pdflatex", "bibtex")
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.commits) != 0 || first.CommitID != "" || second.CommitID != "" {
		t.Fatalf("draft/preview unexpectedly wrote Git: %#v %#v", workspace.commits, store.builds)
	}
	if store.builds[0].Status != BuildSuperseded || store.builds[1].Status != BuildQueued {
		t.Fatalf("preview latest-only state was not enforced: %#v", store.builds)
	}
	if _, _, err = service.CreatePreview(context.Background(), human(), "project-1", flushed.DraftRevision, "template-1", "auto", "auto"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale preview revision was accepted: %v", err)
	}
}

func TestSameCommitCanHaveMultipleBuildsAndFailedPublicationKeepsCommit(t *testing.T) {
	store := &articleTestStore{draft: draftAt(1), template: Template{TemplateID: "template-1", VersionID: "version-1", ArtifactID: "artifact-1", Status: "ready"}}
	workspace := &articleTestWorkspace{}
	service := testService(store, workspace)
	store.commit = Commit{CommitID: "commit-1", ProjectID: "project-1"}

	first, _, err := service.CreateBuild(context.Background(), human(), "project-1", "commit-1", "template-1", "auto", "auto", "build-1")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := service.CreateBuild(context.Background(), human(), "project-1", "commit-1", "template-1", "xelatex", "biber", "build-2")
	if err != nil {
		t.Fatal(err)
	}
	if first.BuildID == second.BuildID || len(store.builds) != 2 || store.builds[0].CommitID != store.builds[1].CommitID {
		t.Fatalf("multiple builds were not preserved: %#v", store.builds)
	}

	store.templateErr = ErrNotReady
	store.draft = draftAt(1)
	if _, _, err = service.Publish(context.Background(), human(), "project-1", PublicationInput{DraftRevision: 1, Message: "publish", TemplateID: "template-1", Engine: "auto", BibliographyTool: "auto", Tag: "v1", Title: "Paper", IdempotencyKey: "publication-1"}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("expected build preparation failure, got %v", err)
	}
	if store.createdCommit.CommitID == "" || len(workspace.commits) == 0 {
		t.Fatal("publication build failure discarded the successful Commit")
	}
}

func TestPublicationSuccessShapeAndRetryReuseTheFrozenCommit(t *testing.T) {
	store := &articleTestStore{draft: draftAt(4), template: Template{TemplateID: "template-1", VersionID: "version-1", ArtifactID: "artifact-1", Status: "ready"}}
	workspace := &articleTestWorkspace{}
	service := testService(store, workspace)
	input := PublicationInput{DraftRevision: 4, Message: "release checkpoint", TemplateID: "template-1", Engine: "pdflatex", BibliographyTool: "bibtex", Tag: "v1.0.0", Title: "Paper", Notes: "accepted", IdempotencyKey: "publication-1"}

	publication, created, err := service.Publish(context.Background(), human(), "project-1", input)
	if err != nil {
		t.Fatal(err)
	}
	if !created || publication.Status != "building" || len(workspace.commits) != 1 || len(store.builds) != 1 || store.builds[0].BuildKind != BuildFormal {
		t.Fatalf("publication did not atomically create Commit and Build intent: %#v %#v", publication, store.builds)
	}
	originalCommitID, originalBuildID := publication.CommitID, publication.BuildID
	store.publications[0].Status = "failed"
	store.builds[0].Status = BuildFailed
	retried, err := service.RetryPublication(context.Background(), human(), "project-1", publication.PublicationID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != "building" || retried.CommitID != originalCommitID || retried.BuildID == originalBuildID || len(workspace.commits) != 1 {
		t.Fatalf("retry did not reuse exactly one frozen Commit: %#v", retried)
	}

	outputs := requiredBuildOutputs()
	store.commit = Commit{CommitID: originalCommitID, ProjectID: "project-1", CommitSHA: strings.Repeat("d", 40)}
	store.builds = append(store.builds, Build{BuildID: "successful-build", ProjectID: "project-1", BuildKind: BuildFormal, Status: BuildSucceeded, CommitID: originalCommitID, TemplateVersionID: "version-1", Engine: "pdflatex", Toolchain: map[string]interface{}{"texlive": "2022"}, Outputs: outputs})
	release, released, err := service.CreateRelease(context.Background(), human(), "project-1", originalCommitID, "successful-build", "v1.0.0", "Paper", "accepted")
	if err != nil {
		t.Fatal(err)
	}
	if !released || release.CommitID != originalCommitID || release.BuildID != "successful-build" || len(release.Outputs) != len(outputs) {
		t.Fatalf("successful Build did not freeze one complete Release: %#v", release)
	}
}

func TestArticlePermissionsAndWorkerOutputBoundary(t *testing.T) {
	store := &articleTestStore{draft: draftAt(1), template: Template{TemplateID: "template-1", Status: "ready"}}
	service := testService(store, &articleTestWorkspace{})
	service.Access = articleTestAccess{denied: project.PermissionArticleBuild}
	if _, _, err := service.CreatePreview(context.Background(), human(), "project-1", 1, "template-1", "auto", "auto"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Article build permission was not enforced: %v", err)
	}

	contents := []byte("%PDF-1.7\n")
	digest := hashBytes(contents)
	store.builds = []Build{{BuildID: "build-1", ProjectID: "project-1", BuildKind: BuildFormal, Status: BuildRunning, JobID: "job-1", TemplateID: "template-1", CreatedBy: "user-1"}}
	artifacts := &articleTestArtifacts{}
	service.Artifacts = artifacts
	service.JobAccess = articleTestJobAccess{job: jobs.Job{ID: "job-1", JobType: JobTypeBuild, ProjectID: "project-1", Payload: map[string]interface{}{"build_id": "build-1"}}}
	output, err := service.WorkerOutput(context.Background(), human(), "job-1", "pdf", "paper.pdf", "application/pdf", digest, int64(len(contents)), bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	if output.ArtifactID == "" || output.VersionID == "" || len(artifacts.archived) != 1 || len(store.outputs) != 1 {
		t.Fatalf("Worker output bypassed immutable Artifact registration: %#v %#v", output, artifacts.archived)
	}
	if _, err = service.WorkerOutput(context.Background(), human(), "job-1", "pdf", "../paper.pdf", "application/pdf", digest, int64(len(contents)), bytes.NewReader(contents)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsafe Worker output filename was accepted: %v", err)
	}
}

func TestFormalWorkerInputUsesCommitFrozenArtifactVersion(t *testing.T) {
	store := &articleTestStore{
		builds: []Build{{BuildID: "build-1", ProjectID: "project-1", BuildKind: BuildFormal, CommitID: "commit-1", JobID: "job-1", TemplateID: "template-1", Engine: "auto", BibliographyTool: "auto"}},
		commit: Commit{CommitID: "commit-1", CommitSHA: strings.Repeat("c", 40), FrozenReferences: []Reference{
			{ReferenceType: "artifact", SourceObjectID: "artifact-fixed", SourceVersionID: "version-fixed", Title: "Figure"},
			{ReferenceType: "artifact", SourceObjectID: "artifact-fixed", SourceVersionID: "version-fixed", Title: "Duplicate"},
			{ReferenceType: "model_snapshot", SourceObjectID: "model-1", SourceVersionID: "snapshot-1"},
		}},
		template: Template{TemplateID: "template-1", ArtifactID: "template-artifact", VersionID: "template-version", Manifest: TemplateManifest{Name: "Template"}},
	}
	workspace := &articleTestWorkspace{files: map[string]string{
		"manuscript.md":  "![Figure](mmdash://artifact/artifact-fixed/versions/version-fixed)\n",
		"references.bib": "", ".mmdash/article.json": `{"schema_version":"1.0"}`,
	}}
	artifacts := &articleTestArtifacts{}
	service := testService(store, workspace)
	service.Artifacts = artifacts
	service.JobAccess = articleTestJobAccess{job: jobs.Job{ID: "job-1", JobType: JobTypeBuild, ProjectID: "project-1", Payload: map[string]interface{}{"build_id": "build-1"}}}

	input, err := service.WorkerInput(context.Background(), human(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Resources) != 1 || len(artifacts.resourceCalls) != 1 || artifacts.resourceCalls[0] != [2]string{"artifact-fixed", "version-fixed"} {
		t.Fatalf("worker input did not preserve one immutable Artifact version: %#v %#v", input.Resources, artifacts.resourceCalls)
	}
	resource := input.Resources[0]
	if resource["sha256"] != strings.Repeat("a", 64) || resource["filename"] != "figure.png" {
		t.Fatalf("worker input lost fixed Artifact integrity metadata: %#v", resource)
	}
}

func testService(store *articleTestStore, workspace *articleTestWorkspace) *Service {
	seed := make([]byte, 16*32)
	for index := range seed {
		seed[index] = byte(index)
	}
	return &Service{
		Access: articleTestAccess{}, Clock: clock.Fixed{Time: time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)},
		Generator: identity.Generator{Reader: bytes.NewReader(seed)}, Store: store, Workspace: workspace,
	}
}

func human() auth.Identity {
	return auth.Identity{Kind: "session", ProjectID: "project-1", User: auth.User{ID: "user-1", Email: "writer@example.com", DisplayName: "Writer"}}
}

func draftAt(revision int64) Draft {
	return Draft{ProjectID: "project-1", DraftRevision: revision, StateVector: "BAUG", YjsUpdate: "AQID", Markdown: "# Paper\n", TiptapJSON: map[string]interface{}{"type": "doc", "content": []interface{}{map[string]interface{}{"type": "heading", "attrs": map[string]interface{}{"id": "block-1", "level": float64(1)}, "content": []interface{}{map[string]interface{}{"type": "text", "text": "Paper"}}}}}}
}

func requiredBuildOutputs() []BuildOutput {
	roles := []string{"pdf", "tex_source", "source_zip", "build_report", "log", "synctex"}
	outputs := make([]BuildOutput, 0, len(roles))
	for _, role := range roles {
		outputs = append(outputs, BuildOutput{Role: role, ArtifactID: "artifact-" + role, VersionID: "version-" + role, Filename: role, SHA256: strings.Repeat("a", 64), SizeBytes: 1})
	}
	return outputs
}
