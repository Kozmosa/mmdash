package artifact

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func (store PostgresStore) CompleteSemanticDescriptionInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job, result SemanticDescriptionResult, now time.Time) error {
	target, err := semanticTarget(job)
	if err != nil {
		return err
	}
	usage, err := json.Marshal(result.RecommendedUsage)
	if err != nil {
		return jobs.ErrInvalid
	}
	command, err := tx.ExecContext(ctx, `
		UPDATE artifact_artifacts
		SET description=$4,recommended_usage=$5::jsonb,updated_at=$6
		WHERE project_id=$1 AND artifact_id=$2 AND current_version_id=$3 AND status='available'
	`, target.ProjectID, target.ArtifactID, target.VersionID, result.Description, usage, now)
	if err != nil {
		return err
	}
	rows, err := command.RowsAffected()
	if err != nil || rows != 1 {
		return jobs.ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE artifact_registry_entries
		SET description=$3,recommended_usage=$4::jsonb,updated_at=$5
		WHERE project_id=$1 AND artifact_id=$2
	`, target.ProjectID, target.ArtifactID, result.Description, usage, now)
	return err
}
