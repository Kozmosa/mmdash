CREATE TABLE artifact_artifacts (
    artifact_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK (kind IN (
        'problem', 'attachment', 'experiment_result',
        'model_file', 'article_build', 'other'
    )),
    source TEXT NOT NULL CHECK (source IN (
        'user_upload', 'experiment', 'model', 'article', 'system'
    )),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
    tags TEXT[] NOT NULL DEFAULT '{}',
    description TEXT CHECK (
        description IS NULL OR length(description) <= 10000
    ),
    recommended_usage TEXT CHECK (
        recommended_usage IS NULL OR length(recommended_usage) <= 10000
    ),
    status TEXT NOT NULL DEFAULT 'pending_upload'
        CHECK (status IN (
            'pending_upload', 'verifying', 'available', 'failed', 'trashed'
        )),
    current_version_id UUID NOT NULL,
    created_by UUID NOT NULL REFERENCES auth_users(user_id) ON DELETE RESTRICT,
    trashed_by UUID REFERENCES auth_users(user_id) ON DELETE RESTRICT,
    trashed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (cardinality(tags) <= 50),
    CHECK (
        (status = 'trashed' AND trashed_at IS NOT NULL AND trashed_by IS NOT NULL)
        OR (status <> 'trashed' AND trashed_at IS NULL AND trashed_by IS NULL)
    ),
    UNIQUE (project_id, artifact_id)
);

CREATE INDEX artifact_artifacts_project_updated_idx
    ON artifact_artifacts (project_id, updated_at DESC, artifact_id DESC)
    WHERE status <> 'trashed';

CREATE INDEX artifact_artifacts_project_trash_idx
    ON artifact_artifacts (project_id, trashed_at DESC, artifact_id DESC)
    WHERE status = 'trashed';

CREATE INDEX artifact_artifacts_tags_idx
    ON artifact_artifacts USING GIN (tags);

CREATE TABLE artifact_blobs (
    blob_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE RESTRICT,
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    backend TEXT NOT NULL CHECK (backend IN ('local', 'minio', 's3')),
    object_key TEXT NOT NULL UNIQUE CHECK (
        length(object_key) BETWEEN 1 AND 1024
        AND object_key !~ '(^/|(^|/)\.\.(/|$)|\\)'
    ),
    reference_count BIGINT NOT NULL DEFAULT 0 CHECK (reference_count >= 0),
    purge_requested_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, sha256, size_bytes),
    UNIQUE (project_id, blob_id)
);

CREATE INDEX artifact_blobs_purge_idx
    ON artifact_blobs (purge_requested_at, blob_id)
    WHERE reference_count = 0 AND purge_requested_at IS NOT NULL;

CREATE TABLE artifact_versions (
    version_id UUID PRIMARY KEY,
    artifact_id UUID NOT NULL,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE RESTRICT,
    version_no INTEGER NOT NULL CHECK (version_no > 0),
    storage_class TEXT NOT NULL CHECK (storage_class IN ('object', 'git')),
    blob_id UUID,
    repository_id UUID REFERENCES repo_repositories(repository_id) ON DELETE RESTRICT,
    commit_sha TEXT CHECK (
        commit_sha IS NULL OR commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'
    ),
    workspace_kind TEXT CHECK (
        workspace_kind IS NULL OR workspace_kind = 'result'
    ),
    repository_path TEXT CHECK (
        repository_path IS NULL OR (
            length(repository_path) BETWEEN 1 AND 4096
            AND repository_path !~ '(^/|(^|/)\.\.(/|$)|\\)'
        )
    ),
    filename TEXT NOT NULL CHECK (length(filename) BETWEEN 1 AND 255),
    mime_type TEXT NOT NULL CHECK (length(mime_type) BETWEEN 1 AND 255),
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    status TEXT NOT NULL DEFAULT 'pending_upload'
        CHECK (status IN ('pending_upload', 'verifying', 'available', 'failed')),
    error_code TEXT,
    created_by UUID NOT NULL REFERENCES auth_users(user_id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ,
    CHECK (
        (storage_class = 'object'
            AND repository_id IS NULL
            AND commit_sha IS NULL
            AND workspace_kind IS NULL
            AND repository_path IS NULL)
        OR
        (storage_class = 'git'
            AND blob_id IS NULL
            AND repository_id IS NOT NULL
            AND commit_sha IS NOT NULL
            AND workspace_kind = 'result'
            AND repository_path IS NOT NULL)
    ),
    CHECK (
        (status = 'available' AND available_at IS NOT NULL)
        OR (status <> 'available' AND available_at IS NULL)
    ),
    CHECK (
        status <> 'available' OR storage_class = 'git' OR blob_id IS NOT NULL
    ),
    FOREIGN KEY (project_id, artifact_id)
        REFERENCES artifact_artifacts(project_id, artifact_id)
        ON DELETE CASCADE,
    FOREIGN KEY (project_id, blob_id)
        REFERENCES artifact_blobs(project_id, blob_id)
        ON DELETE RESTRICT,
    UNIQUE (artifact_id, version_no),
    UNIQUE (artifact_id, version_id),
    UNIQUE (project_id, version_id)
);

ALTER TABLE artifact_artifacts
    ADD CONSTRAINT artifact_artifacts_current_version_fk
    FOREIGN KEY (artifact_id, current_version_id)
    REFERENCES artifact_versions(artifact_id, version_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX artifact_versions_artifact_timeline_idx
    ON artifact_versions (artifact_id, version_no DESC);

CREATE INDEX artifact_versions_blob_idx
    ON artifact_versions (blob_id)
    WHERE blob_id IS NOT NULL;

CREATE TABLE artifact_uploads (
    upload_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE RESTRICT,
    artifact_id UUID NOT NULL,
    version_id UUID NOT NULL UNIQUE,
    provider_upload_id TEXT NOT NULL CHECK (length(provider_upload_id) BETWEEN 1 AND 2048),
    staging_key TEXT NOT NULL UNIQUE CHECK (
        length(staging_key) BETWEEN 1 AND 1024
        AND staging_key !~ '(^/|(^|/)\.\.(/|$)|\\)'
    ),
    expected_sha256 TEXT NOT NULL CHECK (expected_sha256 ~ '^[0-9a-f]{64}$'),
    expected_size_bytes BIGINT NOT NULL CHECK (expected_size_bytes >= 0),
    mime_type TEXT NOT NULL CHECK (length(mime_type) BETWEEN 1 AND 255),
    part_size_bytes BIGINT NOT NULL CHECK (
        part_size_bytes BETWEEN 5242880 AND 5368709120
    ),
    part_count INTEGER NOT NULL CHECK (part_count BETWEEN 1 AND 10000),
    status TEXT NOT NULL DEFAULT 'initialized'
        CHECK (status IN (
            'initialized', 'uploading', 'completing', 'verifying',
            'completed', 'aborted', 'expired', 'failed'
        )),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    created_by UUID NOT NULL REFERENCES auth_users(user_id) ON DELETE RESTRICT,
    expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    aborted_at TIMESTAMPTZ,
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (
        (status = 'completed' AND completed_at IS NOT NULL AND aborted_at IS NULL)
        OR (status IN ('aborted', 'expired') AND aborted_at IS NOT NULL AND completed_at IS NULL)
        OR (status NOT IN ('completed', 'aborted', 'expired')
            AND completed_at IS NULL AND aborted_at IS NULL)
    ),
    FOREIGN KEY (project_id, artifact_id)
        REFERENCES artifact_artifacts(project_id, artifact_id)
        ON DELETE CASCADE,
    FOREIGN KEY (project_id, version_id)
        REFERENCES artifact_versions(project_id, version_id)
        ON DELETE CASCADE,
    FOREIGN KEY (artifact_id, version_id)
        REFERENCES artifact_versions(artifact_id, version_id)
        ON DELETE CASCADE,
    UNIQUE (project_id, idempotency_key)
);

CREATE INDEX artifact_uploads_expiry_idx
    ON artifact_uploads (expires_at, upload_id)
    WHERE status IN ('initialized', 'uploading', 'completing', 'verifying');

CREATE TABLE artifact_upload_parts (
    upload_id UUID NOT NULL REFERENCES artifact_uploads(upload_id) ON DELETE CASCADE,
    part_number INTEGER NOT NULL CHECK (part_number BETWEEN 1 AND 10000),
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    provider_etag TEXT NOT NULL CHECK (length(provider_etag) BETWEEN 1 AND 1024),
    checksum_sha256 TEXT CHECK (
        checksum_sha256 IS NULL OR checksum_sha256 ~ '^[0-9a-f]{64}$'
    ),
    completed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (upload_id, part_number)
);

CREATE TABLE artifact_previews (
    preview_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE RESTRICT,
    artifact_id UUID NOT NULL,
    version_id UUID NOT NULL,
    preview_type TEXT NOT NULL
        CHECK (preview_type IN ('image', 'pdf', 'csv', 'json', 'text', 'thumbnail')),
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'available', 'failed', 'unsupported')),
    blob_id UUID,
    structure_summary JSONB NOT NULL DEFAULT '{}'::JSONB
        CHECK (jsonb_typeof(structure_summary) = 'object'),
    job_id UUID REFERENCES jobs(job_id) ON DELETE SET NULL,
    error_code TEXT,
    error_message TEXT CHECK (
        error_message IS NULL OR length(error_message) <= 2000
    ),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ,
    FOREIGN KEY (project_id, artifact_id)
        REFERENCES artifact_artifacts(project_id, artifact_id)
        ON DELETE CASCADE,
    FOREIGN KEY (project_id, version_id)
        REFERENCES artifact_versions(project_id, version_id)
        ON DELETE CASCADE,
    FOREIGN KEY (artifact_id, version_id)
        REFERENCES artifact_versions(artifact_id, version_id)
        ON DELETE CASCADE,
    FOREIGN KEY (project_id, blob_id)
        REFERENCES artifact_blobs(project_id, blob_id)
        ON DELETE RESTRICT,
    UNIQUE (version_id, preview_type)
);

CREATE INDEX artifact_previews_job_idx
    ON artifact_previews (job_id)
    WHERE job_id IS NOT NULL;

CREATE TABLE artifact_registry_entries (
    attachment_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE RESTRICT,
    artifact_id UUID NOT NULL,
    version_id UUID,
    source TEXT NOT NULL CHECK (source IN (
        'user_upload', 'experiment', 'model', 'article', 'system'
    )),
    description TEXT CHECK (
        description IS NULL OR length(description) <= 10000
    ),
    recommended_usage TEXT CHECK (
        recommended_usage IS NULL OR length(recommended_usage) <= 10000
    ),
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'hidden')),
    created_by UUID NOT NULL REFERENCES auth_users(user_id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (project_id, artifact_id)
        REFERENCES artifact_artifacts(project_id, artifact_id)
        ON DELETE CASCADE,
    FOREIGN KEY (project_id, version_id)
        REFERENCES artifact_versions(project_id, version_id)
        ON DELETE RESTRICT,
    FOREIGN KEY (artifact_id, version_id)
        REFERENCES artifact_versions(artifact_id, version_id)
        ON DELETE RESTRICT,
    UNIQUE (project_id, artifact_id)
);

CREATE INDEX artifact_registry_entries_project_idx
    ON artifact_registry_entries (project_id, updated_at DESC, attachment_id DESC)
    WHERE status = 'active';

CREATE TABLE artifact_relations (
    relation_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE RESTRICT,
    artifact_id UUID NOT NULL,
    version_id UUID,
    relation_type TEXT NOT NULL
        CHECK (relation_type IN ('source', 'attachment', 'generated', 'output')),
    target_type TEXT NOT NULL
        CHECK (target_type IN ('project', 'experiment', 'model', 'article')),
    target_id UUID NOT NULL,
    created_by UUID NOT NULL REFERENCES auth_users(user_id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (project_id, artifact_id)
        REFERENCES artifact_artifacts(project_id, artifact_id)
        ON DELETE CASCADE,
    FOREIGN KEY (project_id, version_id)
        REFERENCES artifact_versions(project_id, version_id)
        ON DELETE RESTRICT,
    FOREIGN KEY (artifact_id, version_id)
        REFERENCES artifact_versions(artifact_id, version_id)
        ON DELETE RESTRICT,
    UNIQUE NULLS NOT DISTINCT (
        artifact_id, version_id, relation_type, target_type, target_id
    )
);

CREATE INDEX artifact_relations_target_idx
    ON artifact_relations (project_id, target_type, target_id, relation_type);

CREATE OR REPLACE FUNCTION reject_available_artifact_version_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status = 'available' THEN
        RAISE EXCEPTION 'available artifact versions are immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER artifact_versions_immutable
    BEFORE UPDATE ON artifact_versions
    FOR EACH ROW EXECUTE FUNCTION reject_available_artifact_version_mutation();

CREATE OR REPLACE FUNCTION maintain_artifact_blob_reference_count()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') AND OLD.blob_id IS NOT NULL
        AND (TG_OP = 'DELETE' OR NEW.blob_id IS DISTINCT FROM OLD.blob_id) THEN
        UPDATE artifact_blobs
        SET reference_count = reference_count - 1,
            purge_requested_at = CASE
                WHEN reference_count = 1
                    THEN COALESCE(purge_requested_at, CURRENT_TIMESTAMP)
                ELSE purge_requested_at
            END,
            updated_at = CURRENT_TIMESTAMP
        WHERE blob_id = OLD.blob_id
          AND project_id = OLD.project_id
          AND reference_count > 0;
    END IF;

    IF TG_OP IN ('INSERT', 'UPDATE') AND NEW.blob_id IS NOT NULL
        AND (TG_OP = 'INSERT' OR NEW.blob_id IS DISTINCT FROM OLD.blob_id) THEN
        UPDATE artifact_blobs
        SET reference_count = reference_count + 1,
            purge_requested_at = NULL,
            updated_at = CURRENT_TIMESTAMP
        WHERE blob_id = NEW.blob_id
          AND project_id = NEW.project_id;
    END IF;

    RETURN NULL;
END;
$$;

CREATE TRIGGER artifact_versions_blob_reference_count
    AFTER INSERT OR UPDATE OF blob_id OR DELETE ON artifact_versions
    FOR EACH ROW EXECUTE FUNCTION maintain_artifact_blob_reference_count();

CREATE TRIGGER artifact_previews_blob_reference_count
    AFTER INSERT OR UPDATE OF blob_id OR DELETE ON artifact_previews
    FOR EACH ROW EXECUTE FUNCTION maintain_artifact_blob_reference_count();
