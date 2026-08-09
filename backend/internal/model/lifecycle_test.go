package model

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
)

type lifecycleStoreStub struct {
	Store
	sources           []Source
	questions         []Question
	pages             []SourcePage
	sync              Sync
	latestHash        string
	createdSyncs      []Sync
	createdJobs       []jobs.CreateInput
	createdNextSyncAt []*time.Time
	advancedSourceID  string
	advancedOwner     string
	advancedNext      time.Time
	advancedAt        time.Time
}

func (store *lifecycleStoreStub) ClaimDueSources(context.Context, string, time.Time, time.Duration, int) ([]Source, error) {
	return store.sources, nil
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

func TestRunScheduledSyncsQueuesDiscoveryAndOnlyDiscoveredQuestions(t *testing.T) {
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	store := &lifecycleStoreStub{
		sources: []Source{{
			ID:                      "source-id",
			ProjectID:               "project-id",
			AutoSyncEnabled:         true,
			AutoSyncIntervalSeconds: 300,
			UpdatedBy:               "user-id",
		}},
		questions: []Question{
			{ID: "question-discovered", ProjectID: "project-id", SourceID: "source-id", NotionPageID: "page-discovered"},
			{ID: "question-stale", ProjectID: "project-id", SourceID: "source-id", NotionPageID: "page-outside-root"},
		},
		pages: []SourcePage{{NotionPageID: "page-discovered"}},
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
	if len(store.createdSyncs) != 2 {
		t.Fatalf("created sync count = %d, want source discovery plus one question", len(store.createdSyncs))
	}
	if store.createdSyncs[0].Scope != SyncScopeSource || store.createdSyncs[1].QuestionID != "question-discovered" {
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
