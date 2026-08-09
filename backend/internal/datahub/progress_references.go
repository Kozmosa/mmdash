package datahub

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// ValidateProgressReferences confirms that every requested stable object ID
// is visible in the same Project. Rows are locked for the Progress mutation so
// a concurrent hide cannot pass validation and win before that mutation.
func (store PostgresStore) ValidateProgressReferences(
	ctx context.Context,
	tx transaction.Tx,
	projectID string,
	objectIDs []string,
) (bool, error) {
	if len(objectIDs) == 0 {
		return true, nil
	}
	unique := make([]string, 0, len(objectIDs))
	seen := make(map[string]struct{}, len(objectIDs))
	for _, objectID := range objectIDs {
		if _, exists := seen[objectID]; exists {
			continue
		}
		seen[objectID] = struct{}{}
		unique = append(unique, objectID)
	}
	raw, err := json.Marshal(unique)
	if err != nil {
		return false, fmt.Errorf("encode Progress object references: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT object_id
		FROM data_objects
		WHERE project_id = $1
		  AND status <> 'hidden'
		  AND object_id IN (
		      SELECT value::uuid
		      FROM jsonb_array_elements_text($2::jsonb)
		  )
		FOR SHARE
	`, projectID, raw)
	if err != nil {
		return false, fmt.Errorf("validate Progress object references: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("scan Progress object references: %w", err)
	}
	return count == len(unique), nil
}
