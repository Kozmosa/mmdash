DROP TABLE experiment_result_files;
DROP TABLE experiment_project_settings;

ALTER TABLE box_tasks
    DROP COLUMN failure_cleanup_result,
    DROP COLUMN failure_retryable,
    DROP COLUMN failure_stage,
    DROP COLUMN result_manifest_sha256,
    DROP COLUMN execution_bundle_version_id,
    DROP COLUMN execution_bundle_artifact_id,
    DROP CONSTRAINT box_tasks_status_check;
UPDATE box_tasks SET status = 'running' WHERE status IN ('uploading', 'processing_result');
ALTER TABLE box_tasks
    ADD CONSTRAINT box_tasks_status_check
        CHECK (status IN ('queued', 'preparing', 'running', 'succeeded', 'failed', 'canceled', 'timed_out'));

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM experiments WHERE experiment_type <> 'box') THEN
        RAISE EXCEPTION 'cannot downgrade: self or rerun Experiments exist';
    END IF;
END
$$;

DROP INDEX experiments_connectivity_idx;
DROP INDEX experiments_retry_root_idx;
DROP INDEX experiments_project_status_idx;
UPDATE experiments
SET execution_status = CASE
        WHEN execution_status IN ('uploading', 'processing_result') THEN 'running'
        ELSE execution_status
    END,
    requested_runtime_policy = CASE
        WHEN requested_runtime_policy = 'auto' THEN COALESCE(actual_runtime, 'e2b')
        ELSE requested_runtime_policy
    END;
ALTER TABLE experiments
    DROP CONSTRAINT experiments_result_directory_check,
    DROP CONSTRAINT experiments_run_idempotency_key_check,
    DROP CONSTRAINT experiments_result_bind_idempotency_key_check,
    DROP CONSTRAINT experiments_type_state_check,
    DROP CONSTRAINT experiments_requested_runtime_policy_check,
    DROP CONSTRAINT experiments_execution_status_check,
    DROP COLUMN result_bind_idempotency_key,
    DROP COLUMN run_idempotency_key,
    DROP COLUMN result_processing,
    DROP COLUMN retry_sequence,
    DROP COLUMN latest_experiment_id,
    DROP COLUMN superseded_by_experiment_id,
    DROP COLUMN root_experiment_id,
    DROP COLUMN retry_of_experiment_id,
    DROP COLUMN cleanup_result,
    DROP COLUMN failure_attempt,
    DROP COLUMN retryable,
    DROP COLUMN failed_at,
    DROP COLUMN failure_stage,
    DROP COLUMN execution_bundle_version_id,
    DROP COLUMN execution_bundle_artifact_id,
    DROP COLUMN result_manifest_sha256,
    DROP COLUMN revert_commit_sha,
    DROP COLUMN staging_commit_sha,
    DROP COLUMN result_commit_sha,
    DROP COLUMN result_directory,
    DROP COLUMN project_timezone,
    DROP COLUMN runtime_version,
    DROP COLUMN actual_runtime,
    DROP COLUMN requested_box_id,
    DROP COLUMN connectivity_status,
    DROP COLUMN experiment_type,
    ADD CONSTRAINT experiments_status_check
        CHECK (execution_status IN ('created', 'queued', 'preparing', 'running', 'succeeded', 'failed', 'canceled', 'archived')),
    ADD CONSTRAINT experiments_runtime_check
        CHECK (requested_runtime_policy IN ('local-docker', 'e2b'));
ALTER TABLE experiments
    RENAME COLUMN requested_runtime_policy TO runtime;
ALTER TABLE experiments
    RENAME COLUMN execution_status TO status;
CREATE INDEX experiments_project_status_idx
    ON experiments (project_id, status, updated_at DESC);
