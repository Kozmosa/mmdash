package datahub

import (
	"context"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/model"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func (store PostgresStore) ProjectModelSource(ctx context.Context, event contract.EventEnvelope, item model.SourceProjection) error {
	if event.ProjectID == nil || item.ProjectID != *event.ProjectID || item.ID == "" || item.RootPageID == "" {
		return ErrInvalid
	}
	objectID, err := store.Generator.New()
	if err != nil {
		return err
	}
	activityID, err := store.Generator.New()
	if err != nil {
		return err
	}
	title := item.RootTitle
	if title == "" {
		title = "Notion Model source"
	}
	metadata := map[string]interface{}{
		"notion_root_page_id":        item.RootPageID,
		"auto_sync_enabled":          item.AutoSyncEnabled,
		"auto_sync_interval_seconds": item.AutoSyncIntervalSeconds,
		"sync_status":                item.SyncStatus,
		"discovered_page_count":      item.DiscoveredPageCount,
	}
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		done, err := projectionDone(ctx, tx, event.EventID)
		if err != nil || done {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO data_objects(object_id,project_id,object_type,source_module,source_id,title,summary,status,metadata,occurred_at,created_at,updated_at) VALUES($1,$2,'model_source','model',$3,$4,$5,$6,$7,$8,$9,$9) ON CONFLICT(source_module,object_type,source_id) DO UPDATE SET title=EXCLUDED.title,summary=EXCLUDED.summary,status=EXCLUDED.status,metadata=EXCLUDED.metadata,version=data_objects.version+1,occurred_at=EXCLUDED.occurred_at,updated_at=EXCLUDED.updated_at`, objectID, item.ProjectID, item.ID, title, item.SyncStatus, item.Status, jsonBytes(metadata), item.OccurredAt, store.Clock.Now().UTC()); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO data_activity(activity_id,project_id,object_id,event_id,activity_type,title,summary,actor,metadata,occurred_at,created_at) SELECT $1,$2,object_id,$3,$4,$5,$6,$7,$8,$9,$10 FROM data_objects WHERE source_module='model' AND object_type='model_source' AND source_id=$11 ON CONFLICT(event_id) WHERE event_id IS NOT NULL DO NOTHING`, activityID, item.ProjectID, event.EventID, event.EventType, title, item.Status, jsonBytes(event.Actor), jsonBytes(metadata), item.OccurredAt, store.Clock.Now().UTC(), item.ID)
		return err
	})
}

func (store PostgresStore) ProjectModelQuestion(ctx context.Context, event contract.EventEnvelope, item model.QuestionProjection) error {
	if event.ProjectID == nil || item.ProjectID != *event.ProjectID || item.ID == "" || item.Title == "" {
		return ErrInvalid
	}
	objectID, err := store.Generator.New()
	if err != nil {
		return err
	}
	activityID, err := store.Generator.New()
	if err != nil {
		return err
	}
	metadata := map[string]interface{}{"source_id": item.SourceID, "code": item.Code, "notion_page_id": item.NotionPageID}
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		done, err := projectionDone(ctx, tx, event.EventID)
		if err != nil || done {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO data_objects(object_id,project_id,object_type,source_module,source_id,title,summary,status,metadata,occurred_at,created_at,updated_at) VALUES($1,$2,'model_question','model',$3,$4,$5,$6,$7,$8,$9,$9) ON CONFLICT(source_module,object_type,source_id) DO UPDATE SET title=EXCLUDED.title,summary=EXCLUDED.summary,status=EXCLUDED.status,metadata=EXCLUDED.metadata,version=data_objects.version+1,occurred_at=EXCLUDED.occurred_at,updated_at=EXCLUDED.updated_at`, objectID, item.ProjectID, item.ID, item.Code+" · "+item.Title, item.Title, item.Status, jsonBytes(metadata), event.OccurredAt, store.Clock.Now().UTC()); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO data_activity(activity_id,project_id,object_id,event_id,activity_type,title,summary,actor,metadata,occurred_at,created_at) SELECT $1,$2,object_id,$3,$4,$5,$6,$7,$8,$9,$10 FROM data_objects WHERE source_module='model' AND object_type='model_question' AND source_id=$11 ON CONFLICT(event_id) WHERE event_id IS NOT NULL DO NOTHING`, activityID, item.ProjectID, event.EventID, event.EventType, item.Code+" · "+item.Title, item.Status, jsonBytes(event.Actor), jsonBytes(metadata), event.OccurredAt, store.Clock.Now().UTC(), item.ID)
		return err
	})
}

func (store PostgresStore) ProjectModelSnapshot(ctx context.Context, event contract.EventEnvelope, item model.SnapshotProjection) error {
	if event.ProjectID == nil || item.ProjectID != *event.ProjectID || item.ID == "" || item.QuestionID == "" {
		return ErrInvalid
	}
	objectID, err := store.Generator.New()
	if err != nil {
		return err
	}
	activityID, err := store.Generator.New()
	if err != nil {
		return err
	}
	metadata := map[string]interface{}{"question_id": item.QuestionID, "previous_snapshot_id": item.PreviousSnapshotID, "content_hash": item.ContentHash, "tags": nonNilStrings(item.Tags)}
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		done, err := projectionDone(ctx, tx, event.EventID)
		if err != nil || done {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO data_objects(object_id,project_id,object_type,source_module,source_id,title,summary,status,metadata,occurred_at,created_at,updated_at) VALUES($1,$2,'model_snapshot','model',$3,$4,$5,'active',$6,$7,$8,$8) ON CONFLICT(source_module,object_type,source_id) DO UPDATE SET title=EXCLUDED.title,summary=EXCLUDED.summary,status=EXCLUDED.status,metadata=EXCLUDED.metadata,version=data_objects.version+1,occurred_at=EXCLUDED.occurred_at,updated_at=EXCLUDED.updated_at`, objectID, item.ProjectID, item.ID, item.Title, item.Summary, jsonBytes(metadata), event.OccurredAt, store.Clock.Now().UTC()); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO data_activity(activity_id,project_id,object_id,event_id,activity_type,title,summary,actor,metadata,occurred_at,created_at) SELECT $1,$2,object_id,$3,$4,$5,$6,$7,$8,$9,$10 FROM data_objects WHERE source_module='model' AND object_type='model_snapshot' AND source_id=$11 ON CONFLICT(event_id) WHERE event_id IS NOT NULL DO NOTHING`, activityID, item.ProjectID, event.EventID, event.EventType, item.Title, item.Summary, jsonBytes(event.Actor), jsonBytes(metadata), event.OccurredAt, store.Clock.Now().UTC(), item.ID)
		return err
	})
}
