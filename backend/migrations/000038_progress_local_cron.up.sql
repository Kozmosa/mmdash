ALTER TABLE progress_settings
    ADD COLUMN cron_next_run_at TIMESTAMPTZ,
    ADD COLUMN cron_last_scheduled_at TIMESTAMPTZ;

UPDATE progress_settings
SET cron_next_run_at = NOW()
WHERE auto_tracking_enabled AND cron_enabled;

ALTER TABLE progress_settings
    DROP COLUMN cron_remote_job_id,
    DROP COLUMN cron_sync_status,
    DROP COLUMN cron_error_code,
    DROP COLUMN cron_synced_at;

CREATE INDEX progress_settings_local_cron_due_idx
    ON progress_settings (cron_next_run_at, project_id)
    WHERE auto_tracking_enabled AND cron_enabled;
