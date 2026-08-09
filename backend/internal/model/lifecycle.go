package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

const modelResultMaxBytes = 32 * 1024 * 1024

var notionOAuthRefreshLock sync.Mutex

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
	token := settingString(resolved, "access_token")
	legacyToken := false
	if token == "" {
		token = settingString(resolved, "integration_token")
		legacyToken = token != ""
	}
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
	result, err := service.Notion.Export(ctx, token, request)
	if !errors.Is(err, ErrNotionUnauthorized) || legacyToken {
		return result, err
	}
	refreshedToken, refreshErr := service.refreshNotionOAuthCredential(ctx, source, token)
	if refreshErr != nil {
		return NotionExport{}, refreshErr
	}
	return service.Notion.Export(ctx, refreshedToken, request)
}

func (service Service) refreshNotionOAuthCredential(ctx context.Context, source Source, rejectedAccessToken string) (string, error) {
	if service.OAuth == nil || !service.OAuth.Available() || service.OAuthSettings == nil || service.Settings == nil {
		return "", ErrNotConfigured
	}
	// A Core process serializes rotations so concurrent Source and Question jobs
	// reuse the first successful rotation instead of consuming the same refresh
	// token twice.
	notionOAuthRefreshLock.Lock()
	defer notionOAuthRefreshLock.Unlock()
	resolved, err := service.Settings.Resolve(ctx, settings.ScopeProject, source.ProjectID, SettingTypeNotion)
	if err != nil {
		return "", err
	}
	currentAccessToken := settingString(resolved, "access_token")
	if currentAccessToken != "" && currentAccessToken != rejectedAccessToken {
		return currentAccessToken, nil
	}
	refreshToken := settingString(resolved, "refresh_token")
	if currentAccessToken == "" || refreshToken == "" {
		return "", ErrNotConfigured
	}
	tokens, err := service.OAuth.Refresh(ctx, refreshToken)
	if err != nil {
		return "", ErrNotConfigured
	}
	actorID := source.UpdatedBy
	if actorID == "" {
		actorID = source.CreatedBy
	}
	if actorID == "" {
		return "", ErrNotConfigured
	}
	if err := service.OAuthSettings.RotateSecrets(ctx, actorID, settings.ScopeProject, source.ProjectID, SettingTypeNotion, map[string]string{
		"access_token": tokens.AccessToken, "refresh_token": tokens.RefreshToken,
	}); err != nil {
		service.record(ctx, auth.Identity{Kind: "system", User: auth.User{ID: actorID}}, source.ProjectID, "model.notion.oauth.refreshed", "error", source.ID, nil)
		return "", err
	}
	service.record(ctx, auth.Identity{Kind: "system", User: auth.User{ID: actorID}}, source.ProjectID, "model.notion.oauth.refreshed", "success", source.ID, nil)
	return tokens.AccessToken, nil
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
		parent, err := service.Store.CompleteDiscoverInTransaction(ctx, tx, job, result, service.now())
		if err != nil {
			return err
		}
		questions, err := service.Store.ListDiscoveredQuestionsInTransaction(ctx, tx, parent.ProjectID)
		if err != nil {
			return err
		}
		source := Source{ID: parent.SourceID, ProjectID: parent.ProjectID}
		for _, question := range questions {
			if _, err := service.requestSyncInTransaction(ctx, tx, parent.RequestedBy, source, question, SyncScopeQuestion, parent.Trigger); err != nil {
				return err
			}
		}
		return nil
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

// RunScheduledSyncs claims due sources, schedules discovery, and advances the
// countdown from the actual trigger time. Successful discovery fans out the
// bound-question snapshots from the freshly persisted descendant set.
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
		active, err := service.sourceAuthorizationActive(ctx, source.ProjectID, actorID)
		if err != nil {
			return processed, err
		}
		if !active {
			if err := service.Store.DisableSource(ctx, source.ProjectID, actorID, now); err != nil {
				return processed, err
			}
			continue
		}
		if _, err := service.requestSync(ctx, actorID, source, Question{}, SyncScopeSource, SyncTriggerScheduled, false); err != nil && !errors.Is(err, ErrConflict) {
			return processed, err
		}
		next := now.Add(time.Duration(source.AutoSyncIntervalSeconds) * time.Second)
		if err := service.Store.AdvanceSchedule(ctx, source.ID, owner, next, now); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (service Service) sourceAuthorizationActive(ctx context.Context, projectID, actorID string) (bool, error) {
	// Tests and deliberately reduced embeddings may omit Settings. Production
	// Core always wires it; there, a missing or invalid binding disables the
	// schedule before any automatic Job can be created.
	if service.Settings == nil {
		return true, nil
	}
	resolved, err := service.Settings.Resolve(ctx, settings.ScopeProject, projectID, SettingTypeNotion)
	if errors.Is(err, settings.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = sourceConfig(projectID, actorID, resolved)
	if errors.Is(err, ErrNotConfigured) || errors.Is(err, ErrInvalid) {
		return false, nil
	}
	return err == nil, err
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
