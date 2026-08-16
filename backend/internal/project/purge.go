package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type purgeObjectStore interface {
	AbortMultipart(context.Context, string, string) error
	Delete(context.Context, string) error
}

type purgeRepoStorage interface {
	RemoveRepository(string) error
}

// Stage8Purger removes mmdash-owned external bytes before deleting a Project.
// It never calls a Git provider or changes a user's remote repository.
type Stage8Purger struct {
	Artifacts    purgeObjectStore
	Clock        interface{ Now() time.Time }
	DB           *sql.DB
	Interval     time.Duration
	Lease        time.Duration
	OnError      func(error)
	Repositories purgeRepoStorage
}

func (purger Stage8Purger) Run(ctx context.Context) {
	interval := purger.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			for index := 0; index < 10; index++ {
				processed, err := purger.RunOnce(ctx)
				if err != nil {
					if purger.OnError != nil && !errors.Is(err, context.Canceled) {
						purger.OnError(err)
					}
					break
				}
				if !processed {
					break
				}
			}
			timer.Reset(interval)
		}
	}
}

func (purger Stage8Purger) RunOnce(ctx context.Context) (bool, error) {
	if purger.DB == nil || purger.Artifacts == nil || purger.Repositories == nil {
		return false, ErrInvalid
	}
	now := time.Now().UTC()
	if purger.Clock != nil {
		now = purger.Clock.Now().UTC()
	}
	lease := purger.Lease
	if lease <= 0 {
		lease = 10 * time.Minute
	}
	projectID, err := purger.claim(ctx, now, lease)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := purger.removeExternalState(ctx, projectID); err != nil {
		_ = purger.fail(ctx, projectID, now)
		return true, err
	}
	if err := purger.complete(ctx, projectID, now); err != nil {
		_ = purger.fail(ctx, projectID, now)
		return true, err
	}
	return true, nil
}

func (purger Stage8Purger) claim(ctx context.Context, now time.Time, lease time.Duration) (string, error) {
	var projectID string
	err := purger.DB.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT purge.project_id
			FROM project_stage8_purges AS purge
			JOIN projects AS project USING (project_id)
			WHERE project.deleted_at IS NOT NULL AND project.purge_at <= $1
			  AND purge.requested_at <= $1
			  AND (purge.status IN ('pending','failed')
			       OR (purge.status='running' AND purge.updated_at <= $2))
			ORDER BY purge.requested_at,purge.project_id
			FOR UPDATE OF purge SKIP LOCKED LIMIT 1
		)
		UPDATE project_stage8_purges AS purge
		SET status='running',attempts=attempts+1,started_at=COALESCE(started_at,$1),
			last_error_code=NULL,last_error_message=NULL,updated_at=$1
		FROM candidate WHERE purge.project_id=candidate.project_id
		RETURNING purge.project_id
	`, now, now.Add(-lease)).Scan(&projectID)
	return projectID, err
}

func (purger Stage8Purger) removeExternalState(ctx context.Context, projectID string) error {
	uploads, err := purger.DB.QueryContext(ctx, `
		SELECT staging_key,provider_upload_id FROM artifact_uploads
		WHERE project_id=$1
		  AND status IN ('initialized','uploading','completing','verifying')
		  AND provider_upload_id NOT LIKE 'deduplicated:%'
		  AND provider_upload_id NOT LIKE 'restored:%'
		  AND provider_upload_id NOT LIKE 'aborted:%'
		UNION ALL
		SELECT staging_key,provider_upload_id FROM artifact_preview_transfers
		WHERE project_id=$1 AND status IN ('prepared','uploaded')
	`, projectID)
	if err != nil {
		return fmt.Errorf("list Project multipart uploads: %w", err)
	}
	type multipart struct{ key, id string }
	multiparts := []multipart{}
	for uploads.Next() {
		var item multipart
		if err := uploads.Scan(&item.key, &item.id); err != nil {
			uploads.Close()
			return err
		}
		multiparts = append(multiparts, item)
	}
	if err := uploads.Close(); err != nil {
		return err
	}
	for _, item := range multiparts {
		if err := purger.Artifacts.AbortMultipart(ctx, item.key, item.id); err != nil {
			return fmt.Errorf("abort Project multipart upload: %w", err)
		}
	}

	rows, err := purger.DB.QueryContext(ctx, `
		SELECT object_key FROM artifact_blobs WHERE project_id=$1
		UNION SELECT staging_key FROM artifact_uploads WHERE project_id=$1
		UNION SELECT staging_key FROM artifact_preview_transfers WHERE project_id=$1
	`, projectID)
	if err != nil {
		return fmt.Errorf("list Project Artifact objects: %w", err)
	}
	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, key := range keys {
		if err := purger.Artifacts.Delete(ctx, key); err != nil {
			return fmt.Errorf("delete Project Artifact object: %w", err)
		}
	}
	repositories, err := purger.DB.QueryContext(ctx, `SELECT storage_key FROM repo_repositories WHERE project_id=$1`, projectID)
	if err != nil {
		return fmt.Errorf("list Project Repo storage: %w", err)
	}
	storageKeys := []string{}
	for repositories.Next() {
		var key string
		if err := repositories.Scan(&key); err != nil {
			repositories.Close()
			return err
		}
		storageKeys = append(storageKeys, key)
	}
	if err := repositories.Close(); err != nil {
		return err
	}
	for _, key := range storageKeys {
		if err := purger.Repositories.RemoveRepository(key); err != nil {
			return fmt.Errorf("delete managed Project Repo storage: %w", err)
		}
	}
	return nil
}

func (purger Stage8Purger) complete(ctx context.Context, projectID string, now time.Time) error {
	tx, err := purger.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE project_id=$1 AND deleted_at IS NOT NULL AND purge_at <= $2`, projectID, now); err != nil {
		return fmt.Errorf("delete expired Project data: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE project_stage8_purges
		SET status='completed',completed_at=$2,updated_at=$2,cursor='{"phase":"completed"}'::jsonb
		WHERE project_id=$1 AND status='running'
	`, projectID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (purger Stage8Purger) fail(ctx context.Context, projectID string, now time.Time) error {
	_, err := purger.DB.ExecContext(ctx, `
		UPDATE project_stage8_purges
		SET status='failed',last_error_code='PROJECT_PURGE_FAILED',
			last_error_message='Project cleanup will be retried',updated_at=$2
		WHERE project_id=$1
	`, projectID, now)
	return err
}
