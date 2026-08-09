package datahub

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
)

// BuildEvaluationFacts returns a bounded, versioned Project Data Hub snapshot
// for automatic Progress evaluation. Volatile timestamps are not introduced,
// so identical authoritative inputs retain the same idempotency hash.
func (store PostgresStore) BuildEvaluationFacts(ctx context.Context, projectID, _ string) (map[string]interface{}, error) {
	var name, title, summary string
	var constraints, sources []byte
	err := store.DB.QueryRowContext(ctx, `
		SELECT name,problem_title,problem_summary,project_constraints,source_artifact_ids
		FROM projects WHERE project_id=$1 AND deleted_at IS NULL
	`, projectID).Scan(&name, &title, &summary, &constraints, &sources)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	page := pagination.Request{Limit: 200}
	objects, err := store.ListObjects(ctx, projectID, "", page)
	if err != nil {
		return nil, err
	}
	activity, err := store.ListActivity(ctx, projectID, page)
	if err != nil {
		return nil, err
	}
	contextEntries, err := store.ListContext(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"facts_schema_version": 1,
		"project": map[string]interface{}{
			"project_id": projectID, "name": name, "problem_title": title,
			"problem_summary": summary, "project_constraints": rawJSON(constraints),
			"source_artifact_ids": rawJSON(sources),
		},
		"data_objects":      stableEvaluationObjects(objects.Items),
		"recent_activity":   stableEvaluationActivity(activity.Items),
		"confirmed_context": contextEntries,
	}, nil
}

func stableEvaluationObjects(items []Object) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if item.ObjectType == "progress_evaluation" || item.ObjectType == "progress_risk" {
			continue
		}
		result = append(result, map[string]interface{}{
			"object_id": item.ID, "object_type": item.ObjectType,
			"source_module": item.SourceModule, "source_id": item.SourceID,
			"title": item.Title, "summary": item.Summary, "status": item.Status,
			"version": item.Version, "metadata": item.Metadata,
		})
	}
	return result
}

func stableEvaluationActivity(items []Activity) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(item.ActivityType, "progress.evaluation.") || strings.HasPrefix(item.ActivityType, "progress.risk.") {
			continue
		}
		result = append(result, map[string]interface{}{
			"activity_id": item.ID, "activity_type": item.ActivityType,
			"object_id": item.ObjectID, "title": item.Title, "summary": item.Summary,
			"actor": item.Actor, "metadata": item.Metadata,
		})
	}
	return result
}

func rawJSON(value []byte) interface{} {
	if len(value) == 0 {
		return []interface{}{}
	}
	var decoded interface{}
	if err := json.Unmarshal(value, &decoded); err != nil {
		return []interface{}{}
	}
	return decoded
}
