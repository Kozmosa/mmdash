package repo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func (store PostgresStore) RecordWebhook(
	ctx context.Context,
	delivery WebhookDelivery,
) (duplicate bool, err error) {
	if delivery.RepositoryID == "" ||
		delivery.DeliveryID == "" ||
		delivery.Event == "" ||
		delivery.PayloadSHA == "" ||
		(delivery.Status != "accepted" &&
			delivery.Status != "ignored" &&
			delivery.Status != "processed") {
		return false, ErrInvalid
	}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO repo_webhook_deliveries (
				provider, delivery_id, repository_id, event_name,
				ref_name, before_sha, after_sha, payload_sha256,
				status, received_at, processed_at
			) VALUES (
				'github', $1, $2, $3, $4, $5, $6, $7, $8, $9,
				CASE WHEN $8 IN ('ignored', 'processed') THEN $9 ELSE NULL END
			)
			ON CONFLICT (provider, delivery_id) DO NOTHING
		`, delivery.DeliveryID, delivery.RepositoryID, delivery.Event,
			delivery.Ref, delivery.BeforeSHA, delivery.AfterSHA,
			delivery.PayloadSHA, delivery.Status, delivery.ReceivedAt.UTC())
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			var payloadSHA, repositoryID string
			if err := tx.QueryRowContext(ctx, `
				SELECT payload_sha256, repository_id
				FROM repo_webhook_deliveries
				WHERE provider = 'github' AND delivery_id = $1
			`, delivery.DeliveryID).Scan(
				&payloadSHA, &repositoryID,
			); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrWebhookConflict
				}
				return err
			}
			if payloadSHA != delivery.PayloadSHA ||
				repositoryID != delivery.RepositoryID {
				return ErrWebhookConflict
			}
			duplicate = true
			return nil
		}
		if !delivery.RequestSync {
			return nil
		}
		if delivery.Workspace == nil || !validWorkspaceKind(*delivery.Workspace) {
			return ErrInvalid
		}
		result, err = tx.ExecContext(ctx, `
			UPDATE repo_repositories
			SET sync_workspace_kinds = CASE
			      WHEN sync_requested_at IS NULL THEN ARRAY[$3]::TEXT[]
			      ELSE ARRAY(
			        SELECT DISTINCT value
			        FROM unnest(sync_workspace_kinds || ARRAY[$3]::TEXT[]) AS value
			        ORDER BY value
			      )
			    END,
			    sync_requested_at = $2,
			    sync_source = 'webhook',
			    next_sync_at = LEAST(COALESCE(next_sync_at, $2), $2),
			    updated_at = $2
			WHERE repository_id = $1 AND status <> 'disconnected'
		`, delivery.RepositoryID, delivery.ReceivedAt.UTC(), *delivery.Workspace)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		return nil
	})
	return duplicate, err
}

var _ WebhookStore = PostgresStore{}
