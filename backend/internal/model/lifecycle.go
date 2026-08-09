package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

const modelResultMaxBytes = 32 * 1024 * 1024

// WorkerExport returns raw Notion page data only to the Worker holding the
// live Job lease. The integration token is resolved and consumed inside Core.
func (service Service) WorkerExport(ctx context.Context, caller auth.Identity, jobID string) (NotionExport, error) {
	if service.Jobs == nil || service.Settings == nil || service.Notion == nil {
		return NotionExport{}, ErrSyncUnavailable
	}
	job, err := service.Jobs.ClaimedWorkerJob(ctx, caller, jobID)
	if err != nil {
		return NotionExport{}, err
	}
	if !ownedJob(job.JobType) {
		return NotionExport{}, ErrNotFound
	}
	sync, err := service.Store.GetSyncByJob(ctx, job.ID)
	if err != nil {
		return NotionExport{}, err
	}
	if sync.ProjectID != job.ProjectID || sync.ID != jobPayloadString(job, "sync_id") {
		return NotionExport{}, ErrInvalid
	}
	source, err := service.Store.GetSource(ctx, job.ProjectID)
	if err != nil || source.ID != sync.SourceID {
		return NotionExport{}, ErrNotConfigured
	}
	resolved, err := service.Settings.Resolve(ctx, settings.ScopeProject, job.ProjectID, SettingTypeNotion)
	if err != nil {
		return NotionExport{}, err
	}
	token := settingString(resolved, "integration_token")
	if token == "" {
		return NotionExport{}, ErrNotConfigured
	}
	request := NotionExportRequest{SyncID: sync.ID, ProjectID: sync.ProjectID, SourceID: sync.SourceID, RootPageID: source.NotionRootPageID}
	switch job.JobType {
	case JobTypeDiscover:
		request.Mode = "discover"
	case JobTypeSnapshot:
		question, err := service.Store.GetQuestion(ctx, sync.ProjectID, sync.QuestionID)
		if err != nil {
			return NotionExport{}, err
		}
		request.Mode, request.QuestionID, request.TargetPageID = "snapshot", question.ID, question.NotionPageID
	default:
		return NotionExport{}, ErrNotFound
	}
	return service.Notion.Export(ctx, token, request)
}

// PrepareComplete validates a bounded Worker result and transfers temporary
// Notion media into Artifact before the Job result transaction commits.
func (service Service) PrepareComplete(ctx context.Context, job jobs.Job, raw map[string]interface{}) error {
	if !ownedJob(job.JobType) {
		return nil
	}
	sync, err := service.Store.GetSyncByJob(ctx, job.ID)
	if err != nil {
		return err
	}
	switch job.JobType {
	case JobTypeDiscover:
		var result DiscoverResult
		if err := decodeModelResult(raw, &result); err != nil {
			return err
		}
		if err := validateDiscoverResult(result); err != nil {
			return err
		}
		if result.SyncID != sync.ID {
			return ErrInvalid
		}
		return replaceResult(raw, result)
	case JobTypeSnapshot:
		var result SnapshotResult
		if err := decodeModelResult(raw, &result); err != nil {
			return err
		}
		if err := validateSnapshotResult(result); err != nil {
			return err
		}
		if result.SyncID != sync.ID || result.QuestionID != sync.QuestionID {
			return ErrInvalid
		}
		latestHash, err := service.Store.LatestSnapshotHash(ctx, sync.QuestionID)
		if err != nil {
			return err
		}
		if latestHash == result.ContentHash {
			result.Media = []MediaResult{}
			return replaceResult(raw, result)
		}
		if len(result.Media) > 0 && service.Artifacts == nil {
			return ErrSyncUnavailable
		}
		for index := range result.Media {
			media := &result.Media[index]
			if strings.TrimSpace(media.SourceBlockID) == "" || strings.TrimSpace(media.URL) == "" || strings.TrimSpace(media.Filename) == "" {
				return ErrInvalid
			}
			reference, err := service.Artifacts.ImportModelFile(ctx, ModelFileImport{ProjectID: sync.ProjectID, CreatedBy: sync.RequestedBy, SourceObjectID: sync.QuestionID, SourceBlockID: media.SourceBlockID, URL: media.URL, Filename: media.Filename, MIMEType: media.MIMEType})
			if err != nil {
				return apperror.New(
					http.StatusBadGateway,
					"MODEL_MEDIA_IMPORT_FAILED",
					"Model media could not be transferred to Artifact",
				).WithCause(fmt.Errorf("import Notion model file: %w", err))
			}
			media.ArtifactID, media.ArtifactVersionID = reference.ArtifactID, reference.VersionID
			media.Filename, media.MIMEType, media.URL = reference.Filename, reference.MIMEType, ""
			attachArtifact(result.Blocks, *media)
		}
		return replaceResult(raw, result)
	default:
		return nil
	}
}

func (service Service) ClaimInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job) error {
	if !ownedJob(job.JobType) {
		return nil
	}
	return service.Store.ClaimSyncInTransaction(ctx, tx, job, service.now())
}

func (service Service) CompleteInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job, raw map[string]interface{}) error {
	if !ownedJob(job.JobType) {
		return nil
	}
	switch job.JobType {
	case JobTypeDiscover:
		var result DiscoverResult
		if err := decodeModelResult(raw, &result); err != nil {
			return err
		}
		return service.Store.CompleteDiscoverInTransaction(ctx, tx, job, result, service.now())
	case JobTypeSnapshot:
		var result SnapshotResult
		if err := decodeModelResult(raw, &result); err != nil {
			return err
		}
		return service.Store.CompleteSnapshotInTransaction(ctx, tx, job, result, service.now())
	default:
		return nil
	}
}

func (service Service) FailInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job, failure jobs.Failure) error {
	if !ownedJob(job.JobType) {
		return nil
	}
	return service.Store.FailSyncInTransaction(ctx, tx, job, failure, service.now())
}

// RunScheduledSyncs claims due sources, schedules one discovery plus each
// bound question, and advances the countdown from the actual trigger time.
func (service Service) RunScheduledSyncs(ctx context.Context, owner string, limit int) (int, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return 0, ErrInvalid
	}
	now := service.now()
	sources, err := service.Store.ClaimDueSources(ctx, owner, now, 30*time.Second, limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, source := range sources {
		actorID := source.UpdatedBy
		if actorID == "" {
			actorID = source.CreatedBy
		}
		if _, err := service.requestSync(ctx, actorID, source, Question{}, SyncScopeSource, SyncTriggerScheduled, false); err != nil && !errors.Is(err, ErrConflict) {
			return processed, err
		}
		questions, err := service.Store.ListQuestions(ctx, source.ProjectID)
		if err != nil {
			return processed, err
		}
		pages, err := service.Store.ListSourcePages(ctx, source.ProjectID)
		if err != nil {
			return processed, err
		}
		for _, question := range questions {
			if !questionPageDiscovered(question, pages) {
				continue
			}
			if _, err := service.requestSync(ctx, actorID, source, question, SyncScopeQuestion, SyncTriggerScheduled, false); err != nil && !errors.Is(err, ErrConflict) {
				return processed, err
			}
		}
		next := now.Add(time.Duration(source.AutoSyncIntervalSeconds) * time.Second)
		if err := service.Store.AdvanceSchedule(ctx, source.ID, owner, next, now); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func decodeModelResult(raw map[string]interface{}, target interface{}) error {
	encoded, err := json.Marshal(raw)
	if err != nil || len(encoded) > modelResultMaxBytes {
		return ErrInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrInvalid
	}
	return nil
}

func replaceResult(raw map[string]interface{}, value interface{}) error {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > modelResultMaxBytes {
		return ErrInvalid
	}
	var sanitized map[string]interface{}
	if err := json.Unmarshal(encoded, &sanitized); err != nil {
		return ErrInvalid
	}
	for key := range raw {
		delete(raw, key)
	}
	for key, value := range sanitized {
		raw[key] = value
	}
	return nil
}

func attachArtifact(blocks []Block, media MediaResult) bool {
	for index := range blocks {
		if blocks[index].ID == media.SourceBlockID {
			blocks[index].ArtifactID = media.ArtifactID
			blocks[index].ArtifactVersionID = media.ArtifactVersionID
			return true
		}
		if attachArtifact(blocks[index].Children, media) {
			return true
		}
	}
	return false
}
