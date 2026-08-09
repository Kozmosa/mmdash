package datahub

import (
	"context"
	"strings"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// ProjectProgress projects Progress lifecycle events into the shared Data Hub.
// The event payload contains only bounded projection metadata; full records
// remain readable through the Progress adapter.
func (store PostgresStore) ProjectProgress(ctx context.Context, event contract.EventEnvelope) error {
	if event.ProjectID == nil {
		return ErrInvalid
	}
	resourceID, _ := event.Payload["resource_id"].(string)
	resourceType, _ := event.Payload["resource_type"].(string)
	title, _ := event.Payload["title"].(string)
	status, _ := event.Payload["status"].(string)
	if strings.TrimSpace(resourceID) == "" || strings.TrimSpace(resourceType) == "" || strings.TrimSpace(title) == "" {
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
	projectID := *event.ProjectID
	metadata := jsonBytes(event.Payload)
	actor := projectionActor(event.Actor)
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		done, err := projectionDone(ctx, tx, event.EventID)
		if err != nil || done {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO data_objects(object_id,project_id,object_type,source_module,source_id,title,summary,status,metadata,occurred_at,created_at,updated_at) VALUES($1,$2,$3,'progress',$4,$5,$6,$7,$8,$9,$9,$9) ON CONFLICT(source_module,object_type,source_id) DO UPDATE SET title=EXCLUDED.title,summary=EXCLUDED.summary,status=EXCLUDED.status,metadata=EXCLUDED.metadata,version=data_objects.version+1,occurred_at=EXCLUDED.occurred_at,updated_at=EXCLUDED.updated_at`, objectID, projectID, resourceType, resourceID, title, event.EventType, status, metadata, event.OccurredAt); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO data_activity(activity_id,project_id,object_id,event_id,activity_type,title,summary,actor,metadata,occurred_at,created_at) SELECT $1,$2,object_id,$3,$4,$5,$6,$7,$8,$9,$10 FROM data_objects WHERE source_module='progress' AND object_type=$11 AND source_id=$12 ON CONFLICT(event_id) WHERE event_id IS NOT NULL DO NOTHING`, activityID, projectID, event.EventID, event.EventType, title, status, actor, metadata, event.OccurredAt, store.Clock.Now().UTC(), resourceType, resourceID)
		return err
	})
}
