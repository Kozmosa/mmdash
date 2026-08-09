package model

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

type lifecycleStoreStub struct {
	Store
	sources             []Source
	questions           []Question
	discoveredQuestions []Question
	pages               []SourcePage
	sync                Sync
	latestHash          string
	createdSyncs        []Sync
	createdJobs         []jobs.CreateInput
	createdNextSyncAt   []*time.Time
	advancedSourceID    string
	advancedOwner       string
	advancedNext        time.Time
	advancedAt          time.Time
	disabledProjectID   string
	disabledActorID     string
}

func (store *lifecycleStoreStub) DisableSource(_ context.Context, projectID, actorID string, _ time.Time) error {
	store.disabledProjectID, store.disabledActorID = projectID, actorID
	return nil
}

type settingResolverStub struct {
	setting settings.ResolvedSetting
	err     error
}

func (stub settingResolverStub) Resolve(context.Context, settings.Scope, string, string) (settings.ResolvedSetting, error) {
	return stub.setting, stub.err
}

func (store *lifecycleStoreStub) ClaimDueSources(context.Context, string, time.Time, time.Duration, int) ([]Source, error) {
	return store.sources, nil
}

func (store *lifecycleStoreStub) GetSource(context.Context, string) (Source, error) {
	if len(store.sources) == 0 {
		return Source{}, ErrNotConfigured
	}
	return store.sources[0], nil
}

func (store *lifecycleStoreStub) GetQuestion(_ context.Context, _, questionID string) (Question, error) {
	for _, question := range store.questions {
		if question.ID == questionID {
			return question, nil
		}
	}
	return Question{}, ErrNotFound
}

func (store *lifecycleStoreStub) ListQuestions(context.Context, string) ([]Question, error) {
	return store.questions, nil
}

func (store *lifecycleStoreStub) ListSourcePages(context.Context, string) ([]SourcePage, error) {
	return store.pages, nil
}

func (store *lifecycleStoreStub) CreateSync(_ context.Context, item Sync, input jobs.CreateInput, next *time.Time) (Sync, error) {
	store.createdSyncs = append(store.createdSyncs, item)
	store.createdJobs = append(store.createdJobs, input)
	store.createdNextSyncAt = append(store.createdNextSyncAt, next)
	item.JobID = "job-" + item.ID
	return item, nil
}

func (store *lifecycleStoreStub) CreateSyncInTransaction(_ context.Context, _ transaction.Tx, item Sync, input jobs.CreateInput, next *time.Time) (Sync, error) {
	return store.CreateSync(context.Background(), item, input, next)
}

func (store *lifecycleStoreStub) CompleteDiscoverInTransaction(_ context.Context, _ transaction.Tx, _ jobs.Job, _ DiscoverResult, _ time.Time) (Sync, error) {
	return store.sync, nil
}

func (store *lifecycleStoreStub) ListDiscoveredQuestionsInTransaction(context.Context, transaction.Tx, string) ([]Question, error) {
	return store.discoveredQuestions, nil
}

func (store *lifecycleStoreStub) AdvanceSchedule(_ context.Context, sourceID, owner string, next, now time.Time) error {
	store.advancedSourceID = sourceID
	store.advancedOwner = owner
	store.advancedNext = next
	store.advancedAt = now
	return nil
}

func (store *lifecycleStoreStub) GetSyncByJob(context.Context, string) (Sync, error) {
	return store.sync, nil
}

func (store *lifecycleStoreStub) LatestSnapshotHash(context.Context, string) (string, error) {
	return store.latestHash, nil
}

type artifactImporterStub struct {
	inputs []ModelFileImport
	err    error
}

func (stub *artifactImporterStub) ImportModelFile(_ context.Context, input ModelFileImport) (ModelFileReference, error) {
	stub.inputs = append(stub.inputs, input)
	if stub.err != nil {
		return ModelFileReference{}, stub.err
	}
	return ModelFileReference{ArtifactID: "artifact-id", VersionID: "version-id", Filename: "diagram.png", MIMEType: "image/png"}, nil
}

func TestRunScheduledSyncsQueuesDiscoveryBeforeQuestionFanout(t *testing.T) {
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	store := &lifecycleStoreStub{
		sources: []Source{{
			ID:                      "source-id",
			ProjectID:               "project-id",
			AutoSyncEnabled:         true,
			AutoSyncIntervalSeconds: 300,
			UpdatedBy:               "user-id",
		}},
	}
	service := Service{
		Clock:     clock.Fixed{Time: now},
		Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 64))},
		Store:     store,
	}

	processed, err := service.RunScheduledSyncs(context.Background(), "scheduler-1", 10)
	if err != nil {
		t.Fatalf("run scheduled syncs: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}
	if len(store.createdSyncs) != 1 {
		t.Fatalf("created sync count = %d, want source discovery only before fresh results", len(store.createdSyncs))
	}
	if store.createdSyncs[0].Scope != SyncScopeSource {
		t.Fatalf("unexpected scheduled syncs: %#v", store.createdSyncs)
	}
	for index, item := range store.createdSyncs {
		if item.Trigger != SyncTriggerScheduled {
			t.Fatalf("sync %d trigger = %q", index, item.Trigger)
		}
		if store.createdNextSyncAt[index] != nil {
			t.Fatalf("scheduled sync %d reset countdown before lease advance", index)
		}
	}
	if store.advancedSourceID != "source-id" || store.advancedOwner != "scheduler-1" {
		t.Fatalf("advance schedule ownership = %q/%q", store.advancedSourceID, store.advancedOwner)
	}
	if !store.advancedAt.Equal(now) || !store.advancedNext.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("advanced schedule = %s -> %s", store.advancedAt, store.advancedNext)
	}
}

func TestDiscoverCompletionFansOutFreshlyDiscoveredQuestions(t *testing.T) {
	now := time.Date(2026, 8, 9, 8, 10, 0, 0, time.UTC)
	store := &lifecycleStoreStub{
		sync:                Sync{ID: "source-sync", ProjectID: "project-id", SourceID: "source-id", Trigger: SyncTriggerScheduled, RequestedBy: "user-id"},
		discoveredQuestions: []Question{{ID: "question-fresh", ProjectID: "project-id", SourceID: "source-id", NotionPageID: "page-fresh"}},
	}
	service := Service{
		Clock: clock.Fixed{Time: now}, Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 16))}, Store: store,
	}
	raw := map[string]interface{}{
		"mode": "discover", "sync_id": "source-sync", "root_title": "Models",
		"pages": []interface{}{map[string]interface{}{"page_id": "page-fresh", "title": "Q1", "url": "https://notion.site/page-fresh", "depth": float64(1), "has_children": false}},
	}
	job := jobs.Job{ID: "job-source", JobType: JobTypeDiscover, ProjectID: "project-id"}

	if err := service.CompleteInTransaction(context.Background(), nil, job, raw); err != nil {
		t.Fatalf("complete discovery: %v", err)
	}
	if len(store.createdSyncs) != 1 {
		t.Fatalf("fan-out sync count = %d, want 1", len(store.createdSyncs))
	}
	created := store.createdSyncs[0]
	if created.QuestionID != "question-fresh" || created.Scope != SyncScopeQuestion || created.Trigger != SyncTriggerScheduled || created.RequestedBy != "user-id" {
		t.Fatalf("unexpected fresh discovery fan-out: %#v", created)
	}
	if store.createdNextSyncAt[0] != nil {
		t.Fatal("discovery fan-out reset the countdown a second time")
	}
}

type modelSyncAccessStub struct {
	permission project.Permission
}

func (stub *modelSyncAccessStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return auth.Identity{}, nil
}

func (stub *modelSyncAccessStub) Authorize(_ context.Context, _ auth.Identity, _ string, permission project.Permission) error {
	stub.permission = permission
	return nil
}

func TestManualSyncUsesTeamMemberSyncPermission(t *testing.T) {
	now := time.Date(2026, 8, 9, 8, 20, 0, 0, time.UTC)
	access := &modelSyncAccessStub{}
	store := &lifecycleStoreStub{
		sources:   []Source{{ID: "source-id", ProjectID: "project-id", AutoSyncEnabled: true, AutoSyncIntervalSeconds: 300}},
		questions: []Question{{ID: "question-id", ProjectID: "project-id", SourceID: "source-id", NotionPageID: "page-id"}},
		pages:     []SourcePage{{NotionPageID: "page-id"}},
	}
	service := Service{Access: access, Clock: clock.Fixed{Time: now}, Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 32))}, Store: store}
	caller := auth.Identity{Kind: "session", User: auth.User{ID: "viewer-id"}}

	if _, err := service.RequestSourceSync(context.Background(), caller, "project-id", SyncTriggerManual); err != nil {
		t.Fatalf("request source sync: %v", err)
	}
	if access.permission != project.PermissionModelSync {
		t.Fatalf("permission = %q, want %q", access.permission, project.PermissionModelSync)
	}
	access.permission = ""
	if _, err := service.RequestQuestionSync(context.Background(), caller, "project-id", "question-id", SyncTriggerManual); err != nil {
		t.Fatalf("request question sync: %v", err)
	}
	if access.permission != project.PermissionModelSync {
		t.Fatalf("question permission = %q, want %q", access.permission, project.PermissionModelSync)
	}
}

func TestRunScheduledSyncsDisablesUnboundSourceWithoutCreatingJobs(t *testing.T) {
	now := time.Date(2026, 8, 9, 8, 15, 0, 0, time.UTC)
	store := &lifecycleStoreStub{sources: []Source{{
		ID: "source-id", ProjectID: "project-id", AutoSyncEnabled: true,
		AutoSyncIntervalSeconds: 300, UpdatedBy: "user-id",
	}}}
	service := Service{
		Clock: clock.Fixed{Time: now}, Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		Settings: settingResolverStub{err: settings.ErrNotFound}, Store: store,
	}

	processed, err := service.RunScheduledSyncs(context.Background(), "scheduler-1", 10)
	if err != nil {
		t.Fatalf("run scheduled syncs: %v", err)
	}
	if processed != 0 || len(store.createdJobs) != 0 || store.disabledProjectID != "project-id" || store.disabledActorID != "user-id" {
		t.Fatalf("unbound source was not stopped: processed=%d jobs=%d disabled=%s/%s", processed, len(store.createdJobs), store.disabledProjectID, store.disabledActorID)
	}
}

func TestManualQuestionSyncResetsAutomaticCountdown(t *testing.T) {
	now := time.Date(2026, 8, 9, 8, 30, 0, 0, time.UTC)
	store := &lifecycleStoreStub{}
	service := Service{
		Clock:     clock.Fixed{Time: now},
		Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		Store:     store,
	}
	source := Source{ID: "source-id", ProjectID: "project-id", AutoSyncEnabled: true, AutoSyncIntervalSeconds: 300}
	question := Question{ID: "question-id", ProjectID: "project-id", SourceID: "source-id", NotionPageID: "page-id"}

	if _, err := service.requestSync(context.Background(), "user-id", source, question, SyncScopeQuestion, SyncTriggerManual, true); err != nil {
		t.Fatalf("request manual sync: %v", err)
	}
	if len(store.createdNextSyncAt) != 1 || store.createdNextSyncAt[0] == nil {
		t.Fatal("manual sync did not reset the automatic countdown")
	}
	if want := now.Add(5 * time.Minute); !store.createdNextSyncAt[0].Equal(want) {
		t.Fatalf("next sync = %s, want %s", store.createdNextSyncAt[0], want)
	}
}

func TestPrepareCompleteTransfersNotionMediaAndRemovesTemporaryURL(t *testing.T) {
	store := &lifecycleStoreStub{sync: Sync{
		ID: "sync-id", ProjectID: "project-id", QuestionID: "question-id", RequestedBy: "user-id",
	}}
	artifacts := &artifactImporterStub{}
	service := Service{Artifacts: artifacts, Store: store}
	raw := map[string]interface{}{
		"mode":             "snapshot",
		"sync_id":          "sync-id",
		"question_id":      "question-id",
		"title":            "Q1 model",
		"content_hash":     strings.Repeat("a", 64),
		"summary":          "summary",
		"outline":          []interface{}{},
		"content_markdown": "![diagram](temporary)",
		"content_text":     "diagram",
		"blocks": []interface{}{map[string]interface{}{
			"block_id": "block-1", "type": "image", "text": "", "rich_text": []interface{}{}, "children": []interface{}{},
		}},
		"media": []interface{}{map[string]interface{}{
			"source_block_id": "block-1", "url": "https://files.notion.test/temporary", "filename": "source.png", "mime_type": "image/png",
		}},
	}

	if err := service.PrepareComplete(context.Background(), jobs.Job{ID: "job-id", JobType: JobTypeSnapshot}, raw); err != nil {
		t.Fatalf("prepare complete: %v", err)
	}
	if len(artifacts.inputs) != 1 || artifacts.inputs[0].SourceObjectID != "question-id" {
		t.Fatalf("Artifact import inputs = %#v", artifacts.inputs)
	}
	media := raw["media"].([]interface{})[0].(map[string]interface{})
	if _, exists := media["url"]; exists {
		t.Fatalf("temporary Notion URL remained in durable Worker result: %#v", media)
	}
	if media["artifact_id"] != "artifact-id" || media["artifact_version_id"] != "version-id" {
		t.Fatalf("Artifact references missing from media: %#v", media)
	}
	block := raw["blocks"].([]interface{})[0].(map[string]interface{})
	if block["artifact_id"] != "artifact-id" || block["artifact_version_id"] != "version-id" {
		t.Fatalf("Artifact references missing from normalized block: %#v", block)
	}
}

func TestPrepareCompleteSkipsMediaTransferWhenHashIsUnchanged(t *testing.T) {
	hash := strings.Repeat("b", 64)
	store := &lifecycleStoreStub{
		sync:       Sync{ID: "sync-id", ProjectID: "project-id", QuestionID: "question-id", RequestedBy: "user-id"},
		latestHash: hash,
	}
	artifacts := &artifactImporterStub{}
	service := Service{Artifacts: artifacts, Store: store}
	raw := map[string]interface{}{
		"mode": "snapshot", "sync_id": "sync-id", "question_id": "question-id", "title": "Q1 model", "content_hash": hash,
		"summary": "", "outline": []interface{}{}, "blocks": []interface{}{}, "content_markdown": "", "content_text": "",
		"media": []interface{}{map[string]interface{}{"source_block_id": "block-1", "url": "https://files.notion.test/temporary", "filename": "source.png", "mime_type": "image/png"}},
	}

	if err := service.PrepareComplete(context.Background(), jobs.Job{ID: "job-id", JobType: JobTypeSnapshot}, raw); err != nil {
		t.Fatalf("prepare unchanged complete: %v", err)
	}
	if len(artifacts.inputs) != 0 {
		t.Fatalf("unchanged snapshot imported %d media files", len(artifacts.inputs))
	}
	if media := raw["media"].([]interface{}); len(media) != 0 {
		t.Fatalf("unchanged result retained media work: %#v", media)
	}
}

func TestPrepareCompleteReturnsSafeMediaTransferError(t *testing.T) {
	store := &lifecycleStoreStub{sync: Sync{
		ID: "sync-id", ProjectID: "project-id", QuestionID: "question-id", RequestedBy: "user-id",
	}}
	service := Service{Artifacts: &artifactImporterStub{err: errors.New("signed URL timed out")}, Store: store}
	raw := map[string]interface{}{
		"mode": "snapshot", "sync_id": "sync-id", "question_id": "question-id", "title": "Q1 model", "content_hash": strings.Repeat("c", 64),
		"summary": "", "outline": []interface{}{}, "content_markdown": "", "content_text": "",
		"blocks": []interface{}{map[string]interface{}{"block_id": "block-1", "type": "image", "text": "", "rich_text": []interface{}{}, "children": []interface{}{}}},
		"media":  []interface{}{map[string]interface{}{"source_block_id": "block-1", "url": "https://files.notion.test/temporary", "filename": "source.png", "mime_type": "image/png"}},
	}

	err := service.PrepareComplete(context.Background(), jobs.Job{ID: "job-id", JobType: JobTypeSnapshot}, raw)
	var applicationError *apperror.Error
	if !errors.As(err, &applicationError) {
		t.Fatalf("prepare complete error = %T %v, want application error", err, err)
	}
	if applicationError.Code != "MODEL_MEDIA_IMPORT_FAILED" || applicationError.Status != 502 {
		t.Fatalf("application error = %#v", applicationError)
	}
}
