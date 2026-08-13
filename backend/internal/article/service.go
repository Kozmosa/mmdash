package article

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/repo"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

const (
	JobTypeBuild      = "article.build"
	SettingTypeZotero = "article.zotero"
	maxOutputBytes    = 512 * 1024 * 1024
)

var (
	shaPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	rolePattern = regexp.MustCompile(`^(pdf|tex_source|source_zip|build_report|log|synctex)$`)
	tagPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
)

type Access interface {
	Authenticate(context.Context, string) (auth.Identity, error)
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

type JobAccess interface {
	ClaimedWorkerJob(context.Context, auth.Identity, string) (jobs.Job, error)
}
type SettingsAccess interface {
	Update(context.Context, auth.Identity, settings.Scope, string, string, map[string]interface{}) (settings.Setting, error)
	Delete(context.Context, auth.Identity, settings.Scope, string, string) error
	Resolve(context.Context, settings.Scope, string, string) (settings.ResolvedSetting, error)
}

type Service struct {
	Access     Access
	Artifacts  ArtifactAccess
	Clock      clock.Clock
	Generator  identity.Generator
	HTTPClient *http.Client
	JobAccess  JobAccess
	JobWriter  jobs.TransactionalWriter
	Settings   SettingsAccess
	Store      Store
	Workspace  repo.ArticleWorkspace
}

func (service *Service) Authenticate(ctx context.Context, authorization string) (auth.Identity, error) {
	return service.Access.Authenticate(ctx, authorization)
}

func (service *Service) Draft(ctx context.Context, caller auth.Identity, projectID string) (Draft, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return Draft{}, err
	}
	return service.Store.GetDraft(ctx, projectID)
}
func (service *Service) Aggregate(ctx context.Context, caller auth.Identity, projectID string) (Aggregate, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return Aggregate{}, err
	}
	draft, err := service.Store.GetDraft(ctx, projectID)
	if err != nil {
		return Aggregate{}, err
	}
	references, err := service.Store.ListReferences(ctx, projectID)
	if err != nil {
		return Aggregate{}, err
	}
	commits, err := service.Store.ListCommits(ctx, projectID)
	if err != nil {
		return Aggregate{}, err
	}
	builds, err := service.Store.ListBuilds(ctx, projectID, "")
	if err != nil {
		return Aggregate{}, err
	}
	releases, err := service.Store.ListReleases(ctx, projectID)
	if err != nil {
		return Aggregate{}, err
	}
	templates, err := service.Store.ListTemplates(ctx, projectID)
	if err != nil {
		return Aggregate{}, err
	}
	unreviewed := 0
	completed := 0
	for _, block := range draft.Blocks {
		if block.Tag != "reviewed" {
			unreviewed++
		}
		if strings.TrimSpace(block.Text) != "" {
			completed++
		}
	}
	completion := 0.0
	if len(draft.Blocks) > 0 {
		completion = float64(completed) / float64(len(draft.Blocks))
	}
	return Aggregate{Draft: draft, References: references, Commits: commits, Builds: builds, Releases: releases, Templates: templates, UnreviewedBlocks: unreviewed, SectionCompletion: completion}, nil
}

func (service *Service) ArticleHomeItems(ctx context.Context, caller auth.Identity, projectID string) ([]interface{}, error) {
	aggregate, err := service.Aggregate(ctx, caller, projectID)
	if err != nil {
		return nil, err
	}
	items := []interface{}{map[string]interface{}{"draft_revision": aggregate.Draft.DraftRevision, "unreviewed_blocks": aggregate.UnreviewedBlocks, "section_completion": aggregate.SectionCompletion}}
	if len(aggregate.Builds) > 0 {
		items = append(items, aggregate.Builds[0])
	}
	if len(aggregate.Releases) > 0 {
		items = append(items, aggregate.Releases[0])
	}
	return items, nil
}

// ArticleProjectionBlocks exposes bounded, secret-free block metadata to the
// Data Hub projector. It is Core-internal and is invoked only for a durable
// article.draft.flushed event.
func (service *Service) ArticleProjectionBlocks(ctx context.Context, projectID string) ([]map[string]interface{}, error) {
	draft, err := service.Store.GetDraft(ctx, projectID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(draft.Blocks))
	for _, block := range draft.Blocks {
		items = append(items, map[string]interface{}{
			"block_id": block.BlockID, "draft_revision": draft.DraftRevision,
			"node_type": block.NodeType, "ordinal": block.Ordinal, "text": block.Text,
			"tag": block.Tag, "provenance": block.Provenance, "updated_at": block.UpdatedAt,
		})
	}
	return items, nil
}

func (service *Service) ArticleBlock(ctx context.Context, caller auth.Identity, projectID, blockID string) (Block, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return Block{}, err
	}
	draft, err := service.Store.GetDraft(ctx, projectID)
	if err != nil {
		return Block{}, err
	}
	for _, block := range draft.Blocks {
		if block.BlockID == blockID {
			return block, nil
		}
	}
	return Block{}, ErrNotFound
}

func (service *Service) PersistDraft(ctx context.Context, caller auth.Identity, projectID string, input PersistDraftInput) (Draft, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleEdit); err != nil {
		return Draft{}, err
	}
	return service.persistDraft(ctx, caller.ActorID(), projectID, input)
}
func (service *Service) persistDraft(ctx context.Context, actorID, projectID string, input PersistDraftInput) (Draft, error) {
	markdown, blocks, manifest, referencesBIB, err := service.prepareProjectDraft(ctx, projectID, input)
	if err != nil {
		return Draft{}, err
	}
	return service.Store.PersistDraft(ctx, projectID, actorID, input, markdown, blocks, manifest, referencesBIB)
}

func (service *Service) prepareDraft(ctx context.Context, input PersistDraftInput) (string, []Block, map[string]interface{}, string, error) {
	if input.Provenance == nil || strings.TrimSpace(input.YjsUpdate) == "" || strings.TrimSpace(input.StateVector) == "" || input.TiptapJSON == nil {
		return "", nil, nil, "", ErrInvalid
	}
	now := service.now()
	markdown, blocks, err := NormalizeDocument(input.TiptapJSON, service.Generator, input.ActorKind, input.Provenance, now)
	if err != nil {
		return "", nil, nil, "", err
	}
	return markdown, blocks, nil, "", nil
}

func (service *Service) prepareProjectDraft(ctx context.Context, projectID string, input PersistDraftInput) (string, []Block, map[string]interface{}, string, error) {
	markdown, blocks, _, _, err := service.prepareDraft(ctx, input)
	if err != nil {
		return "", nil, nil, "", err
	}
	references, err := service.Store.ListReferences(ctx, projectID)
	if err != nil {
		return "", nil, nil, "", err
	}
	manifest := map[string]interface{}{"schema_version": "1.0", "draft_revision": input.ExpectedRevision + 1, "state_vector": input.StateVector, "format": "markdown", "editable_files": []string{"manuscript.md", "references.bib", ".mmdash/article.json"}}
	return markdown, blocks, manifest, Bibliography(references), nil
}

func (service *Service) ListPatches(ctx context.Context, caller auth.Identity, projectID, status string) ([]Patch, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return nil, err
	}
	return service.Store.ListPatches(ctx, projectID, status)
}
func (service *Service) CreatePatch(ctx context.Context, caller auth.Identity, projectID string, base int64, patch map[string]interface{}, rationale string, provenance map[string]interface{}) (Patch, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticlePropose); err != nil {
		return Patch{}, err
	}
	if patch == nil || provenance == nil || strings.TrimSpace(rationale) == "" {
		return Patch{}, ErrInvalid
	}
	id, err := service.Generator.New()
	if err != nil {
		return Patch{}, err
	}
	now := service.now()
	return service.Store.CreatePatch(ctx, Patch{PatchID: id, ProjectID: projectID, BaseRevision: base, Status: "proposed", Patch: patch, Rationale: rationale, Provenance: provenance, CreatedBy: caller.ActorID(), CreatedAt: now, UpdatedAt: now})
}
func (service *Service) ReviewPatch(ctx context.Context, caller auth.Identity, projectID, patchID, decision string, acceptedDraft *PersistDraftInput) (Patch, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleEdit); err != nil {
		return Patch{}, err
	}
	if decision != "accepted" && decision != "rejected" {
		return Patch{}, ErrInvalid
	}
	var accepted *int64
	if decision == "accepted" {
		if acceptedDraft == nil {
			return Patch{}, ErrInvalid
		}
		acceptedDraft.ActorKind = "ai"
		if acceptedDraft.Provenance == nil {
			acceptedDraft.Provenance = map[string]interface{}{}
		}
		acceptedDraft.Provenance["patch_id"] = patchID
		acceptedDraft.Provenance["reviewed_by"] = caller.ActorID()
		markdown, blocks, manifest, referencesBIB, err := service.prepareProjectDraft(ctx, projectID, *acceptedDraft)
		if err != nil {
			return Patch{}, err
		}
		return service.Store.AcceptPatch(ctx, projectID, patchID, caller.ActorID(), *acceptedDraft, markdown, blocks, manifest, referencesBIB)
	}
	return service.Store.ReviewPatch(ctx, projectID, patchID, decision, caller.ActorID(), accepted)
}

func (service *Service) ListReferences(ctx context.Context, caller auth.Identity, projectID string) ([]Reference, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return nil, err
	}
	return service.Store.ListReferences(ctx, projectID)
}
func (service *Service) CreateReference(ctx context.Context, caller auth.Identity, projectID string, item Reference) (Reference, bool, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleEdit); err != nil {
		return Reference{}, false, err
	}
	if item.Metadata == nil {
		item.Metadata = map[string]interface{}{}
	}
	if item.ReferenceType == "" || item.SourceObjectID == "" || item.SourceVersionID == "" || item.Title == "" {
		return Reference{}, false, ErrInvalid
	}
	id, err := service.Generator.New()
	if err != nil {
		return Reference{}, false, err
	}
	item.ReferenceID = id
	item.ProjectID = projectID
	item.CreatedBy = caller.ActorID()
	item.CreatedAt = service.now()
	return service.Store.CreateReference(ctx, item)
}
func (service *Service) DeleteReference(ctx context.Context, caller auth.Identity, projectID, id string) error {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleEdit); err != nil {
		return err
	}
	return service.Store.DeleteReference(ctx, projectID, id, caller.ActorID())
}

func (service *Service) ListCommits(ctx context.Context, caller auth.Identity, projectID string) ([]Commit, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return nil, err
	}
	return service.Store.ListCommits(ctx, projectID)
}
func (service *Service) Commit(ctx context.Context, caller auth.Identity, projectID string, draftRevision int64, message string) (Commit, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleEdit); err != nil {
		return Commit{}, err
	}
	draft, err := service.Store.GetDraft(ctx, projectID)
	if err != nil {
		return Commit{}, err
	}
	if draft.DraftRevision != draftRevision || draftRevision < 1 {
		return Commit{}, ErrConflict
	}
	references, err := service.Store.ListReferences(ctx, projectID)
	if err != nil {
		return Commit{}, err
	}
	referencesBIB := Bibliography(references)
	manifest := map[string]interface{}{"schema_version": "1.0", "project_id": projectID, "draft_revision": draft.DraftRevision, "state_vector": draft.StateVector, "frozen_references": references, "editable_files": []string{"manuscript.md", "references.bib", ".mmdash/article.json"}}
	manifestBytes, err := StableJSON(manifest)
	if err != nil {
		return Commit{}, err
	}
	manifestBytes = append(manifestBytes, '\n')
	manuscript := []byte(draft.Markdown)
	bibliography := []byte(referencesBIB)
	head, err := service.Workspace.ResolveHead(ctx, projectID)
	if err != nil {
		return Commit{}, err
	}
	requestDigest := hashBytes(append(append(append([]byte{}, manuscript...), bibliography...), manifestBytes...))
	commitID, err := service.Generator.New()
	if err != nil {
		return Commit{}, err
	}
	result, err := service.Workspace.Commit(ctx, repo.WorkspaceCommitRequest{ActorEmail: caller.User.Email, ActorID: caller.ActorID(), ActorName: displayName(caller), Changes: []repo.FileChange{{Path: "manuscript.md", Operation: "upsert", Content: manuscript}, {Path: "references.bib", Operation: "upsert", Content: bibliography}, {Path: ".mmdash/article.json", Operation: "upsert", Content: manifestBytes}}, ExpectedHeadSHA: head.CommitSHA, IdempotencyKey: "article-commit:" + projectID + ":" + fmt.Sprint(draftRevision) + ":" + requestDigest, Message: strings.TrimSpace(message), ProjectID: projectID, RequestSHA256: requestDigest})
	if err != nil {
		return Commit{}, err
	}
	tiptapSnapshot, err := cloneDocument(draft.TiptapJSON)
	if err != nil {
		return Commit{}, err
	}
	item := Commit{CommitID: commitID, ProjectID: projectID, DraftRevision: draftRevision, StateVector: draft.StateVector, TiptapJSON: tiptapSnapshot, YjsUpdate: draft.YjsUpdate, CommitSHA: result.CommitSHA, PreviousCommitSHA: result.PreviousCommitSHA, Message: message, ManuscriptSHA256: hashBytes(manuscript), ReferencesSHA256: hashBytes(bibliography), ManifestSHA256: hashBytes(manifestBytes), FrozenReferences: append([]Reference(nil), references...), CreatedBy: caller.ActorID(), CreatedAt: service.now()}
	created, _, err := service.Store.CreateCommit(ctx, item)
	return created, err
}
func (service *Service) CommitDetail(ctx context.Context, caller auth.Identity, projectID, id string) (CommitDetail, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return CommitDetail{}, err
	}
	commit, err := service.Store.GetCommit(ctx, projectID, id)
	if err != nil {
		return CommitDetail{}, err
	}
	builds, err := service.Store.ListBuilds(ctx, projectID, id)
	if err != nil {
		return CommitDetail{}, err
	}
	releases, err := service.Store.ListReleases(ctx, projectID)
	if err != nil {
		return CommitDetail{}, err
	}
	filtered := []Release{}
	for _, release := range releases {
		if release.CommitID == id {
			filtered = append(filtered, release)
		}
	}
	return CommitDetail{Commit: commit, Builds: builds, Releases: filtered}, nil
}
func (service *Service) RestoreCommit(ctx context.Context, caller auth.Identity, projectID, id string) (Draft, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleEdit); err != nil {
		return Draft{}, err
	}
	commit, err := service.Store.GetCommit(ctx, projectID, id)
	if err != nil {
		return Draft{}, err
	}
	if commit.TiptapJSON == nil || commit.YjsUpdate == "" || commit.StateVector == "" {
		return Draft{}, ErrNotReady
	}
	current, err := service.Store.GetDraft(ctx, projectID)
	if err != nil {
		return Draft{}, err
	}
	return service.persistDraft(ctx, caller.ActorID(), projectID, PersistDraftInput{ActorKind: "restore", ExpectedRevision: current.DraftRevision, Provenance: map[string]interface{}{"commit_id": id, "commit_sha": commit.CommitSHA}, StateVector: commit.StateVector, TiptapJSON: cloneObject(commit.TiptapJSON), YjsUpdate: commit.YjsUpdate})
}

func (service *Service) CreateBuild(ctx context.Context, caller auth.Identity, projectID, commitID, templateID, engine, bibliographyTool, idempotency string) (Build, bool, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleBuild); err != nil {
		return Build{}, false, err
	}
	return service.createBuild(ctx, caller.ActorID(), projectID, BuildFormal, commitID, nil, templateID, engine, bibliographyTool, idempotency)
}
func (service *Service) CreatePreview(ctx context.Context, caller auth.Identity, projectID string, draftRevision int64, templateID, engine, bibliographyTool string) (Build, bool, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleBuild); err != nil {
		return Build{}, false, err
	}
	draft, err := service.Store.GetDraft(ctx, projectID)
	if err != nil {
		return Build{}, false, err
	}
	if draft.DraftRevision != draftRevision {
		return Build{}, false, ErrConflict
	}
	return service.createBuild(ctx, caller.ActorID(), projectID, BuildPreview, "", &draftRevision, templateID, engine, bibliographyTool, fmt.Sprintf("preview:%d:%s:%s:%s", draftRevision, templateID, engine, bibliographyTool))
}
func (service *Service) createBuild(ctx context.Context, actorID, projectID, kind, commitID string, draftRevision *int64, templateID, engine, bibliographyTool, idempotency string) (Build, bool, error) {
	item, jobInput, err := service.prepareBuild(ctx, actorID, projectID, kind, commitID, draftRevision, templateID, engine, bibliographyTool, idempotency)
	if err != nil {
		return Build{}, false, err
	}
	return service.Store.CreateBuild(ctx, item, jobInput, service.JobWriter)
}

func (service *Service) prepareBuild(ctx context.Context, actorID, projectID, kind, commitID string, draftRevision *int64, templateID, engine, bibliographyTool, idempotency string) (Build, jobs.CreateInput, error) {
	template, err := service.Store.GetTemplate(ctx, projectID, templateID)
	if err != nil {
		return Build{}, jobs.CreateInput{}, err
	}
	if kind != BuildTemplateTest && template.Status != "ready" {
		return Build{}, jobs.CreateInput{}, ErrNotReady
	}
	if kind == BuildFormal {
		if _, err := service.Store.GetCommit(ctx, projectID, commitID); err != nil {
			return Build{}, jobs.CreateInput{}, err
		}
	}
	id, err := service.Generator.New()
	if err != nil {
		return Build{}, jobs.CreateInput{}, err
	}
	now := service.now()
	item := Build{BuildID: id, ProjectID: projectID, BuildKind: kind, Status: BuildQueued, CommitID: commitID, DraftRevision: draftRevision, TemplateID: templateID, TemplateArtifactID: template.ArtifactID, TemplateVersionID: template.VersionID, Engine: engine, BibliographyTool: bibliographyTool, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now, Outputs: []BuildOutput{}, Toolchain: map[string]interface{}{}, IdempotencyKey: idempotency}
	jobInput := jobs.CreateInput{JobType: JobTypeBuild, ProjectID: projectID, Payload: map[string]interface{}{"build_id": id, "build_kind": kind}, Priority: kindPriority(kind), IdempotencyKey: "article-build:" + id, MaxAttempts: 2, TimeoutSeconds: 900}
	return item, jobInput, nil
}
func (service *Service) GetBuild(ctx context.Context, caller auth.Identity, projectID, id string) (Build, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return Build{}, err
	}
	return service.Store.GetBuild(ctx, projectID, id)
}
func (service *Service) ListBuilds(ctx context.Context, caller auth.Identity, projectID string) ([]Build, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return nil, err
	}
	return service.Store.ListBuilds(ctx, projectID, "")
}
func (service *Service) RetryBuild(ctx context.Context, caller auth.Identity, projectID, id string) (Build, bool, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleBuild); err != nil {
		return Build{}, false, err
	}
	previous, err := service.Store.GetBuild(ctx, projectID, id)
	if err != nil {
		return Build{}, false, err
	}
	if previous.Status != BuildFailed && previous.Status != BuildSuperseded {
		return Build{}, false, ErrConflict
	}
	return service.createBuild(ctx, caller.ActorID(), projectID, previous.BuildKind, previous.CommitID, previous.DraftRevision, previous.TemplateID, previous.Engine, previous.BibliographyTool, "retry:"+id+":"+fmt.Sprint(service.now().UnixNano()))
}

func (service *Service) RegisterTemplate(ctx context.Context, caller auth.Identity, projectID, artifactID, versionID string, raw map[string]interface{}) (Template, bool, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleTemplate); err != nil {
		return Template{}, false, err
	}
	manifest, err := decodeManifest(raw)
	if err != nil {
		return Template{}, false, err
	}
	if _, err = service.Artifacts.ArticleTemplateGrant(ctx, projectID, artifactID, versionID); err != nil {
		return Template{}, false, err
	}
	id, err := service.Generator.New()
	if err != nil {
		return Template{}, false, err
	}
	now := service.now()
	item, created, err := service.Store.CreateTemplate(ctx, Template{TemplateID: id, ProjectID: projectID, ArtifactID: artifactID, VersionID: versionID, Manifest: manifest, Status: "validating", CreatedBy: caller.ActorID(), CreatedAt: now, UpdatedAt: now})
	if err != nil || !created {
		return item, created, err
	}
	revision := int64(0)
	build, _, err := service.createBuild(ctx, caller.ActorID(), projectID, BuildTemplateTest, "", &revision, item.TemplateID, manifest.Engine, manifest.BibliographyTool, "template-test:"+versionID)
	if err != nil {
		return Template{}, false, err
	}
	item.TestBuildID = build.BuildID
	return item, true, nil
}
func (service *Service) ListTemplates(ctx context.Context, caller auth.Identity, projectID string) ([]Template, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return nil, err
	}
	return service.Store.ListTemplates(ctx, projectID)
}

func (service *Service) CreateRelease(ctx context.Context, caller auth.Identity, projectID, commitID, buildID, tag, title, notes string) (Release, bool, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRelease); err != nil {
		return Release{}, false, err
	}
	if !tagPattern.MatchString(tag) {
		return Release{}, false, ErrInvalid
	}
	commit, err := service.Store.GetCommit(ctx, projectID, commitID)
	if err != nil {
		return Release{}, false, err
	}
	build, err := service.Store.GetBuild(ctx, projectID, buildID)
	if err != nil {
		return Release{}, false, err
	}
	if build.Status != BuildSucceeded || build.BuildKind != BuildFormal || build.CommitID != commitID || !hasRequiredOutputs(build.Outputs) {
		return Release{}, false, ErrNotReady
	}
	id, err := service.Generator.New()
	if err != nil {
		return Release{}, false, err
	}
	return service.Store.CreateRelease(ctx, Release{ReleaseID: id, ProjectID: projectID, CommitID: commitID, CommitSHA: commit.CommitSHA, BuildID: buildID, Tag: tag, Title: title, Notes: notes, TemplateVersionID: build.TemplateVersionID, Engine: build.Engine, Toolchain: build.Toolchain, Outputs: build.Outputs, CreatedBy: caller.ActorID(), CreatedAt: service.now()})
}
func (service *Service) ListReleases(ctx context.Context, caller auth.Identity, projectID string) ([]Release, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return nil, err
	}
	return service.Store.ListReleases(ctx, projectID)
}
func (service *Service) GetRelease(ctx context.Context, caller auth.Identity, projectID, id string) (Release, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return Release{}, err
	}
	return service.Store.GetRelease(ctx, projectID, id)
}

type PublicationInput struct {
	DraftRevision                                                                    int64
	Message, TemplateID, Engine, BibliographyTool, Tag, Title, Notes, IdempotencyKey string
}

func (service *Service) Publish(ctx context.Context, caller auth.Identity, projectID string, input PublicationInput) (Publication, bool, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRelease); err != nil {
		return Publication{}, false, err
	}
	if !tagPattern.MatchString(input.Tag) || strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Message) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
		return Publication{}, false, ErrInvalid
	}
	if existing, err := service.Store.GetPublication(ctx, projectID, input.IdempotencyKey); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Publication{}, false, err
	}
	commit, err := service.Commit(ctx, caller, projectID, input.DraftRevision, input.Message)
	if err != nil {
		return Publication{}, false, err
	}
	build, jobInput, err := service.prepareBuild(ctx, caller.ActorID(), projectID, BuildFormal, commit.CommitID, nil, input.TemplateID, input.Engine, input.BibliographyTool, "publication:"+input.IdempotencyKey)
	if err != nil {
		return Publication{}, false, err
	}
	id, err := service.Generator.New()
	if err != nil {
		return Publication{}, false, err
	}
	now := service.now()
	publication := Publication{PublicationID: id, ProjectID: projectID, CommitID: commit.CommitID, BuildID: build.BuildID, Status: "building", Tag: input.Tag, Title: input.Title, Notes: input.Notes, CreatedBy: caller.ActorID(), CreatedAt: now, UpdatedAt: now, IdempotencyKey: input.IdempotencyKey}
	return service.Store.CreatePublicationBuild(ctx, publication, build, jobInput, service.JobWriter)
}

func (service *Service) RetryPublication(ctx context.Context, caller auth.Identity, projectID, publicationID string) (Publication, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRelease); err != nil {
		return Publication{}, err
	}
	publication, err := service.Store.GetPublication(ctx, projectID, publicationID)
	if err != nil {
		return Publication{}, err
	}
	if publication.Status != "failed" {
		return Publication{}, ErrConflict
	}
	previous, err := service.Store.GetBuild(ctx, projectID, publication.BuildID)
	if err != nil {
		return Publication{}, err
	}
	if previous.BuildKind != BuildFormal || previous.CommitID != publication.CommitID {
		return Publication{}, ErrConflict
	}
	build, jobInput, err := service.prepareBuild(ctx, caller.ActorID(), projectID, BuildFormal, publication.CommitID, nil, previous.TemplateID, previous.Engine, previous.BibliographyTool, "publication-retry:"+publication.PublicationID+":"+previous.BuildID)
	if err != nil {
		return Publication{}, err
	}
	publication.UpdatedAt = service.now()
	return service.Store.RetryPublicationBuild(ctx, publication, previous.BuildID, build, jobInput, service.JobWriter)
}

func (service *Service) WorkerInput(ctx context.Context, caller auth.Identity, jobID string) (BuildJobInput, error) {
	job, build, template, err := service.workerBuild(ctx, caller, jobID)
	if err != nil {
		return BuildJobInput{}, err
	}
	if build.Status == BuildSuperseded {
		return BuildJobInput{}, ErrSuperseded
	}
	var manuscript, referencesBIB string
	var frozenReferences []Reference
	manifest := map[string]interface{}{}
	switch build.BuildKind {
	case BuildFormal:
		commit, err := service.Store.GetCommit(ctx, job.ProjectID, build.CommitID)
		if err != nil {
			return BuildJobInput{}, err
		}
		frozenReferences = commit.FrozenReferences
		for _, filename := range []string{"manuscript.md", "references.bib", ".mmdash/article.json"} {
			file, err := service.Workspace.ReadFile(ctx, job.ProjectID, commit.CommitSHA, filename)
			if err != nil || file.Content == nil {
				return BuildJobInput{}, ErrNotReady
			}
			switch filename {
			case "manuscript.md":
				manuscript = *file.Content
			case "references.bib":
				referencesBIB = *file.Content
			default:
				if json.Unmarshal([]byte(*file.Content), &manifest) != nil {
					return BuildJobInput{}, ErrInvalid
				}
			}
		}
	case BuildPreview:
		draft, err := service.Store.GetDraft(ctx, job.ProjectID)
		if err != nil {
			return BuildJobInput{}, err
		}
		if build.DraftRevision == nil || draft.DraftRevision != *build.DraftRevision {
			return BuildJobInput{}, ErrSuperseded
		}
		manuscript, referencesBIB, manifest = draft.Markdown, draft.ReferencesBIB, draft.Manifest
		frozenReferences, err = service.Store.ListReferences(ctx, job.ProjectID)
		if err != nil {
			return BuildJobInput{}, err
		}
	case BuildTemplateTest:
		manuscript = "# Template validation\n\nA citation-free equation: $x^2$.\n"
		referencesBIB = ""
		manifest = map[string]interface{}{"schema_version": "1.0", "template_test": true}
	default:
		return BuildJobInput{}, ErrInvalid
	}
	grant, err := service.Artifacts.ArticleTemplateGrant(ctx, job.ProjectID, template.ArtifactID, template.VersionID)
	if err != nil {
		return BuildJobInput{}, err
	}
	resources := []map[string]interface{}{}
	seen := map[string]struct{}{}
	for _, reference := range frozenReferences {
		if reference.ReferenceType != "artifact" {
			continue
		}
		key := reference.SourceObjectID + ":" + reference.SourceVersionID
		if _, exists := seen[key]; exists {
			continue
		}
		resourceGrant, grantErr := service.Artifacts.ArticleResourceGrant(ctx, job.ProjectID, reference.SourceObjectID, reference.SourceVersionID)
		if grantErr != nil {
			return BuildJobInput{}, grantErr
		}
		resources = append(resources, map[string]interface{}{
			"artifact_id": reference.SourceObjectID, "version_id": reference.SourceVersionID,
			"title": reference.Title, "filename": resourceGrant["filename"],
			"mime_type": resourceGrant["mime_type"], "size_bytes": resourceGrant["size_bytes"],
			"sha256":   resourceGrant["sha256"],
			"transfer": map[string]interface{}{"method": resourceGrant["method"], "url": resourceGrant["url"], "headers": resourceGrant["headers"], "expires_at": resourceGrant["expires_at"]},
		})
		seen[key] = struct{}{}
	}
	return BuildJobInput{BuildID: build.BuildID, ProjectID: job.ProjectID, BuildKind: build.BuildKind, Manuscript: manuscript, ReferencesBIB: referencesBIB, ArticleManifest: manifest, Template: map[string]interface{}{"artifact_id": template.ArtifactID, "version_id": template.VersionID, "manifest": template.Manifest, "transfer": grant}, Engine: build.Engine, BibliographyTool: build.BibliographyTool, Limits: map[string]interface{}{"timeout_seconds": 600, "memory_bytes": 1073741824, "disk_bytes": int64(2 * 1024 * 1024 * 1024), "output_bytes": maxOutputBytes, "network": "none"}, Toolchain: map[string]interface{}{"pandoc": "pandoc 2.17.1.1", "latexmk": "Version 4.79", "texlive": "TeX Live 2022/Debian"}, Resources: resources}, nil
}
func (service *Service) WorkerOutput(ctx context.Context, caller auth.Identity, jobID, role, filename, mimeType, expectedSHA string, expectedSize int64, input io.Reader) (BuildOutput, error) {
	job, build, _, err := service.workerBuild(ctx, caller, jobID)
	if err != nil {
		return BuildOutput{}, err
	}
	if build.Status == BuildSuperseded {
		return BuildOutput{}, ErrSuperseded
	}
	if build.Status != BuildQueued && build.Status != BuildRunning {
		return BuildOutput{}, ErrConflict
	}
	if !rolePattern.MatchString(role) || !shaPattern.MatchString(strings.ToLower(expectedSHA)) || expectedSize < 1 || expectedSize > maxOutputBytes || !safeFilename(filename) {
		return BuildOutput{}, ErrInvalid
	}
	artifactID, versionID, err := service.Artifacts.ArchiveArticleBuildOutput(ctx, job.ProjectID, build.BuildID, build.CreatedBy, role, filename, mimeType, expectedSHA, expectedSize, input)
	if err != nil {
		return BuildOutput{}, err
	}
	output := BuildOutput{Role: role, ArtifactID: artifactID, VersionID: versionID, Filename: filename, MIMEType: mimeType, SHA256: strings.ToLower(expectedSHA), SizeBytes: expectedSize}
	if err = service.Store.AddBuildOutput(ctx, build.BuildID, output); err != nil {
		return BuildOutput{}, err
	}
	return output, nil
}
func (service *Service) workerBuild(ctx context.Context, caller auth.Identity, jobID string) (jobs.Job, Build, Template, error) {
	if service.JobAccess == nil {
		return jobs.Job{}, Build{}, Template{}, ErrUnavailable
	}
	job, err := service.JobAccess.ClaimedWorkerJob(ctx, caller, jobID)
	if err != nil {
		return jobs.Job{}, Build{}, Template{}, err
	}
	if job.JobType != JobTypeBuild {
		return jobs.Job{}, Build{}, Template{}, ErrNotFound
	}
	buildID, _ := job.Payload["build_id"].(string)
	build, err := service.Store.GetBuild(ctx, job.ProjectID, buildID)
	if err != nil {
		return jobs.Job{}, Build{}, Template{}, err
	}
	if build.JobID != job.ID {
		return jobs.Job{}, Build{}, Template{}, ErrInvalid
	}
	template, err := service.Store.GetTemplate(ctx, job.ProjectID, build.TemplateID)
	return job, build, template, err
}

func (service *Service) PrepareComplete(ctx context.Context, job jobs.Job, result map[string]interface{}) error {
	if job.JobType != JobTypeBuild {
		return nil
	}
	buildID, _ := job.Payload["build_id"].(string)
	build, err := service.Store.GetBuild(ctx, job.ProjectID, buildID)
	if err != nil {
		return err
	}
	if build.Status == BuildSuperseded {
		return nil
	}
	if !hasRequiredOutputs(build.Outputs) {
		return ErrNotReady
	}
	toolchain, ok := result["toolchain"].(map[string]interface{})
	if !ok || len(toolchain) == 0 {
		return ErrInvalid
	}
	return nil
}
func (service *Service) ClaimInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job) error {
	if job.JobType != JobTypeBuild {
		return nil
	}
	return service.Store.MarkBuildRunning(ctx, tx, job.ID)
}
func (service *Service) CompleteInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job, result map[string]interface{}) error {
	if job.JobType != JobTypeBuild {
		return nil
	}
	_, err := service.Store.CompleteBuild(ctx, tx, job.ID, result)
	return err
}
func (service *Service) FailInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job, failure jobs.Failure) error {
	if job.JobType != JobTypeBuild {
		return nil
	}
	_, err := service.Store.FailBuild(ctx, tx, job.ID, failure.Code, failure.Message)
	return err
}

func (service *Service) UpdateZotero(ctx context.Context, caller auth.Identity, projectID, libraryType, libraryID, collectionKey, apiKey string) (ZoteroBinding, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleZotero); err != nil {
		return ZoteroBinding{}, err
	}
	if libraryType != "user" && libraryType != "group" || libraryID == "" || apiKey == "" {
		return ZoteroBinding{}, ErrInvalid
	}
	if _, err := service.Settings.Update(ctx, caller, settings.ScopeProject, projectID, SettingTypeZotero, map[string]interface{}{"api_key": apiKey}); err != nil {
		return ZoteroBinding{}, err
	}
	return service.Store.UpsertZoteroBinding(ctx, ZoteroBinding{ProjectID: projectID, LibraryType: libraryType, LibraryID: libraryID, CollectionKey: collectionKey, APIKeyConfigured: true, ReadOnly: true}, caller.ActorID())
}
func (service *Service) GetZotero(ctx context.Context, caller auth.Identity, projectID string) (ZoteroBinding, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return ZoteroBinding{}, err
	}
	return service.Store.GetZoteroBinding(ctx, projectID)
}
func (service *Service) DeleteZotero(ctx context.Context, caller auth.Identity, projectID string) error {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleZotero); err != nil {
		return err
	}
	if err := service.Settings.Delete(ctx, caller, settings.ScopeProject, projectID, SettingTypeZotero); err != nil && !errors.Is(err, settings.ErrNotFound) {
		return err
	}
	return service.Store.DeleteZoteroBinding(ctx, projectID)
}
func (service *Service) SearchZotero(ctx context.Context, caller auth.Identity, projectID, query string) ([]ZoteroItem, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return nil, err
	}
	binding, err := service.Store.GetZoteroBinding(ctx, projectID)
	if err != nil {
		return nil, err
	}
	resolved, err := service.Settings.Resolve(ctx, settings.ScopeProject, projectID, SettingTypeZotero)
	if err != nil {
		return nil, err
	}
	apiKey, _ := resolved.Values["api_key"].(string)
	if apiKey == "" {
		return nil, ErrNotReady
	}
	base := "https://api.zotero.org/" + binding.LibraryType + "s/" + url.PathEscape(binding.LibraryID) + "/items"
	values := url.Values{"q": []string{query}, "format": []string{"json"}, "limit": []string{"50"}}
	if binding.CollectionKey != "" {
		base = "https://api.zotero.org/" + binding.LibraryType + "s/" + url.PathEscape(binding.LibraryID) + "/collections/" + url.PathEscape(binding.CollectionKey) + "/items"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Zotero-API-Key", apiKey)
	response, err := service.httpClient().Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	var raw []map[string]interface{}
	if json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&raw) != nil {
		return nil, ErrUnavailable
	}
	items := make([]ZoteroItem, 0, len(raw))
	for _, entry := range raw {
		data := object(entry["data"])
		item := ZoteroItem{ItemKey: stringValue(entry["key"]), Version: int64Value(entry["version"]), CitationKey: stringValue(data["citationKey"]), Title: stringValue(data["title"]), ItemType: stringValue(data["itemType"]), DOI: stringValue(data["DOI"]), Raw: entry, Authors: []string{}}
		if date := stringValue(data["date"]); len(date) >= 4 {
			item.Year = date[:4]
		}
		if item.CitationKey == "" {
			item.CitationKey = "zotero" + safeID(item.ItemKey)
		}
		if creators, ok := data["creators"].([]interface{}); ok {
			for _, rawCreator := range creators {
				creator := object(rawCreator)
				name := strings.TrimSpace(stringValue(creator["firstName"]) + " " + stringValue(creator["lastName"]))
				if name == "" {
					name = stringValue(creator["name"])
				}
				if name != "" {
					item.Authors = append(item.Authors, name)
				}
			}
		}
		items = append(items, item)
	}
	return items, nil
}

func (service *Service) authorize(ctx context.Context, caller auth.Identity, projectID string, permission project.Permission) error {
	if service.Access == nil || strings.TrimSpace(projectID) == "" || service.Access.Authorize(ctx, caller, projectID, permission) != nil {
		return ErrForbidden
	}
	return nil
}
func (service *Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock.Now().UTC()
}
func (service *Service) httpClient() *http.Client {
	if service.HTTPClient != nil {
		return service.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}
func displayName(caller auth.Identity) string {
	if caller.User.DisplayName != "" {
		return caller.User.DisplayName
	}
	if caller.User.Email != "" {
		return caller.User.Email
	}
	return caller.ActorID()
}
func hashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
func kindPriority(kind string) int {
	if kind == BuildFormal {
		return 50
	}
	if kind == BuildTemplateTest {
		return 20
	}
	return 10
}
func hasRequiredOutputs(outputs []BuildOutput) bool {
	roles := map[string]bool{}
	for _, output := range outputs {
		roles[output.Role] = true
	}
	for _, required := range []string{"pdf", "tex_source", "source_zip", "build_report", "log"} {
		if !roles[required] {
			return false
		}
	}
	return true
}
func safeFilename(value string) bool {
	return value != "" && len(value) <= 255 && path.Base(value) == value && value != "." && value != ".." && !strings.ContainsAny(value, "\\/\x00")
}
func stringValue(value interface{}) string { text, _ := value.(string); return text }
func int64Value(value interface{}) int64 {
	switch value := value.(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	}
	return 0
}
func decodeManifest(raw map[string]interface{}) (TemplateManifest, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return TemplateManifest{}, ErrInvalid
	}
	var manifest TemplateManifest
	if json.Unmarshal(encoded, &manifest) != nil {
		return TemplateManifest{}, ErrInvalid
	}
	if manifest.SchemaVersion != "1.0" || manifest.Name == "" || manifest.Version == "" || !safeTemplatePath(manifest.Entrypoint, ".tex") || !safeTemplatePath(manifest.ContentTarget, ".tex") || !safeTemplatePath(manifest.BibliographyTarget, ".bib") || !safeTemplatePath(manifest.Output, ".pdf") || !validEngine(manifest.Engine) || !validBibliographyTool(manifest.BibliographyTool) {
		return TemplateManifest{}, ErrInvalid
	}
	return manifest, nil
}
func safeTemplatePath(value, suffix string) bool {
	return value != "" && len(value) <= 255 && !strings.HasPrefix(value, "/") && !strings.Contains(value, "\\") && path.Clean(value) == value && !strings.HasPrefix(value, "../") && strings.HasSuffix(strings.ToLower(value), suffix)
}
func validEngine(value string) bool {
	return value == "auto" || value == "pdflatex" || value == "xelatex" || value == "lualatex"
}
func validBibliographyTool(value string) bool {
	return value == "auto" || value == "bibtex" || value == "biber" || value == "none"
}

var _ jobs.LifecycleHook = (*Service)(nil)
