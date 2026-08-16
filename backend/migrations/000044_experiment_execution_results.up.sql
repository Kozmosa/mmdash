ALTER TABLE experiments
    RENAME COLUMN status TO execution_status;
ALTER TABLE experiments
    RENAME COLUMN runtime TO requested_runtime_policy;
ALTER TABLE experiments
    DROP CONSTRAINT experiments_status_check;
ALTER TABLE experiments
    DROP CONSTRAINT experiments_runtime_check;
ALTER TABLE experiments
    ADD COLUMN experiment_type TEXT NOT NULL DEFAULT 'box'
        CHECK (experiment_type IN ('box', 'box-re', 'self')),
    ADD COLUMN connectivity_status TEXT
        CHECK (connectivity_status IS NULL OR connectivity_status IN ('online', 'box_offline')),
    ADD COLUMN requested_box_id UUID REFERENCES box_nodes(box_id) ON DELETE SET NULL,
    ADD COLUMN actual_runtime TEXT
        CHECK (actual_runtime IS NULL OR actual_runtime IN ('local-docker', 'e2b')),
    ADD COLUMN runtime_version TEXT,
    ADD COLUMN project_timezone TEXT NOT NULL DEFAULT 'UTC',
    ADD COLUMN result_directory TEXT,
    ADD COLUMN result_commit_sha TEXT
        CHECK (result_commit_sha IS NULL OR result_commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    ADD COLUMN staging_commit_sha TEXT
        CHECK (staging_commit_sha IS NULL OR staging_commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    ADD COLUMN revert_commit_sha TEXT
        CHECK (revert_commit_sha IS NULL OR revert_commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    ADD COLUMN result_manifest_sha256 TEXT
        CHECK (result_manifest_sha256 IS NULL OR result_manifest_sha256 ~ '^[0-9a-f]{64}$'),
    ADD COLUMN execution_bundle_artifact_id UUID REFERENCES artifact_artifacts(artifact_id) ON DELETE SET NULL,
    ADD COLUMN execution_bundle_version_id UUID REFERENCES artifact_versions(version_id) ON DELETE SET NULL,
    ADD COLUMN failure_stage TEXT,
    ADD COLUMN failed_at TIMESTAMPTZ,
    ADD COLUMN retryable BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN failure_attempt INTEGER NOT NULL DEFAULT 0 CHECK (failure_attempt >= 0),
    ADD COLUMN cleanup_result JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN retry_of_experiment_id UUID REFERENCES experiments(experiment_id) ON DELETE SET NULL,
    ADD COLUMN root_experiment_id UUID REFERENCES experiments(experiment_id) ON DELETE SET NULL,
    ADD COLUMN superseded_by_experiment_id UUID REFERENCES experiments(experiment_id) ON DELETE SET NULL,
    ADD COLUMN latest_experiment_id UUID REFERENCES experiments(experiment_id) ON DELETE SET NULL,
    ADD COLUMN retry_sequence INTEGER NOT NULL DEFAULT 0 CHECK (retry_sequence >= 0),
    ADD COLUMN result_processing JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN run_idempotency_key TEXT,
    ADD COLUMN result_bind_idempotency_key TEXT,
    ADD CONSTRAINT experiments_execution_status_check CHECK (
        execution_status IN (
            'created', 'queued', 'preparing', 'running', 'uploading',
            'processing_result', 'awaiting_result', 'verifying_result',
            'succeeded', 'failed', 'canceled', 'timed_out', 'archived'
        )
    ),
    ADD CONSTRAINT experiments_requested_runtime_policy_check CHECK (
        requested_runtime_policy IN ('auto', 'local-docker', 'e2b')
    ),
    ADD CONSTRAINT experiments_type_state_check CHECK (
        (
            experiment_type = 'self'
            AND execution_status IN (
                'created', 'awaiting_result', 'verifying_result',
                'succeeded', 'failed', 'canceled', 'archived'
            )
        )
        OR (
            experiment_type IN ('box', 'box-re')
            AND execution_status IN (
                'created', 'queued', 'preparing', 'running', 'uploading',
                'processing_result', 'succeeded', 'failed', 'canceled',
                'timed_out', 'archived'
            )
        )
    ),
    ADD CONSTRAINT experiments_result_directory_check CHECK (
        result_directory IS NULL
        OR result_directory ~ '^experiments/[0-9a-f-]+_[0-9]{8}_[0-9]{4}/$'
    ),
    ADD CONSTRAINT experiments_run_idempotency_key_check CHECK (
        run_idempotency_key IS NULL OR length(run_idempotency_key) BETWEEN 1 AND 200
    ),
    ADD CONSTRAINT experiments_result_bind_idempotency_key_check CHECK (
        result_bind_idempotency_key IS NULL
        OR length(result_bind_idempotency_key) BETWEEN 1 AND 200
    );

CREATE TABLE experiment_project_settings (
    project_id UUID PRIMARY KEY REFERENCES projects(project_id) ON DELETE CASCADE,
    timezone TEXT NOT NULL DEFAULT 'UTC'
        CHECK (length(timezone) BETWEEN 1 AND 100),
    default_runtime_policy TEXT NOT NULL DEFAULT 'auto'
        CHECK (default_runtime_policy IN ('auto', 'local-docker', 'e2b')),
    default_limits JSONB NOT NULL DEFAULT
        '{"cpu_millis":1000,"memory_bytes":1073741824,"timeout_seconds":3600,"disk_bytes":10737418240,"pids":256,"network":"disabled"}'::jsonb,
    git_large_file_threshold_bytes BIGINT NOT NULL DEFAULT 52428800
        CHECK (git_large_file_threshold_bytes BETWEEN 1 AND 5368709120),
    updated_by UUID NOT NULL REFERENCES auth_users(user_id),
    updated_at TIMESTAMPTZ NOT NULL
);

INSERT INTO experiment_project_settings (
    project_id,
    timezone,
    default_runtime_policy,
    default_limits,
    git_large_file_threshold_bytes,
    updated_by,
    updated_at
)
SELECT
    project_id,
    'UTC',
    'auto',
    '{"cpu_millis":1000,"memory_bytes":1073741824,"timeout_seconds":3600,"disk_bytes":10737418240,"pids":256,"network":"disabled"}'::jsonb,
    52428800,
    created_by,
    updated_at
FROM projects;

UPDATE experiments
SET experiment_type = 'box',
    connectivity_status = CASE
        WHEN execution_status IN ('preparing', 'running') THEN 'online'
        ELSE NULL
    END,
    actual_runtime = requested_runtime_policy,
    requested_box_id = box_id,
    project_timezone = 'UTC',
    result_directory = 'experiments/' || experiment_id::text || '_' ||
        to_char(created_at AT TIME ZONE 'UTC', 'YYYYMMDD_HH24MI') || '/',
    execution_bundle_artifact_id = CASE
        WHEN COALESCE(result_artifact->>'artifact_id', '') ~
             '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
        THEN (result_artifact->>'artifact_id')::uuid
        ELSE NULL
    END,
    execution_bundle_version_id = CASE
        WHEN COALESCE(result_artifact->>'version_id', '') ~
             '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
        THEN (result_artifact->>'version_id')::uuid
        ELSE NULL
    END,
    execution_status = CASE
        WHEN execution_status = 'succeeded' THEN 'processing_result'
        ELSE execution_status
    END,
    finished_at = CASE
        WHEN execution_status = 'succeeded' THEN NULL
        ELSE finished_at
    END,
    result_processing = CASE
        WHEN execution_status = 'succeeded'
        THEN '{"legacy_result_migration":true}'::jsonb
        ELSE result_processing
    END,
    root_experiment_id = experiment_id,
    latest_experiment_id = experiment_id;

ALTER TABLE experiments
    ALTER COLUMN result_directory SET NOT NULL;

DROP INDEX experiments_project_status_idx;
CREATE INDEX experiments_project_status_idx
    ON experiments (project_id, execution_status, updated_at DESC);
CREATE INDEX experiments_retry_root_idx
    ON experiments (root_experiment_id, retry_sequence, experiment_id);
CREATE INDEX experiments_connectivity_idx
    ON experiments (connectivity_status, updated_at, experiment_id)
    WHERE connectivity_status = 'box_offline';

ALTER TABLE box_tasks
    DROP CONSTRAINT box_tasks_status_check;
ALTER TABLE box_tasks
    ADD CONSTRAINT box_tasks_status_check CHECK (
        status IN (
            'queued', 'preparing', 'running', 'uploading', 'processing_result',
            'succeeded', 'failed', 'canceled', 'timed_out'
        )
    ),
    ADD COLUMN execution_bundle_artifact_id UUID REFERENCES artifact_artifacts(artifact_id) ON DELETE SET NULL,
    ADD COLUMN execution_bundle_version_id UUID REFERENCES artifact_versions(version_id) ON DELETE SET NULL,
    ADD COLUMN result_manifest_sha256 TEXT
        CHECK (result_manifest_sha256 IS NULL OR result_manifest_sha256 ~ '^[0-9a-f]{64}$'),
    ADD COLUMN failure_stage TEXT,
    ADD COLUMN failure_retryable BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN failure_cleanup_result JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE box_tasks
SET execution_bundle_artifact_id = CASE
        WHEN COALESCE(result_artifact->>'artifact_id', '') ~
             '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
        THEN (result_artifact->>'artifact_id')::uuid
        ELSE NULL
    END,
    execution_bundle_version_id = CASE
        WHEN COALESCE(result_artifact->>'version_id', '') ~
             '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
        THEN (result_artifact->>'version_id')::uuid
        ELSE NULL
    END,
    status = CASE
        WHEN status = 'succeeded' THEN 'processing_result'
        ELSE status
    END,
    finished_at = CASE
        WHEN status = 'succeeded' THEN NULL
        ELSE finished_at
    END;

CREATE TABLE experiment_result_files (
    experiment_id UUID NOT NULL REFERENCES experiments(experiment_id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    path TEXT NOT NULL CHECK (
        length(path) BETWEEN 1 AND 4096
        AND path !~ '(^/|(^|/)\.\.(/|$)|\\)'
    ),
    storage_kind TEXT NOT NULL CHECK (storage_kind IN ('git', 'artifact')),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    media_type TEXT NOT NULL CHECK (length(media_type) BETWEEN 1 AND 255),
    artifact_id UUID REFERENCES artifact_artifacts(artifact_id) ON DELETE RESTRICT,
    artifact_version_id UUID REFERENCES artifact_versions(version_id) ON DELETE RESTRICT,
    repository_path TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (experiment_id, path),
    CHECK (
        (storage_kind = 'git' AND artifact_id IS NULL AND artifact_version_id IS NULL AND repository_path IS NOT NULL)
        OR
        (storage_kind = 'artifact' AND artifact_id IS NOT NULL AND artifact_version_id IS NOT NULL)
    )
);
CREATE INDEX experiment_result_files_project_idx
    ON experiment_result_files (project_id, experiment_id, path);
