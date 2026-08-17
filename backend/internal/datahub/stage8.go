package datahub

import (
	"context"
	"fmt"
	"strings"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// ProjectStage8 is the idempotent Data Hub projection for the Experiment and
// Box boundaries. The source modules remain authoritative; Data Hub stores
// only searchable cards and event activity, while data.read uses adapters back
// to those modules for complete, permission-checked records.
func (store PostgresStore) ProjectStage8(ctx context.Context, event contract.EventEnvelope) error {
	if event.ProjectID == nil || strings.TrimSpace(*event.ProjectID) == "" {
		return ErrInvalid
	}
	objects := stage8Objects(event)
	if len(objects) == 0 {
		return ErrInvalid
	}
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		done, err := projectionDone(ctx, tx, event.EventID)
		if err != nil || done {
			return err
		}
		for _, object := range objects {
			objectID, err := store.Generator.New()
			if err != nil {
				return err
			}
			activityID, err := store.Generator.New()
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO data_objects(object_id,project_id,object_type,source_module,source_id,title,summary,status,metadata,occurred_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11) ON CONFLICT(source_module,object_type,source_id) DO UPDATE SET title=EXCLUDED.title,summary=EXCLUDED.summary,status=EXCLUDED.status,metadata=EXCLUDED.metadata,version=data_objects.version+1,occurred_at=EXCLUDED.occurred_at,updated_at=EXCLUDED.updated_at`, objectID, *event.ProjectID, object.objectType, object.sourceModule, object.sourceID, object.title, object.summary, object.status, jsonBytes(object.metadata), event.OccurredAt, store.Clock.Now().UTC()); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO data_activity(activity_id,project_id,object_id,event_id,activity_type,title,summary,actor,metadata,occurred_at,created_at) SELECT $1,$2,object_id,$3,$4,$5,$6,$7,$8,$9,$10 FROM data_objects WHERE source_module=$11 AND object_type=$12 AND source_id=$13 ON CONFLICT(event_id) WHERE event_id IS NOT NULL DO NOTHING`, activityID, *event.ProjectID, event.EventID, event.EventType, object.title, object.summary, jsonBytes(event.Actor), jsonBytes(object.metadata), event.OccurredAt, store.Clock.Now().UTC(), object.sourceModule, object.objectType, object.sourceID); err != nil {
				return err
			}
			if object.delete {
				if _, err := tx.ExecContext(ctx, `DELETE FROM data_objects WHERE source_module=$1 AND object_type=$2 AND source_id=$3 AND project_id=$4`, object.sourceModule, object.objectType, object.sourceID, *event.ProjectID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

type stage8ProjectionObject struct {
	objectType, sourceModule, sourceID, title, summary, status string
	metadata                                                   map[string]interface{}
	delete                                                     bool
}

func stage8Objects(event contract.EventEnvelope) []stage8ProjectionObject {
	payload := event.Payload
	status, _ := payload["execution_status"].(string)
	if status == "" {
		status, _ = payload["status"].(string)
	}
	metadata := map[string]interface{}{}
	for key, value := range payload {
		if key != "name" && key != "summary" {
			metadata[key] = value
		}
	}
	switch {
	case strings.HasPrefix(event.EventType, "experiment."):
		experimentID, _ := payload["experiment_id"].(string)
		if status == "" {
			switch event.EventType {
			case "experiment.rerun_created":
				status = "created"
			case "experiment.result_bound":
				status = "succeeded"
			}
		}
		if experimentID == "" || status == "" {
			return nil
		}
		name, _ := payload["name"].(string)
		if name == "" {
			name = fmt.Sprintf("Experiment %s", experimentID[:minInt(8, len(experimentID))])
		}
		objects := []stage8ProjectionObject{{objectType: "experiment", sourceModule: "experiment", sourceID: experimentID, title: name, summary: status, status: status, metadata: metadata}}
		if taskID, ok := payload["task_id"].(string); ok && taskID != "" {
			runMetadata := cloneMetadata(metadata)
			runMetadata["experiment_id"] = experimentID
			objects = append(objects, stage8ProjectionObject{objectType: "experiment_run", sourceModule: "experiment", sourceID: taskID, title: name + " run", summary: status, status: status, metadata: runMetadata})
		}
		if event.EventType == "experiment.succeeded" {
			resultMetadata := cloneMetadata(metadata)
			resultMetadata["experiment_id"] = experimentID
			objects = append(objects, stage8ProjectionObject{objectType: "result_bundle", sourceModule: "experiment", sourceID: experimentID, title: name + " result", summary: status, status: status, metadata: resultMetadata})
		}
		return objects
	case strings.HasPrefix(event.EventType, "box."):
		boxID, _ := payload["box_id"].(string)
		switch event.EventType {
		case "box.assigned":
			status = "assigned"
		case "box.unassigned":
			status = "unassigned"
		case "box.offline":
			status = "offline"
		case "box.recovered":
			status = "online"
		}
		if boxID == "" || status == "" || event.ProjectID == nil {
			return nil
		}
		name, _ := payload["name"].(string)
		if name == "" {
			name = fmt.Sprintf("Box %s", boxID[:minInt(8, len(boxID))])
		}
		metadata["box_id"] = boxID
		return []stage8ProjectionObject{{
			objectType: "box", sourceModule: "boxcontrol",
			sourceID: boxID + "@" + *event.ProjectID,
			title:    name, summary: status, status: status, metadata: metadata,
			delete: event.EventType == "box.unassigned",
		}}
	default:
		return nil
	}
}

func cloneMetadata(value map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{}, len(value))
	for key, item := range value {
		copy[key] = item
	}
	return copy
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
