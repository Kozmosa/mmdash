package artifact

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func (store PostgresStore) schedulePreviewInTransaction(
	ctx context.Context,
	tx transaction.Tx,
	projectID string,
	artifactID string,
	versionID string,
	actorID string,
	now time.Time,
) error {
	// Lightweight store fixtures used by storage-focused tests do not run the
	// Job subsystem. Production always wires both dependencies together.
	if store.Jobs == nil {
		return nil
	}
	var existing string
	err := tx.QueryRowContext(ctx, `
		SELECT preview_id::text
		FROM artifact_previews
		WHERE project_id=$1 AND version_id=$2
		LIMIT 1
	`, projectID, versionID).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var filename, mimeType, versionStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT filename,mime_type,status
		FROM artifact_versions
		WHERE project_id=$1 AND artifact_id=$2 AND version_id=$3
	`, projectID, artifactID, versionID).Scan(
		&filename, &mimeType, &versionStatus,
	); err != nil {
		return mapNotFound(err)
	}
	if versionStatus != StatusAvailable {
		return ErrNotAvailable
	}
	attachmentID, err := store.Generator.New()
	if err != nil {
		return err
	}
	previewID, err := store.Generator.New()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifact_registry_entries(
			attachment_id,project_id,artifact_id,version_id,source,
			description,recommended_usage,status,created_by,created_at,updated_at
		)
		SELECT $1,artifact.project_id,artifact.artifact_id,$4,artifact.source,
		       artifact.description,artifact.recommended_usage,'active',$5,$6,$6
		FROM artifact_artifacts AS artifact
		WHERE artifact.project_id=$2 AND artifact.artifact_id=$3
		ON CONFLICT(project_id,artifact_id) DO UPDATE
		SET version_id=EXCLUDED.version_id,
		    source=EXCLUDED.source,
		    description=EXCLUDED.description,
		    recommended_usage=EXCLUDED.recommended_usage,
		    status='active',
		    updated_at=EXCLUDED.updated_at
	`, attachmentID, projectID, artifactID, versionID, actorID, now); err != nil {
		return err
	}

	previewType := classifyPreviewType(filename, mimeType)
	var jobID interface{}
	if store.Jobs != nil {
		job, _, err := store.Jobs.CreateInTransaction(
			ctx, tx, actorID, jobs.CreateInput{
				IdempotencyKey: "artifact-preview:" + versionID,
				JobType:        "artifact.preview",
				MaxAttempts:    3,
				Payload: map[string]interface{}{
					"project_id":   projectID,
					"artifact_id":  artifactID,
					"version_id":   versionID,
					"preview_id":   previewID,
					"preview_type": previewType,
				},
				Priority:       0,
				ProjectID:      projectID,
				TimeoutSeconds: 300,
			},
		)
		if err != nil {
			return err
		}
		jobID = job.ID
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO artifact_previews(
			preview_id,project_id,artifact_id,version_id,preview_type,
			status,structure_summary,job_id,created_at,updated_at
		) VALUES($1,$2,$3,$4,$5,'queued','{}'::jsonb,$6,$7,$7)
	`, previewID, projectID, artifactID, versionID, previewType, jobID, now)
	return err
}

func classifyPreviewType(filename, mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	extension := strings.ToLower(filepath.Ext(filename))
	switch {
	case mimeType == "image/svg+xml":
		return PreviewText
	case strings.HasPrefix(mimeType, "image/"):
		return PreviewImage
	case mimeType == "application/pdf" || extension == ".pdf":
		return PreviewPDF
	case mimeType == "text/csv" || mimeType == "application/csv" ||
		extension == ".csv":
		return PreviewCSV
	case mimeType == "application/json" || strings.HasSuffix(mimeType, "+json") ||
		extension == ".json":
		return PreviewJSON
	default:
		return PreviewText
	}
}

func (store PostgresStore) ListPreviews(
	ctx context.Context,
	projectID string,
	artifactID string,
	versionID string,
) (PreviewList, error) {
	rows, err := store.DB.QueryContext(ctx, previewSelect+`
		WHERE preview.project_id=$1
		  AND preview.artifact_id=$2
		  AND preview.version_id=$3
		ORDER BY
		  CASE preview.preview_type
		    WHEN 'thumbnail' THEN 2
		    ELSE 1
		  END,
		  preview.created_at,
		  preview.preview_id
	`, projectID, artifactID, versionID)
	if err != nil {
		return PreviewList{}, err
	}
	defer rows.Close()
	items := []Preview{}
	for rows.Next() {
		preview, err := scanPreview(rows.Scan)
		if err != nil {
			return PreviewList{}, err
		}
		items = append(items, preview)
	}
	if err := rows.Err(); err != nil {
		return PreviewList{}, err
	}
	if len(items) == 0 {
		return PreviewList{}, ErrNotFound
	}
	return PreviewList{Items: items}, nil
}

func (store PostgresStore) GetPreview(
	ctx context.Context,
	projectID string,
	artifactID string,
	versionID string,
	previewID string,
) (Preview, error) {
	return scanPreview(store.DB.QueryRowContext(ctx, previewSelect+`
		WHERE preview.project_id=$1
		  AND preview.artifact_id=$2
		  AND preview.version_id=$3
		  AND preview.preview_id=$4
	`, projectID, artifactID, versionID, previewID).Scan)
}

func (store PostgresStore) GetPreviewByJob(
	ctx context.Context,
	jobID string,
) (Preview, error) {
	return scanPreview(store.DB.QueryRowContext(ctx, previewSelect+`
		WHERE preview.job_id=$1
		  AND preview.preview_type<>'thumbnail'
	`, jobID).Scan)
}

func (store PostgresStore) CreatePreviewTransfer(
	ctx context.Context,
	transfer PreviewTransfer,
) (PreviewTransfer, bool, error) {
	var result PreviewTransfer
	created := true
	err := store.DB.QueryRowContext(ctx, `
		INSERT INTO artifact_preview_transfers(
			transfer_id,job_id,project_id,artifact_id,version_id,preview_type,
			backend,provider_upload_id,staging_key,filename,mime_type,
			expected_size,expected_sha256,status,expires_at,created_at,updated_at
		) VALUES(
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'prepared',$14,$15,$15
		)
		ON CONFLICT(job_id,preview_type) DO NOTHING
		RETURNING `+previewTransferColumns,
		transfer.ID, transfer.JobID, transfer.ProjectID, transfer.ArtifactID,
		transfer.VersionID, transfer.PreviewType, transfer.Backend,
		transfer.ProviderUploadID,
		transfer.StagingKey, transfer.Filename, transfer.MIMEType,
		transfer.ExpectedSize, transfer.ExpectedSHA256, transfer.ExpiresAt,
		transfer.CreatedAt,
	).Scan(previewTransferDestinations(&result)...)
	if errors.Is(err, sql.ErrNoRows) {
		created = false
		result, err = store.GetPreviewTransfer(ctx, transfer.JobID, transfer.PreviewType)
	}
	return result, created, err
}

func (store PostgresStore) GetPreviewTransfer(
	ctx context.Context,
	jobID string,
	previewType string,
) (PreviewTransfer, error) {
	var transfer PreviewTransfer
	err := store.DB.QueryRowContext(ctx, `
		SELECT `+previewTransferColumns+`
		FROM artifact_preview_transfers
		WHERE job_id=$1 AND preview_type=$2
	`, jobID, previewType).Scan(previewTransferDestinations(&transfer)...)
	return transfer, mapNotFound(err)
}

func (store PostgresStore) MarkPreviewTransferUploaded(
	ctx context.Context,
	transferID string,
	etag string,
	now time.Time,
) error {
	result, err := store.DB.ExecContext(ctx, `
		UPDATE artifact_preview_transfers
		SET status='uploaded',provider_etag=$2,updated_at=$3
		WHERE transfer_id=$1
		  AND status IN ('prepared','uploaded')
		  AND (provider_etag IS NULL OR provider_etag=$2)
		  AND expires_at>$3
	`, transferID, normalizeETag(etag), now)
	return requireAffected(result, err)
}

func (store PostgresStore) ExpirePreviewTransfers(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]PreviewTransfer, error) {
	if limit < 1 {
		limit = 50
	}
	transfers := []PreviewTransfer{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			WITH expired AS (
			  SELECT transfer_id
			  FROM artifact_preview_transfers
			  WHERE status IN ('prepared','uploaded') AND expires_at<=$1
			  ORDER BY expires_at,transfer_id
			  FOR UPDATE SKIP LOCKED
			  LIMIT $2
			)
			UPDATE artifact_preview_transfers AS transfer
			SET status='expired',updated_at=$1
			FROM expired
			WHERE transfer.transfer_id=expired.transfer_id
			RETURNING `+previewTransferReturningColumns,
			now, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var transfer PreviewTransfer
			if err := rows.Scan(previewTransferDestinations(&transfer)...); err != nil {
				return err
			}
			transfers = append(transfers, transfer)
		}
		return rows.Err()
	})
	return transfers, err
}

func (store PostgresStore) MarkPreviewTransferAborted(
	ctx context.Context,
	transferID string,
	now time.Time,
) error {
	_, err := store.DB.ExecContext(ctx, `
		UPDATE artifact_preview_transfers
		SET aborted_at=COALESCE(aborted_at,$2),updated_at=$2
		WHERE transfer_id=$1 AND status IN ('aborted','expired')
	`, transferID, now)
	return err
}

func (store PostgresStore) CompletePreviewInTransaction(
	ctx context.Context,
	tx transaction.Tx,
	job jobs.Job,
	result previewResult,
	now time.Time,
) error {
	if job.JobType != "artifact.preview" {
		return nil
	}
	if job.Status != jobs.StatusSucceeded {
		return store.UpdatePreviewJobInTransaction(
			ctx, tx, job, "ARTIFACT_PREVIEW_JOB_CANCELLED",
			"Preview Job did not complete successfully", now,
		)
	}
	summary, err := json.Marshal(result.StructuralSummary)
	if err != nil {
		return jobs.ErrInvalid
	}
	errorCode := interface{}(nil)
	if result.ErrorCode != "" {
		errorCode = result.ErrorCode
	}
	internalStatus := result.Status
	if internalStatus == PreviewProcessing {
		internalStatus = "running"
	}
	update, err := tx.ExecContext(ctx, `
		UPDATE artifact_previews
		SET status=$4,structure_summary=$5,error_code=$6,error_message=NULL,
		    available_at=CASE
		      WHEN $4='available' THEN $7::timestamptz
		      ELSE NULL::timestamptz
		    END,
		    updated_at=$7
		WHERE preview_id=$1 AND project_id=$2 AND job_id=$3
		  AND preview_type<>'thumbnail'
	`, result.PreviewID, result.ProjectID, job.ID, internalStatus, summary,
		errorCode, now)
	if err := requireAffected(update, err); err != nil {
		return err
	}
	for _, output := range result.Outputs {
		if output.PreviewType != PreviewThumbnail {
			return jobs.ErrInvalid
		}
		var transfer PreviewTransfer
		err := tx.QueryRowContext(ctx, `
			SELECT `+previewTransferColumns+`
			FROM artifact_preview_transfers
			WHERE job_id=$1 AND preview_type=$2
			FOR UPDATE
		`, job.ID, output.PreviewType).Scan(
			previewTransferDestinations(&transfer)...,
		)
		if err != nil || transfer.Status == "expired" ||
			transfer.Status == "aborted" {
			return jobs.ErrInvalid
		}
		blobID, err := store.Generator.New()
		if err != nil {
			return err
		}
		var persisted Blob
		objectKey := ContentObjectKey(
			transfer.ProjectID, transfer.ExpectedSHA256,
		)
		err = tx.QueryRowContext(ctx, `
			INSERT INTO artifact_blobs(
				blob_id,project_id,sha256,size_bytes,backend,object_key,
				reference_count,created_at,updated_at
			) VALUES($1,$2,$3,$4,$5,$6,0,$7,$7)
			ON CONFLICT(project_id,sha256,size_bytes) DO UPDATE
			SET updated_at=artifact_blobs.updated_at
			RETURNING blob_id,project_id,sha256,size_bytes,backend,object_key,
			          reference_count
		`, blobID, transfer.ProjectID, transfer.ExpectedSHA256,
			transfer.ExpectedSize, transfer.Backend,
			objectKey, now).Scan(
			&persisted.ID, &persisted.ProjectID, &persisted.SHA256,
			&persisted.SizeBytes, &persisted.Backend, &persisted.ObjectKey,
			&persisted.ReferenceCount,
		)
		if err != nil {
			return err
		}
		if persisted.ObjectKey != objectKey {
			return ErrUploadConflict
		}
		previewID, err := store.Generator.New()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO artifact_previews(
				preview_id,project_id,artifact_id,version_id,preview_type,
				status,blob_id,structure_summary,job_id,
				created_at,updated_at,available_at
			) VALUES(
				$1,$2,$3,$4,$5,'available',$6,'{}'::jsonb,$7,$8,$8,$8
			)
			ON CONFLICT(version_id,preview_type) DO UPDATE
			SET status='available',blob_id=EXCLUDED.blob_id,
			    structure_summary='{}'::jsonb,job_id=EXCLUDED.job_id,
			    error_code=NULL,error_message=NULL,
			    updated_at=EXCLUDED.updated_at,available_at=EXCLUDED.available_at
		`, previewID, transfer.ProjectID, transfer.ArtifactID,
			transfer.VersionID, output.PreviewType, persisted.ID, job.ID, now)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE artifact_preview_transfers
			SET status='completed',completed_at=$2,updated_at=$2
			WHERE transfer_id=$1 AND status IN ('prepared','uploaded','completed')
		`, transfer.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (store PostgresStore) UpdatePreviewJobInTransaction(
	ctx context.Context,
	tx transaction.Tx,
	job jobs.Job,
	errorCode string,
	errorMessage string,
	now time.Time,
) error {
	if job.JobType != "artifact.preview" {
		return nil
	}
	status := "failed"
	if job.Status == jobs.StatusQueued {
		status = "queued"
		errorCode = ""
		errorMessage = ""
	} else if job.Status == jobs.StatusRunning {
		status = "running"
		errorCode = ""
		errorMessage = ""
	}
	var code, message interface{}
	if errorCode != "" {
		code = errorCode
	}
	if errorMessage != "" {
		message = errorMessage
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE artifact_previews
		SET status=$2,error_code=$3,error_message=$4,updated_at=$5
		WHERE job_id=$1 AND preview_type<>'thumbnail'
		  AND status NOT IN ('available','unsupported')
	`, job.ID, status, code, message, now)
	if err != nil {
		return err
	}
	_, err = result.RowsAffected()
	return err
}

func (store PostgresStore) ReconcilePreviewJobs(
	ctx context.Context,
	now time.Time,
) error {
	_, err := store.DB.ExecContext(ctx, `
		UPDATE artifact_previews AS preview
		SET status=CASE
		      WHEN job.status='queued' THEN 'queued'
		      WHEN job.status='running' THEN 'running'
		      ELSE 'failed'
		    END,
		    error_code=CASE
		      WHEN job.status IN ('failed','cancelled','timed_out')
		        THEN COALESCE(job.error_code,'ARTIFACT_PREVIEW_JOB_FAILED')
		      ELSE NULL
		    END,
		    error_message=CASE
		      WHEN job.status IN ('failed','cancelled','timed_out')
		        THEN COALESCE(job.error_message,'Preview Job did not complete')
		      ELSE NULL
		    END,
		    updated_at=$1
		FROM jobs AS job
		WHERE preview.job_id=job.job_id
		  AND preview.preview_type<>'thumbnail'
		  AND preview.status IN ('queued','running')
		  AND job.status IN ('queued','running','failed','cancelled','timed_out')
		  AND preview.status IS DISTINCT FROM CASE
		    WHEN job.status='queued' THEN 'queued'
		    WHEN job.status='running' THEN 'running'
		    ELSE 'failed'
		  END
	`, now)
	return err
}

const previewSelect = `
	SELECT preview.preview_id::text,preview.project_id::text,
	       preview.artifact_id::text,preview.version_id::text,
	       preview.preview_type,preview.status,preview.structure_summary,
	       preview.error_code,preview.created_at,preview.updated_at,
	       COALESCE(preview.job_id::text,''),COALESCE(preview.blob_id::text,''),
	       COALESCE(blob.object_key,''),COALESCE(blob.backend,''),
	       COALESCE(transfer.mime_type,''),COALESCE(transfer.expected_size,0),
	       COALESCE(transfer.filename,'')
	FROM artifact_previews AS preview
	LEFT JOIN artifact_blobs AS blob
	  ON blob.project_id=preview.project_id AND blob.blob_id=preview.blob_id
	LEFT JOIN artifact_preview_transfers AS transfer
	  ON transfer.job_id=preview.job_id
	 AND transfer.preview_type=preview.preview_type
	 AND transfer.status='completed'
`

type previewScanner func(...interface{}) error

func scanPreview(scan previewScanner) (Preview, error) {
	var preview Preview
	var summary []byte
	var errorCode sql.NullString
	err := scan(
		&preview.ID, &preview.ProjectID, &preview.ArtifactID, &preview.VersionID,
		&preview.PreviewType, &preview.Status, &summary, &errorCode,
		&preview.CreatedAt, &preview.UpdatedAt, &preview.JobID, &preview.BlobID,
		&preview.ObjectKey, &preview.Backend, &preview.MIMEType,
		&preview.SizeBytes, &preview.Filename,
	)
	if err != nil {
		return Preview{}, mapNotFound(err)
	}
	if err := json.Unmarshal(summary, &preview.StructuralSummary); err != nil {
		return Preview{}, fmt.Errorf("decode preview summary: %w", err)
	}
	if preview.StructuralSummary == nil {
		preview.StructuralSummary = map[string]interface{}{}
	}
	if errorCode.Valid {
		preview.ErrorCode = &errorCode.String
	}
	if preview.Status == "running" {
		preview.Status = PreviewProcessing
	}
	return preview, nil
}

const previewTransferColumns = `
	transfer_id::text,job_id::text,project_id::text,artifact_id::text,
	version_id::text,preview_type,backend,provider_upload_id,staging_key,
	filename,mime_type,expected_size,expected_sha256,status,
	COALESCE(provider_etag,''),expires_at,completed_at,aborted_at,
	created_at,updated_at`

const previewTransferReturningColumns = `
	transfer.transfer_id::text,transfer.job_id::text,
	transfer.project_id::text,transfer.artifact_id::text,
	transfer.version_id::text,transfer.preview_type,transfer.backend,
	transfer.provider_upload_id,transfer.staging_key,transfer.filename,
	transfer.mime_type,transfer.expected_size,transfer.expected_sha256,
	transfer.status,COALESCE(transfer.provider_etag,''),
	transfer.expires_at,transfer.completed_at,transfer.aborted_at,
	transfer.created_at,transfer.updated_at`

func previewTransferDestinations(transfer *PreviewTransfer) []interface{} {
	return []interface{}{
		&transfer.ID, &transfer.JobID, &transfer.ProjectID, &transfer.ArtifactID,
		&transfer.VersionID, &transfer.PreviewType, &transfer.Backend,
		&transfer.ProviderUploadID,
		&transfer.StagingKey, &transfer.Filename, &transfer.MIMEType,
		&transfer.ExpectedSize, &transfer.ExpectedSHA256, &transfer.Status,
		&transfer.ProviderETag, &transfer.ExpiresAt, &transfer.CompletedAt,
		&transfer.AbortedAt, &transfer.CreatedAt, &transfer.UpdatedAt,
	}
}
