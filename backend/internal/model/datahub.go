package model

import (
	"context"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

type QuestionProjection struct {
	ID           string
	ProjectID    string
	SourceID     string
	Code         string
	Title        string
	NotionPageID string
	Status       string
	OccurredAt   time.Time
}

type SourceProjection struct {
	ID                      string
	ProjectID               string
	RootPageID              string
	RootTitle               string
	AutoSyncEnabled         bool
	AutoSyncIntervalSeconds int
	SyncStatus              string
	DiscoveredPageCount     int
	Status                  string
	OccurredAt              time.Time
}

type SnapshotProjection struct {
	ID                 string
	ProjectID          string
	QuestionID         string
	PreviousSnapshotID string
	Title              string
	Summary            string
	ContentHash        string
	Tags               []string
	CapturedAt         time.Time
}

type DataHubProjectionSink interface {
	ProjectModelSource(context.Context, contract.EventEnvelope, SourceProjection) error
	ProjectModelQuestion(context.Context, contract.EventEnvelope, QuestionProjection) error
	ProjectModelSnapshot(context.Context, contract.EventEnvelope, SnapshotProjection) error
}

type DataHubProjector struct {
	Store Store
	Sink  DataHubProjectionSink
}

func (projector DataHubProjector) Project(ctx context.Context, event contract.EventEnvelope) error {
	if event.ProjectID == nil || projector.Store == nil || projector.Sink == nil {
		return ErrInvalid
	}
	switch event.EventType {
	case "model.source.changed":
		sourceID, _ := event.Payload["source_id"].(string)
		status, _ := event.Payload["status"].(string)
		if sourceID == "" || status != "active" && status != "hidden" {
			return ErrInvalid
		}
		source, err := projector.Store.GetSource(ctx, *event.ProjectID)
		if err != nil {
			return err
		}
		if source.ID != sourceID {
			return ErrInvalid
		}
		return projector.Sink.ProjectModelSource(ctx, event, SourceProjection{
			ID: source.ID, ProjectID: source.ProjectID, RootPageID: source.NotionRootPageID,
			RootTitle: source.NotionRootTitle, AutoSyncEnabled: source.AutoSyncEnabled,
			AutoSyncIntervalSeconds: source.AutoSyncIntervalSeconds, SyncStatus: source.SyncStatus,
			DiscoveredPageCount: source.DiscoveredPageCount, Status: status, OccurredAt: event.OccurredAt,
		})
	case "model.question.changed":
		questionID, _ := event.Payload["question_id"].(string)
		status, _ := event.Payload["status"].(string)
		if questionID == "" || status != "active" && status != "hidden" {
			return ErrInvalid
		}
		projection := QuestionProjection{ID: questionID, ProjectID: *event.ProjectID, Status: status, OccurredAt: event.OccurredAt}
		if status == "active" {
			question, err := projector.Store.GetQuestion(ctx, *event.ProjectID, questionID)
			if err != nil {
				return err
			}
			projection.SourceID, projection.Code, projection.Title, projection.NotionPageID = question.SourceID, question.Code, question.Title, question.NotionPageID
		} else {
			projection.SourceID, _ = event.Payload["source_id"].(string)
			projection.Code, _ = event.Payload["code"].(string)
			projection.Title, _ = event.Payload["title"].(string)
			projection.NotionPageID, _ = event.Payload["notion_page_id"].(string)
		}
		return projector.Sink.ProjectModelQuestion(ctx, event, projection)
	case "model.snapshot.created":
		snapshotID, _ := event.Payload["snapshot_id"].(string)
		questionID, _ := event.Payload["question_id"].(string)
		if snapshotID == "" || questionID == "" {
			return ErrInvalid
		}
		snapshot, err := projector.Store.GetSnapshot(ctx, *event.ProjectID, questionID, snapshotID)
		if err != nil {
			return err
		}
		return projector.Sink.ProjectModelSnapshot(ctx, event, SnapshotProjection{ID: snapshot.ID, ProjectID: snapshot.ProjectID, QuestionID: snapshot.QuestionID, PreviousSnapshotID: snapshot.PreviousSnapshotID, Title: snapshot.Title, Summary: snapshot.Summary, ContentHash: snapshot.ContentHash, Tags: snapshot.Tags, CapturedAt: snapshot.CapturedAt})
	default:
		return ErrInvalid
	}
}

// ModelHomeItems supplies compact question cards without exposing Notion
// credentials or duplicating authoritative state in Home.
func (service Service) ModelHomeItems(ctx context.Context, caller auth.Identity, projectID string) ([]interface{}, error) {
	questions, err := service.ListQuestions(ctx, caller, projectID)
	if err != nil {
		return nil, err
	}
	items := make([]interface{}, 0, len(questions))
	for _, question := range questions {
		items = append(items, map[string]interface{}{
			"question_id": question.ID, "code": question.Code,
			"title": question.Title, "snapshot_count": question.SnapshotCount,
			"latest_snapshot_id": question.LatestSnapshotID,
			"sync_status":        question.SyncStatus,
			"last_synced_at":     question.LastSyncedAt,
			"summary":            strings.TrimSpace(question.LastErrorMessage),
		})
	}
	return items, nil
}
