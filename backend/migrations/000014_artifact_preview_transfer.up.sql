CREATE TABLE artifact_preview_transfers (
    transfer_id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(job_id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE RESTRICT,
    artifact_id UUID NOT NULL,
    version_id UUID NOT NULL,
    preview_type TEXT NOT NULL
        CHECK (preview_type IN ('image', 'pdf', 'csv', 'json', 'text', 'thumbnail')),
    backend TEXT NOT NULL CHECK (backend IN ('local', 'minio', 's3')),
    provider_upload_id TEXT NOT NULL CHECK (
        length(provider_upload_id) BETWEEN 1 AND 2048
    ),
    staging_key TEXT NOT NULL CHECK (
        length(staging_key) BETWEEN 1 AND 1024
        AND staging_key !~ '(^/|(^|/)\.\.(/|$)|\\)'
    ),
    filename TEXT NOT NULL CHECK (length(filename) BETWEEN 1 AND 255),
    mime_type TEXT NOT NULL CHECK (length(mime_type) BETWEEN 1 AND 255),
    expected_size BIGINT NOT NULL CHECK (expected_size >= 0),
    expected_sha256 TEXT NOT NULL CHECK (expected_sha256 ~ '^[0-9a-f]{64}$'),
    status TEXT NOT NULL DEFAULT 'prepared'
        CHECK (status IN ('prepared', 'uploaded', 'completed', 'aborted', 'expired')),
    provider_etag TEXT CHECK (
        provider_etag IS NULL OR length(provider_etag) BETWEEN 1 AND 1024
    ),
    expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    aborted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (project_id, artifact_id)
        REFERENCES artifact_artifacts(project_id, artifact_id)
        ON DELETE CASCADE,
    FOREIGN KEY (project_id, version_id)
        REFERENCES artifact_versions(project_id, version_id)
        ON DELETE CASCADE,
    FOREIGN KEY (artifact_id, version_id)
        REFERENCES artifact_versions(artifact_id, version_id)
        ON DELETE CASCADE,
    UNIQUE (job_id, preview_type)
);

CREATE INDEX artifact_preview_transfers_expiry_idx
    ON artifact_preview_transfers (expires_at, transfer_id)
    WHERE status IN ('prepared', 'uploaded');
