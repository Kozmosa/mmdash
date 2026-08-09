ALTER TABLE agent_runs DROP CONSTRAINT IF EXISTS agent_runs_source_check;
ALTER TABLE agent_runs
    ADD CONSTRAINT agent_runs_source_check
    CHECK (source IN ('message', 'regenerate', 'rerun'));

DROP TABLE IF EXISTS progress_evaluation_risks;
DROP TABLE IF EXISTS progress_tracker_state;
DROP TABLE IF EXISTS progress_stage_overrides;

DROP INDEX IF EXISTS progress_proposals_evaluation_source_unique_idx;
DROP INDEX IF EXISTS progress_tasks_automatic_source_unique_idx;
ALTER TABLE progress_proposals DROP CONSTRAINT IF EXISTS progress_proposals_source_evaluation_fk;
ALTER TABLE progress_tasks DROP CONSTRAINT IF EXISTS progress_tasks_source_evaluation_fk;
ALTER TABLE progress_evaluation_requests DROP CONSTRAINT IF EXISTS progress_evaluation_requests_merged_evaluation_fk;

DROP TABLE IF EXISTS progress_evaluations;
DROP TABLE IF EXISTS progress_evaluation_triggers;
DROP TABLE IF EXISTS progress_evaluation_requests;

ALTER TABLE progress_proposals
    DROP COLUMN IF EXISTS source_key,
    DROP COLUMN IF EXISTS source_evaluation_id;

ALTER TABLE progress_tasks
    DROP COLUMN IF EXISTS source_key,
    DROP COLUMN IF EXISTS source_evaluation_id,
    DROP COLUMN IF EXISTS manual_override_fields;

ALTER TABLE progress_settings
    DROP COLUMN IF EXISTS cron_lease_expires_at,
    DROP COLUMN IF EXISTS cron_lease_owner,
    DROP COLUMN IF EXISTS cron_retry_at,
    DROP COLUMN IF EXISTS cron_synced_at,
    DROP COLUMN IF EXISTS cron_error_code,
    DROP COLUMN IF EXISTS cron_sync_status,
    DROP COLUMN IF EXISTS cron_remote_job_id,
    DROP COLUMN IF EXISTS agent_instance_id,
    DROP COLUMN IF EXISTS min_interval_seconds,
    DROP COLUMN IF EXISTS debounce_seconds,
    DROP COLUMN IF EXISTS cron_schedule,
    DROP COLUMN IF EXISTS cron_enabled,
    DROP COLUMN IF EXISTS event_triggers_enabled,
    DROP COLUMN IF EXISTS auto_tracking_enabled;
