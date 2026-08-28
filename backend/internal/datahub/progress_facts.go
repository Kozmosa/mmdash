package datahub

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
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
	projectFacts := map[string]interface{}{
		"project_id": projectID, "name": name, "problem_title": title,
		"problem_summary": summary, "project_constraints": rawJSON(constraints),
		"source_artifact_ids": rawJSON(sources),
	}
	return evaluationEvidenceSeed(
		projectID,
		projectFacts,
		stableEvaluationObjects(objects.Items),
		stableEvaluationActivity(activity.Items),
		stableEvaluationContext(contextEntries),
		objects.HasMore || activity.HasMore,
	)
}

// evaluationEvidenceSeed keeps automatic evaluation input semantic and
// versionable without copying the Project's full working set into the Agent
// Run. The Agent discovers current content through audited MCP reads.
func evaluationEvidenceSeed(
	projectID string,
	projectFacts map[string]interface{},
	objects []map[string]interface{},
	activity []map[string]interface{},
	contextEntries []map[string]interface{},
	catalogTruncated bool,
) (map[string]interface{}, error) {
	semanticEvidence := map[string]interface{}{
		"project": projectFacts, "data_objects": objects,
		"recent_activity": activity, "confirmed_context": contextEntries,
	}
	encoded, err := json.Marshal(semanticEvidence)
	if err != nil {
		return nil, fmt.Errorf("encode Progress evidence revision: %w", err)
	}
	revision := fmt.Sprintf("%x", sha256.Sum256(encoded))
	counts := map[string]int{}
	for _, object := range objects {
		objectType, _ := object["object_type"].(string)
		if objectType != "" {
			counts[objectType]++
		}
	}
	types := make([]string, 0, len(counts))
	for objectType := range counts {
		types = append(types, objectType)
	}
	sort.Strings(types)
	return map[string]interface{}{
		"facts_schema_version": 2,
		"project":              map[string]interface{}{"project_id": projectID},
		"evidence_catalog": map[string]interface{}{
			"revision": revision, "available_object_types": types,
			"object_type_counts": counts, "catalog_truncated": catalogTruncated,
		},
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

func stableEvaluationContext(items []ContextEntry) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]interface{}{
			"context_id": item.ID, "title": item.Title, "content": item.Content,
			"context_type":      item.ContextType,
			"source_object_ids": append([]string{}, item.SourceObjectIDs...),
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
