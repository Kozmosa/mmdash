package artifact

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgconn"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PostgresStore persists Artifact business state.
type PostgresStore struct {
	Audit       TransactionalAuditRecorder
	DB          *sql.DB
	Generator   identity.Generator
	Jobs        jobs.TransactionalWriter
	Outbox      *outbox.Writer
	Transaction transaction.Manager
}

func (store PostgresStore) CreateFirst(
	ctx context.Context,
	artifact Artifact,
	version Version,
	upload UploadSession,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := insertArtifact(ctx, tx, artifact); err != nil {
			return mapPostgresError(err)
		}
		if err := insertVersion(ctx, tx, version); err != nil {
			return mapPostgresError(err)
		}
		if err := insertUpload(ctx, tx, upload); err != nil {
			return mapPostgresError(err)
		}
		if artifact.Source == SourceModel {
			if artifact.SourceObjectID == nil {
				return ErrSourceInvalid
			}
			relationID, err := store.Generator.New()
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO artifact_relations(
					relation_id,project_id,artifact_id,version_id,
					relation_type,target_type,target_id,created_by,created_at
				) VALUES($1,$2,$3,$4,'attachment','model',$5,$6,$7)
			`, relationID, artifact.ProjectID, artifact.ID, version.ID,
				*artifact.SourceObjectID, artifact.CreatedBy, artifact.CreatedAt); err != nil {
				return mapPostgresError(err)
			}
		}
		if version.Status == StatusAvailable {
			if err := store.schedulePreviewInTransaction(
				ctx, tx, artifact.ProjectID, artifact.ID, version.ID,
				artifact.CreatedBy, version.CreatedAt,
			); err != nil {
				return err
			}
		}
		if err := store.artifactCreated(ctx, tx, artifact, version); err != nil {
			return err
		}
		if version.Status == StatusAvailable {
			if err := store.artifactAvailable(
				ctx, tx, artifact.ProjectID, artifact.ID, version,
				"deduplicated",
			); err != nil {
				return err
			}
		}
		return store.audit(
			ctx, tx, "artifact.upload.initialized", artifact.ProjectID,
			artifact.ID, map[string]interface{}{
				"deduplicated": upload.Status == UploadCompleted,
				"size_bytes":   upload.ExpectedSize,
				"version_id":   version.ID,
			},
		)
	})
}

func (store PostgresStore) CreateGit(
	ctx context.Context,
	artifact Artifact,
	version Version,
	relationID string,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := insertArtifact(ctx, tx, artifact); err != nil {
			return mapPostgresError(err)
		}
		if err := insertVersion(ctx, tx, version); err != nil {
			return mapPostgresError(err)
		}
		if err := store.schedulePreviewInTransaction(
			ctx, tx, artifact.ProjectID, artifact.ID, version.ID,
			artifact.CreatedBy, version.CreatedAt,
		); err != nil {
			return err
		}
		if err := store.artifactCreated(ctx, tx, artifact, version); err != nil {
			return err
		}
		if err := store.artifactAvailable(
			ctx, tx, artifact.ProjectID, artifact.ID, version, "uploaded",
		); err != nil {
			return err
		}
		if artifact.Source == SourceSystem {
			return store.audit(
				ctx, tx, "artifact.git.registered", artifact.ProjectID,
				artifact.ID, map[string]interface{}{"version_id": version.ID},
			)
		}
		if artifact.SourceObjectID == nil {
			return ErrSourceInvalid
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO artifact_relations(
				relation_id,project_id,artifact_id,version_id,
				relation_type,target_type,target_id,created_by,created_at
			) VALUES($1,$2,$3,$4,'output',$5,$6,$7,$8)
		`, relationID, artifact.ProjectID, artifact.ID, version.ID,
			artifact.Source, *artifact.SourceObjectID, artifact.CreatedBy,
			artifact.CreatedAt)
		if err := mapPostgresError(err); err != nil {
			return err
		}
		return store.audit(
			ctx, tx, "artifact.git.registered", artifact.ProjectID,
			artifact.ID, map[string]interface{}{"version_id": version.ID},
		)
	})
}

func (store PostgresStore) CreateVersion(
	ctx context.Context,
	projectID string,
	artifactID string,
	version Version,
	upload UploadSession,
) (UploadSession, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `
			SELECT status
			FROM artifact_artifacts
			WHERE project_id=$1 AND artifact_id=$2
			FOR UPDATE
		`, projectID, artifactID).Scan(&status); err != nil {
			return mapNotFound(err)
		}
		if status != StatusAvailable {
			return ErrNotAvailable
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(version_no), 0) + 1
			FROM artifact_versions
			WHERE project_id=$1 AND artifact_id=$2
		`, projectID, artifactID).Scan(&version.VersionNo); err != nil {
			return err
		}
		upload.VersionNo = version.VersionNo
		if err := insertVersion(ctx, tx, version); err != nil {
			return mapPostgresError(err)
		}
		if err := insertUpload(ctx, tx, upload); err != nil {
			return mapPostgresError(err)
		}
		if version.Status == StatusAvailable {
			result, err := tx.ExecContext(ctx, `
				UPDATE artifact_artifacts
				SET current_version_id=$3, status='available', updated_at=$4
				WHERE project_id=$1 AND artifact_id=$2 AND status='available'
			`, projectID, artifactID, version.ID, version.AvailableAt)
			if err := requireAffected(result, err); err != nil {
				return err
			}
			if err := store.schedulePreviewInTransaction(
				ctx, tx, projectID, artifactID, version.ID,
				version.CreatedBy, version.CreatedAt,
			); err != nil {
				return err
			}
			if err := store.artifactAvailable(
				ctx, tx, projectID, artifactID, version, "deduplicated",
			); err != nil {
				return err
			}
		}
		return store.audit(
			ctx, tx, "artifact.version.upload.initialized", projectID,
			artifactID, map[string]interface{}{
				"deduplicated": upload.Status == UploadCompleted,
				"size_bytes":   upload.ExpectedSize,
				"version_id":   version.ID,
			},
		)
	})
	return upload, err
}

func (store PostgresStore) FindBlob(
	ctx context.Context,
	projectID string,
	sha256 string,
	sizeBytes int64,
) (Blob, error) {
	var blob Blob
	err := store.DB.QueryRowContext(ctx, `
		SELECT blob_id, project_id, sha256, size_bytes, backend,
		       object_key, reference_count
		FROM artifact_blobs
		WHERE project_id=$1 AND sha256=$2 AND size_bytes=$3
	`, projectID, sha256, sizeBytes).Scan(
		&blob.ID, &blob.ProjectID, &blob.SHA256, &blob.SizeBytes,
		&blob.Backend, &blob.ObjectKey, &blob.ReferenceCount,
	)
	return blob, mapNotFound(err)
}

func (store PostgresStore) GetArtifact(
	ctx context.Context,
	projectID string,
	artifactID string,
) (Artifact, error) {
	return scanArtifact(store.DB.QueryRowContext(ctx, artifactSelect+`
		WHERE artifact.project_id=$1 AND artifact.artifact_id=$2
	`, projectID, artifactID).Scan)
}

func (store PostgresStore) GetDetail(
	ctx context.Context,
	projectID string,
	artifactID string,
	includeTrashed bool,
) (Detail, error) {
	artifact, err := store.GetArtifact(ctx, projectID, artifactID)
	if err != nil {
		return Detail{}, err
	}
	if artifact.Status == StatusTrashed && !includeTrashed {
		return Detail{}, ErrNotFound
	}
	if artifact.CurrentVersionID == nil {
		return Detail{Artifact: artifact}, nil
	}
	version, err := store.GetVersion(ctx, projectID, artifactID, *artifact.CurrentVersionID)
	if err != nil {
		return Detail{}, err
	}
	return Detail{Artifact: artifact, CurrentVersion: &version}, nil
}

func (store PostgresStore) GetUpload(
	ctx context.Context,
	projectID string,
	uploadID string,
) (UploadSession, error) {
	upload, err := scanUpload(store.DB.QueryRowContext(ctx, uploadSelect+`
		WHERE upload.project_id=$1 AND upload.upload_id=$2
	`, projectID, uploadID).Scan)
	if err != nil {
		return UploadSession{}, err
	}
	parts, err := store.listParts(ctx, upload.ID)
	if err != nil {
		return UploadSession{}, err
	}
	upload.Parts = parts
	return upload, nil
}

func (store PostgresStore) GetUploadByIdempotency(
	ctx context.Context,
	projectID string,
	idempotencyKey string,
) (UploadSession, error) {
	upload, err := scanUpload(store.DB.QueryRowContext(ctx, uploadSelect+`
		WHERE upload.project_id=$1 AND upload.idempotency_key=$2
	`, projectID, idempotencyKey).Scan)
	if err != nil {
		return UploadSession{}, err
	}
	parts, err := store.listParts(ctx, upload.ID)
	if err != nil {
		return UploadSession{}, err
	}
	upload.Parts = parts
	return upload, nil
}

func (store PostgresStore) GetVersion(
	ctx context.Context,
	projectID string,
	artifactID string,
	versionID string,
) (Version, error) {
	return scanVersion(store.DB.QueryRowContext(ctx, versionSelect+`
		WHERE version.project_id=$1
		  AND version.artifact_id=$2
		  AND version.version_id=$3
	`, projectID, artifactID, versionID).Scan)
}

func (store PostgresStore) List(
	ctx context.Context,
	projectID string,
	filter ListFilter,
) (Page, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return Page{}, ErrInvalid
	}
	cursorTime, cursorID, err := decodeArtifactCursor(filter.Cursor)
	if err != nil {
		return Page{}, ErrInvalid
	}
	rows, err := store.DB.QueryContext(ctx, `
		SELECT artifact_id, updated_at
		FROM artifact_artifacts
		WHERE project_id=$1
		  AND (($2 AND status='trashed') OR (NOT $2 AND status<>'trashed'))
		  AND ($3='' OR kind=$3)
		  AND ($4='' OR source=$4)
		  AND ($5='' OR status=$5)
		  AND ($6='' OR $6=ANY(tags))
		  AND (
		    NULLIF($7, '') IS NULL
		    OR (updated_at, artifact_id) <
		       (NULLIF($7, '')::timestamptz, NULLIF($8, '')::uuid)
		  )
		ORDER BY updated_at DESC, artifact_id DESC
		LIMIT $9
	`, projectID, filter.Trash, filter.Kind, filter.Source, filter.Status,
		filter.Tag, cursorTime, cursorID, limit+1)
	if err != nil {
		return Page{}, wrap("list artifacts", err)
	}
	defer rows.Close()
	type listed struct {
		id        string
		updatedAt time.Time
	}
	values := make([]listed, 0, limit+1)
	for rows.Next() {
		var value listed
		if err := rows.Scan(&value.id, &value.updatedAt); err != nil {
			return Page{}, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	hasMore := len(values) > limit
	if hasMore {
		values = values[:limit]
	}
	items := make([]Detail, 0, len(values))
	for _, value := range values {
		detail, err := store.GetDetail(ctx, projectID, value.id, filter.Trash)
		if err != nil {
			return Page{}, err
		}
		items = append(items, detail)
	}
	var nextCursor *string
	if hasMore && len(values) > 0 {
		last := values[len(values)-1]
		encoded, err := pagination.Encode(pagination.Cursor{
			ID: last.id, SortValue: last.updatedAt.Format(time.RFC3339Nano),
		})
		if err != nil {
			return Page{}, err
		}
		nextCursor = &encoded
	}
	return Page{Items: items, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func (store PostgresStore) ListVersions(
	ctx context.Context,
	projectID string,
	artifactID string,
) (VersionList, error) {
	rows, err := store.DB.QueryContext(ctx, versionSelect+`
		WHERE version.project_id=$1 AND version.artifact_id=$2
		ORDER BY version.version_no DESC
	`, projectID, artifactID)
	if err != nil {
		return VersionList{}, err
	}
	defer rows.Close()
	items := []Version{}
	for rows.Next() {
		version, err := scanVersion(rows.Scan)
		if err != nil {
			return VersionList{}, err
		}
		items = append(items, version)
	}
	if err := rows.Err(); err != nil {
		return VersionList{}, err
	}
	if len(items) == 0 {
		return VersionList{}, ErrNotFound
	}
	return VersionList{Items: items}, nil
}

func (store PostgresStore) Update(
	ctx context.Context,
	projectID string,
	artifactID string,
	input UpdateInput,
	now time.Time,
) (Detail, error) {
	var tags interface{}
	if input.Tags != nil {
		encoded, _ := json.Marshal(*input.Tags)
		tags = encoded
	}
	descriptionSet := input.Description != nil
	var description interface{}
	if descriptionSet && *input.Description != nil {
		description = **input.Description
	}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE artifact_artifacts
			SET name=COALESCE($3, name),
			    kind=COALESCE($4, kind),
			    tags=CASE WHEN $5::jsonb IS NULL THEN tags
			      ELSE ARRAY(SELECT jsonb_array_elements_text($5::jsonb)) END,
			    description=CASE WHEN $6 THEN $7::text ELSE description END,
			    updated_at=$8
			WHERE project_id=$1 AND artifact_id=$2 AND status<>'trashed'
		`, projectID, artifactID, input.Name, input.Kind, tags,
			descriptionSet, description, now)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE artifact_registry_entries AS registry
			SET description=artifact.description,updated_at=$3
			FROM artifact_artifacts AS artifact
			WHERE registry.project_id=$1 AND registry.artifact_id=$2
			  AND artifact.project_id=registry.project_id
			  AND artifact.artifact_id=registry.artifact_id
		`, projectID, artifactID, now); err != nil {
			return err
		}
		return store.audit(
			ctx, tx, "artifact.updated", projectID, artifactID,
			map[string]interface{}{},
		)
	})
	if err != nil {
		return Detail{}, mapPostgresError(err)
	}
	return store.GetDetail(ctx, projectID, artifactID, false)
}

func (store PostgresStore) MarkUploading(
	ctx context.Context,
	uploadID string,
	now time.Time,
) error {
	_, err := store.DB.ExecContext(ctx, `
		UPDATE artifact_uploads
		SET status='uploading', updated_at=$2
		WHERE upload_id=$1 AND status='initialized'
	`, uploadID, now)
	return err
}

func (store PostgresStore) BeginConfirm(
	ctx context.Context,
	uploadID string,
	now time.Time,
	recoveryBefore time.Time,
) (bool, error) {
	result, err := store.DB.ExecContext(ctx, `
		UPDATE artifact_uploads
		SET status='completing', updated_at=$2
		WHERE upload_id=$1
		  AND (
		    status IN ('initialized','uploading')
		    OR (
		      status IN ('completing','verifying')
		      AND updated_at<=$3
		    )
		  )
	`, uploadID, now, recoveryBefore)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (store PostgresStore) SetUploadStatus(
	ctx context.Context,
	uploadID string,
	status string,
	errorCode string,
	now time.Time,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var artifactID, projectID, versionID string
		if err := tx.QueryRowContext(ctx, `
			UPDATE artifact_uploads
			SET status=$2,
			    error_code=NULLIF($3, ''),
			    aborted_at=CASE
			      WHEN $2 IN ('aborted','expired') THEN $4
			      ELSE aborted_at
			    END,
			    updated_at=$4
			WHERE upload_id=$1 AND status<>'completed'
			RETURNING artifact_id,project_id,version_id
		`, uploadID, status, errorCode, now).Scan(
			&artifactID, &projectID, &versionID,
		); err != nil {
			return mapNotFound(err)
		}
		versionStatus := ""
		switch status {
		case UploadUploading:
			versionStatus = StatusPendingUpload
		case UploadVerifying:
			versionStatus = StatusVerifying
		case UploadFailed, UploadAborted, UploadExpired:
			versionStatus = StatusFailed
		}
		if versionStatus == "" {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE artifact_versions
			SET status=$2,error_code=NULLIF($3,'')
			WHERE version_id=$1 AND status<>'available'
		`, versionID, versionStatus, errorCode); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE artifact_artifacts
			SET status=$3,updated_at=$4
			WHERE project_id=$1 AND artifact_id=$2
			  AND current_version_id=$5 AND status<>'trashed'
		`, projectID, artifactID, versionStatus, now, versionID)
		if err != nil {
			return err
		}
		if status == UploadAborted {
			return store.audit(
				ctx, tx, "artifact.upload.aborted", projectID, artifactID,
				map[string]interface{}{"version_id": versionID},
			)
		}
		return nil
	})
}

func (store PostgresStore) UpsertParts(
	ctx context.Context,
	uploadID string,
	parts []UploadPart,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		for _, part := range parts {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO artifact_upload_parts(
					upload_id,part_number,size_bytes,provider_etag,completed_at
				) VALUES($1,$2,$3,$4,$5)
				ON CONFLICT(upload_id,part_number) DO UPDATE
				SET size_bytes=EXCLUDED.size_bytes,
				    provider_etag=EXCLUDED.provider_etag,
				    completed_at=EXCLUDED.completed_at
			`, uploadID, part.PartNumber, part.SizeBytes,
				normalizeETag(part.ETag), part.CompletedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

func (store PostgresStore) FinalizeUpload(
	ctx context.Context,
	upload UploadSession,
	blob Blob,
	now time.Time,
) (Detail, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var persisted Blob
		err := tx.QueryRowContext(ctx, `
			INSERT INTO artifact_blobs(
				blob_id,project_id,sha256,size_bytes,backend,object_key,
				reference_count,created_at,updated_at
			) VALUES($1,$2,$3,$4,$5,$6,0,$7,$7)
			ON CONFLICT(project_id,sha256,size_bytes) DO UPDATE
			SET updated_at=artifact_blobs.updated_at
			RETURNING blob_id,project_id,sha256,size_bytes,backend,object_key,
			          reference_count
		`, blob.ID, blob.ProjectID, blob.SHA256, blob.SizeBytes,
			blob.Backend, blob.ObjectKey, now).Scan(
			&persisted.ID, &persisted.ProjectID, &persisted.SHA256,
			&persisted.SizeBytes, &persisted.Backend, &persisted.ObjectKey,
			&persisted.ReferenceCount,
		)
		if err != nil {
			return err
		}
		if persisted.ObjectKey != blob.ObjectKey || persisted.Backend != blob.Backend {
			return ErrUploadConflict
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE artifact_versions
			SET blob_id=$2,status='available',available_at=$3,error_code=NULL
			WHERE version_id=$1 AND project_id=$4
			  AND status IN ('pending_upload','verifying')
		`, upload.VersionID, persisted.ID, now, upload.ProjectID)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, `
			UPDATE artifact_artifacts
			SET current_version_id=$3,status='available',updated_at=$4
			WHERE project_id=$1 AND artifact_id=$2 AND status<>'trashed'
		`, upload.ProjectID, upload.ArtifactID, upload.VersionID, now)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, `
			UPDATE artifact_uploads
			SET status='completed',completed_at=$2,updated_at=$2,error_code=NULL
			WHERE upload_id=$1 AND status IN ('completing','verifying')
		`, upload.ID, now)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		if err := store.schedulePreviewInTransaction(
			ctx, tx, upload.ProjectID, upload.ArtifactID, upload.VersionID,
			upload.CreatedBy, now,
		); err != nil {
			return err
		}
		available := Version{
			ID: upload.VersionID, ArtifactID: upload.ArtifactID,
			VersionNo: upload.VersionNo, SHA256: upload.ExpectedSHA256,
			MIMEType: upload.MIMEType, SizeBytes: upload.ExpectedSize,
			AvailableAt: &now, CreatedBy: upload.CreatedBy,
		}
		if err := store.artifactAvailable(
			ctx, tx, upload.ProjectID, upload.ArtifactID, available, "uploaded",
		); err != nil {
			return err
		}
		return store.audit(
			ctx, tx, "artifact.upload.confirmed", upload.ProjectID,
			upload.ArtifactID, map[string]interface{}{
				"size_bytes": upload.ExpectedSize,
				"version_id": upload.VersionID,
			},
		)
	})
	if err != nil {
		return Detail{}, err
	}
	return store.GetDetail(ctx, upload.ProjectID, upload.ArtifactID, false)
}

func (store PostgresStore) Trash(
	ctx context.Context,
	projectID string,
	artifactID string,
	actorID string,
	now time.Time,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var currentVersionID string
		if err := tx.QueryRowContext(ctx, `
			SELECT current_version_id::text
			FROM artifact_artifacts
			WHERE project_id=$1 AND artifact_id=$2 AND status='available'
			FOR UPDATE
		`, projectID, artifactID).Scan(&currentVersionID); err != nil {
			return mapNotFound(err)
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE artifact_artifacts AS artifact
			SET status='trashed',trashed_by=$3,trashed_at=$4,updated_at=$4
			WHERE project_id=$1 AND artifact_id=$2 AND status='available'
			  AND NOT EXISTS(
			    SELECT 1 FROM artifact_uploads AS upload
			    WHERE upload.artifact_id=artifact.artifact_id
			      AND upload.status IN (
			        'initialized','uploading','completing','verifying'
			      )
			  )
		`, projectID, artifactID, actorID, now)
		if err := requireAffected(result, err); err != nil {
			return mapPostgresError(err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE artifact_registry_entries
			SET status='hidden',updated_at=$3
			WHERE project_id=$1 AND artifact_id=$2
		`, projectID, artifactID, now); err != nil {
			return err
		}
		if err := store.artifactDeleted(
			ctx, tx, projectID, artifactID, currentVersionID, actorID, now,
		); err != nil {
			return err
		}
		return store.audit(
			ctx, tx, "artifact.trashed", projectID, artifactID,
			map[string]interface{}{},
		)
	})
}

func (store PostgresStore) Restore(
	ctx context.Context,
	projectID string,
	artifactID string,
	actorID string,
	now time.Time,
) (Detail, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE artifact_artifacts
			SET status='available',trashed_by=NULL,trashed_at=NULL,updated_at=$3
			WHERE project_id=$1 AND artifact_id=$2 AND status='trashed'
		`, projectID, artifactID, now)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE artifact_registry_entries AS registry
			SET description=artifact.description,status='active',updated_at=$3
			FROM artifact_artifacts AS artifact
			WHERE registry.project_id=$1 AND registry.artifact_id=$2
			  AND artifact.project_id=registry.project_id
			  AND artifact.artifact_id=registry.artifact_id
		`, projectID, artifactID, now); err != nil {
			return err
		}
		version, err := scanVersion(tx.QueryRowContext(ctx, versionSelect+`
			JOIN artifact_artifacts AS artifact
			  ON artifact.current_version_id=version.version_id
			WHERE artifact.project_id=$1 AND artifact.artifact_id=$2
		`, projectID, artifactID).Scan)
		if err != nil {
			return err
		}
		version.CreatedBy = actorID
		if err := store.artifactAvailable(
			ctx, tx, projectID, artifactID, version, "restored",
		); err != nil {
			return err
		}
		return store.audit(
			ctx, tx, "artifact.restored", projectID, artifactID,
			map[string]interface{}{},
		)
	})
	if err != nil {
		return Detail{}, ErrNotTrashed
	}
	return store.GetDetail(ctx, projectID, artifactID, false)
}

func (store PostgresStore) RestoreVersion(
	ctx context.Context,
	projectID string,
	artifactID string,
	sourceVersionID string,
	newVersionID string,
	idempotencyKey string,
	actorID string,
	now time.Time,
) (Detail, error) {
	var targetArtifactID string
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var existingVersionID string
		var providerUploadID string
		err := tx.QueryRowContext(ctx, `
			SELECT version_id::text,artifact_id::text,provider_upload_id
			FROM artifact_uploads
			WHERE project_id=$1 AND idempotency_key=$2
		`, projectID, idempotencyKey).Scan(
			&existingVersionID, &targetArtifactID, &providerUploadID,
		)
		if err == nil {
			if targetArtifactID != artifactID ||
				providerUploadID != "restored:"+sourceVersionID {
				return ErrUploadConflict
			}
			newVersionID = existingVersionID
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var status string
		if err := tx.QueryRowContext(ctx, `
			SELECT status FROM artifact_artifacts
			WHERE project_id=$1 AND artifact_id=$2 FOR UPDATE
		`, projectID, artifactID).Scan(&status); err != nil {
			return mapNotFound(err)
		}
		if status != StatusAvailable {
			return ErrNotAvailable
		}
		source, err := scanVersion(tx.QueryRowContext(ctx, versionSelect+`
			WHERE version.project_id=$1 AND version.artifact_id=$2
			  AND version.version_id=$3
		`, projectID, artifactID, sourceVersionID).Scan)
		if err != nil {
			return err
		}
		if source.Status != StatusAvailable {
			return ErrNotAvailable
		}
		var versionNo int
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(version_no),0)+1
			FROM artifact_versions WHERE artifact_id=$1
		`, artifactID).Scan(&versionNo); err != nil {
			return err
		}
		restored := source
		restored.ID = newVersionID
		restored.VersionNo = versionNo
		restored.CreatedBy = actorID
		restored.CreatedAt = now
		restored.AvailableAt = &now
		if err := insertVersion(ctx, tx, restored); err != nil {
			return err
		}
		upload := UploadSession{
			ID: newVersionID, ProjectID: projectID, ArtifactID: artifactID,
			VersionID: newVersionID, ProviderUploadID: "restored:" + sourceVersionID,
			StagingKey: "restores/" + newVersionID, ExpectedSHA256: source.SHA256,
			ExpectedSize: source.SizeBytes, MIMEType: source.MIMEType,
			PartSizeBytes: MultipartMinPartBytes, PartCount: 1,
			Status: UploadCompleted, IdempotencyKey: idempotencyKey,
			CreatedBy: actorID, ExpiresAt: now, CompletedAt: &now,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := insertUpload(ctx, tx, upload); err != nil {
			return mapPostgresError(err)
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE artifact_artifacts
			SET current_version_id=$3,updated_at=$4
			WHERE project_id=$1 AND artifact_id=$2 AND status='available'
		`, projectID, artifactID, newVersionID, now)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		if err := store.schedulePreviewInTransaction(
			ctx, tx, projectID, artifactID, newVersionID, actorID, now,
		); err != nil {
			return err
		}
		if err := store.artifactAvailable(
			ctx, tx, projectID, artifactID, restored, "restored",
		); err != nil {
			return err
		}
		return store.audit(
			ctx, tx, "artifact.version.restored", projectID, artifactID,
			map[string]interface{}{
				"source_version_id": sourceVersionID,
				"version_id":        newVersionID,
			},
		)
	})
	if err != nil {
		return Detail{}, err
	}
	return store.GetDetail(ctx, projectID, artifactID, false)
}

func (store PostgresStore) Purge(
	ctx context.Context,
	projectID string,
	artifactID string,
	deleteObject PurgeObject,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `
			SELECT status FROM artifact_artifacts
			WHERE project_id=$1 AND artifact_id=$2 FOR UPDATE
		`, projectID, artifactID).Scan(&status); err != nil {
			return mapNotFound(err)
		}
		if status != StatusTrashed {
			return ErrNotTrashed
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT blob.blob_id,blob.object_key
			FROM artifact_blobs AS blob
			WHERE blob.blob_id IN (
			  SELECT version.blob_id
			  FROM artifact_versions AS version
			  WHERE version.project_id=$1
			    AND version.artifact_id=$2
			    AND version.blob_id IS NOT NULL
			  UNION
			  SELECT preview.blob_id
			  FROM artifact_previews AS preview
			  WHERE preview.project_id=$1
			    AND preview.artifact_id=$2
			    AND preview.blob_id IS NOT NULL
			)
		`, projectID, artifactID)
		if err != nil {
			return err
		}
		type candidate struct{ id, key string }
		candidates := []candidate{}
		for rows.Next() {
			var value candidate
			if err := rows.Scan(&value.id, &value.key); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, value)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM artifact_artifacts
			WHERE project_id=$1 AND artifact_id=$2 AND status='trashed'
		`, projectID, artifactID); err != nil {
			return err
		}
		for _, candidate := range candidates {
			var references int64
			if err := tx.QueryRowContext(ctx, `
				SELECT reference_count FROM artifact_blobs
				WHERE blob_id=$1 FOR UPDATE
			`, candidate.id).Scan(&references); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return err
			}
			if references != 0 {
				continue
			}
			if err := deleteObject(ctx, candidate.key); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM artifact_blobs
				WHERE blob_id=$1 AND reference_count=0
			`, candidate.id); err != nil {
				return err
			}
		}
		return store.audit(
			ctx, tx, "artifact.purged", projectID, artifactID,
			map[string]interface{}{},
		)
	})
}

func (store PostgresStore) ExpireUploads(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]UploadSession, error) {
	if limit < 1 {
		limit = 50
	}
	uploads := []UploadSession{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		rows, err := tx.QueryContext(ctx, uploadSelect+`
			WHERE upload.expires_at<=$1
			  AND upload.status IN ('initialized','uploading','completing','verifying')
			ORDER BY upload.expires_at,upload.upload_id
			LIMIT $2
			FOR UPDATE OF upload SKIP LOCKED
		`, now, limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			upload, err := scanUpload(rows.Scan)
			if err != nil {
				rows.Close()
				return err
			}
			uploads = append(uploads, upload)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, upload := range uploads {
			if _, err := tx.ExecContext(ctx, `
				UPDATE artifact_uploads
				SET status='expired',aborted_at=$2,updated_at=$2,
				    error_code='ARTIFACT_UPLOAD_EXPIRED'
				WHERE upload_id=$1
			`, upload.ID, now); err != nil {
				return err
			}
		}
		return nil
	})
	return uploads, err
}

func (store PostgresStore) MarkProviderAborted(
	ctx context.Context,
	uploadID string,
	now time.Time,
) error {
	_, err := store.DB.ExecContext(ctx, `
		UPDATE artifact_uploads
		SET provider_upload_id='aborted:'||upload_id::text,updated_at=$2
		WHERE upload_id=$1
		  AND status IN ('aborted','expired')
		  AND provider_upload_id NOT LIKE 'aborted:%'
	`, uploadID, now)
	return err
}

func (store PostgresStore) listParts(
	ctx context.Context,
	uploadID string,
) ([]UploadPart, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT part_number,size_bytes,provider_etag,completed_at
		FROM artifact_upload_parts
		WHERE upload_id=$1 ORDER BY part_number
	`, uploadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parts := []UploadPart{}
	for rows.Next() {
		var part UploadPart
		if err := rows.Scan(
			&part.PartNumber, &part.SizeBytes, &part.ETag, &part.CompletedAt,
		); err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, rows.Err()
}

const artifactSelect = `
	SELECT artifact.artifact_id,artifact.project_id,artifact.kind,artifact.source,
	       COALESCE(
	         (
	           SELECT relation.target_id::text
	           FROM artifact_relations AS relation
	           WHERE relation.artifact_id=artifact.artifact_id
	             AND relation.relation_type='output'
	           ORDER BY relation.created_at,relation.relation_id
	           LIMIT 1
	         ),
	         (
	           SELECT version.repository_id::text
	           FROM artifact_versions AS version
	           WHERE version.version_id=artifact.current_version_id
	             AND version.storage_class='git'
	         ),
	         ''
	       ),
	       to_json(artifact.tags),artifact.name,artifact.description,
	       artifact.recommended_usage,artifact.current_version_id::text,
	       artifact.status,artifact.created_by,artifact.trashed_at,
	       artifact.created_at,artifact.updated_at
	FROM artifact_artifacts AS artifact
`

const versionSelect = `
	SELECT version.version_id,version.artifact_id,version.project_id,
	       version.version_no,version.storage_class,version.filename,
	       version.sha256,version.mime_type,version.size_bytes,version.status,
	       version.available_at,version.created_by,version.created_at,
	       COALESCE(version.blob_id::text,''),COALESCE(blob.object_key,''),
	       COALESCE(blob.backend,''),COALESCE(version.repository_id::text,''),
	       COALESCE(version.commit_sha,''),COALESCE(version.workspace_kind,''),
	       COALESCE(version.repository_path,'')
	FROM artifact_versions AS version
	LEFT JOIN artifact_blobs AS blob ON blob.blob_id=version.blob_id
`

const uploadSelect = `
	SELECT upload.upload_id,upload.project_id,upload.artifact_id,upload.version_id,
	       upload.provider_upload_id,upload.staging_key,upload.expected_sha256,
	       upload.expected_size_bytes,upload.mime_type,upload.part_size_bytes,
	       upload.part_count,upload.status,upload.idempotency_key,upload.created_by,
	       upload.expires_at,upload.completed_at,upload.aborted_at,
	       COALESCE(upload.error_code,''),upload.created_at,upload.updated_at,
	       version.filename,version.version_no,artifact.status
	FROM artifact_uploads AS upload
	JOIN artifact_versions AS version ON version.version_id=upload.version_id
	JOIN artifact_artifacts AS artifact ON artifact.artifact_id=upload.artifact_id
`

type scanner func(...interface{}) error

func scanArtifact(scan scanner) (Artifact, error) {
	var artifact Artifact
	var tags []byte
	var description sql.NullString
	var recommended sql.NullString
	var currentVersion sql.NullString
	var sourceObjectID string
	err := scan(
		&artifact.ID, &artifact.ProjectID, &artifact.Kind, &artifact.Source,
		&sourceObjectID, &tags, &artifact.Name, &description, &recommended,
		&currentVersion,
		&artifact.Status, &artifact.CreatedBy, &artifact.TrashedAt,
		&artifact.CreatedAt, &artifact.UpdatedAt,
	)
	if err != nil {
		return Artifact{}, mapNotFound(err)
	}
	if err := json.Unmarshal(tags, &artifact.Tags); err != nil {
		return Artifact{}, fmt.Errorf("decode artifact tags: %w", err)
	}
	if artifact.Tags == nil {
		artifact.Tags = []string{}
	}
	if description.Valid {
		artifact.Description = &description.String
	}
	if recommended.Valid && strings.TrimSpace(recommended.String) != "" {
		if json.Unmarshal([]byte(recommended.String), &artifact.RecommendedUsage) != nil {
			artifact.RecommendedUsage = []string{recommended.String}
		}
	}
	if artifact.RecommendedUsage == nil {
		artifact.RecommendedUsage = []string{}
	}
	if currentVersion.Valid {
		artifact.CurrentVersionID = &currentVersion.String
	}
	if sourceObjectID != "" {
		artifact.SourceObjectID = &sourceObjectID
	}
	return artifact, nil
}

func scanVersion(scan scanner) (Version, error) {
	var version Version
	var repositoryID, commitSHA, workspace, repositoryPath string
	err := scan(
		&version.ID, &version.ArtifactID, &version.ProjectID,
		&version.VersionNo, &version.StorageClass, &version.Filename,
		&version.SHA256, &version.MIMEType, &version.SizeBytes, &version.Status,
		&version.AvailableAt, &version.CreatedBy, &version.CreatedAt,
		&version.BlobID, &version.ObjectKey, &version.Backend,
		&repositoryID, &commitSHA, &workspace, &repositoryPath,
	)
	if err != nil {
		return Version{}, mapNotFound(err)
	}
	if version.StorageClass == "git" {
		version.GitReference = &GitReference{
			RepositoryID: repositoryID, CommitSHA: commitSHA,
			Workspace: workspace, Path: repositoryPath,
		}
	}
	return version, nil
}

func scanUpload(scan scanner) (UploadSession, error) {
	var upload UploadSession
	err := scan(
		&upload.ID, &upload.ProjectID, &upload.ArtifactID, &upload.VersionID,
		&upload.ProviderUploadID, &upload.StagingKey, &upload.ExpectedSHA256,
		&upload.ExpectedSize, &upload.MIMEType, &upload.PartSizeBytes,
		&upload.PartCount, &upload.Status, &upload.IdempotencyKey,
		&upload.CreatedBy, &upload.ExpiresAt, &upload.CompletedAt,
		&upload.AbortedAt, &upload.ErrorCode, &upload.CreatedAt,
		&upload.UpdatedAt, &upload.Filename, &upload.VersionNo,
		&upload.ArtifactStatus,
	)
	if err != nil {
		return UploadSession{}, mapNotFound(err)
	}
	return upload, nil
}

func insertArtifact(
	ctx context.Context,
	tx transaction.Tx,
	artifact Artifact,
) error {
	tags, _ := json.Marshal(artifact.Tags)
	var recommended interface{}
	if len(artifact.RecommendedUsage) > 0 {
		encoded, _ := json.Marshal(artifact.RecommendedUsage)
		recommended = string(encoded)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO artifact_artifacts(
			artifact_id,project_id,kind,source,name,tags,description,
			recommended_usage,status,current_version_id,created_by,
			created_at,updated_at
		) VALUES(
			$1,$2,$3,$4,$5,
			ARRAY(SELECT jsonb_array_elements_text($6::jsonb)),
			$7,$8,$9,$10,$11,$12,$12
		)
	`, artifact.ID, artifact.ProjectID, artifact.Kind, artifact.Source,
		artifact.Name, tags, artifact.Description, recommended,
		artifact.Status, artifact.CurrentVersionID, artifact.CreatedBy,
		artifact.CreatedAt)
	return err
}

func insertVersion(
	ctx context.Context,
	tx transaction.Tx,
	version Version,
) error {
	var repositoryID, commitSHA, workspace, repositoryPath interface{}
	if version.GitReference != nil {
		repositoryID = version.GitReference.RepositoryID
		commitSHA = version.GitReference.CommitSHA
		workspace = version.GitReference.Workspace
		repositoryPath = version.GitReference.Path
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO artifact_versions(
			version_id,artifact_id,project_id,version_no,storage_class,blob_id,
			repository_id,commit_sha,workspace_kind,repository_path,
			filename,mime_type,size_bytes,sha256,status,created_by,
			created_at,available_at
		) VALUES(
			$1,$2,$3,$4,$5,NULLIF($6,'')::uuid,
			NULLIF($7,'')::uuid,NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),
			$11,$12,$13,$14,$15,$16,$17,$18
		)
	`, version.ID, version.ArtifactID, version.ProjectID, version.VersionNo,
		version.StorageClass, version.BlobID, repositoryID, commitSHA,
		workspace, repositoryPath, version.Filename, version.MIMEType,
		version.SizeBytes, version.SHA256, version.Status, version.CreatedBy,
		version.CreatedAt, version.AvailableAt)
	return err
}

func insertUpload(
	ctx context.Context,
	tx transaction.Tx,
	upload UploadSession,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO artifact_uploads(
			upload_id,project_id,artifact_id,version_id,provider_upload_id,
			staging_key,expected_sha256,expected_size_bytes,mime_type,
			part_size_bytes,part_count,status,idempotency_key,created_by,
			expires_at,completed_at,aborted_at,error_code,created_at,updated_at
		) VALUES(
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,
			$15,$16,$17,NULLIF($18,''),$19,$20
		)
	`, upload.ID, upload.ProjectID, upload.ArtifactID, upload.VersionID,
		upload.ProviderUploadID, upload.StagingKey, upload.ExpectedSHA256,
		upload.ExpectedSize, upload.MIMEType, upload.PartSizeBytes,
		upload.PartCount, upload.Status, upload.IdempotencyKey,
		upload.CreatedBy, upload.ExpiresAt, upload.CompletedAt,
		upload.AbortedAt, upload.ErrorCode, upload.CreatedAt, upload.UpdatedAt)
	return err
}

func decodeArtifactCursor(value string) (string, string, error) {
	if value == "" {
		return "", "", nil
	}
	cursor, err := pagination.Decode(value)
	if err != nil {
		return "", "", err
	}
	if _, err := time.Parse(time.RFC3339Nano, cursor.SortValue); err != nil {
		return "", "", err
	}
	return cursor.SortValue, cursor.ID, nil
}

func requireAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrUploadConflict
	}
	return nil
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func mapPostgresError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return ErrUploadConflict
		case "23503":
			return ErrNotFound
		case "23514", "22P02":
			return ErrInvalid
		}
	}
	return err
}

func (store PostgresStore) audit(
	ctx context.Context,
	tx transaction.Tx,
	action string,
	projectID string,
	artifactID string,
	metadata map[string]interface{},
) error {
	if store.Audit == nil {
		return nil
	}
	return store.Audit.RecordInTransaction(ctx, tx, audit.Event{
		Action: action, Category: "artifact", Metadata: metadata,
		Outcome: "success", ProjectID: projectID, ResourceID: artifactID,
		ResourceType: "artifact", Source: "core",
	})
}

func (store PostgresStore) artifactCreated(
	ctx context.Context,
	tx transaction.Tx,
	item Artifact,
	version Version,
) error {
	if store.Outbox == nil {
		return nil
	}
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{
		Actor:     map[string]string{"user_id": item.CreatedBy},
		EventType: "artifact.created",
		Payload: map[string]interface{}{
			"artifact_id": item.ID,
			"version_id":  version.ID,
			"kind":        item.Kind,
			"source":      item.Source,
			"name":        item.Name,
			"filename":    version.Filename,
			"sha256":      version.SHA256,
			"size_bytes":  version.SizeBytes,
			"status":      StatusPendingUpload,
		},
		Producer: "artifact", ProjectID: item.ProjectID,
	})
	return err
}

func (store PostgresStore) artifactAvailable(
	ctx context.Context,
	tx transaction.Tx,
	projectID string,
	artifactID string,
	version Version,
	reason string,
) error {
	if store.Outbox == nil {
		return nil
	}
	availableAt := version.AvailableAt
	if availableAt == nil {
		return ErrInvalid
	}
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{
		Actor:     map[string]string{"user_id": version.CreatedBy},
		EventType: "artifact.available",
		Payload: map[string]interface{}{
			"artifact_id":  artifactID,
			"version_id":   version.ID,
			"version_no":   version.VersionNo,
			"sha256":       version.SHA256,
			"size_bytes":   version.SizeBytes,
			"mime_type":    version.MIMEType,
			"reason":       reason,
			"available_at": *availableAt,
		},
		Producer: "artifact", ProjectID: projectID,
	})
	return err
}

func (store PostgresStore) artifactDeleted(
	ctx context.Context,
	tx transaction.Tx,
	projectID string,
	artifactID string,
	currentVersionID string,
	actorID string,
	trashedAt time.Time,
) error {
	if store.Outbox == nil {
		return nil
	}
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{
		Actor:     map[string]string{"user_id": actorID},
		EventType: "artifact.deleted",
		Payload: map[string]interface{}{
			"artifact_id":        artifactID,
			"current_version_id": currentVersionID,
			"reason":             "trashed",
			"trashed_at":         trashedAt,
		},
		Producer: "artifact", ProjectID: projectID,
	})
	return err
}
