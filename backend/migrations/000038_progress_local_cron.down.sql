DROP INDEX IF EXISTS progress_settings_local_cron_due_idx;

ALTER TABLE progress_settings
    ADD COLUMN cron_remote_job_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN cron_sync_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (cron_sync_status IN ('pending', 'syncing', 'ready', 'failed', 'disabled')),
    ADD COLUMN cron_error_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN cron_synced_at TIMESTAMPTZ;

UPDATE progress_settings
SET cron_sync_status = CASE
    WHEN auto_tracking_enabled AND cron_enabled THEN 'pending'
    ELSE 'disabled'
END;

ALTER TABLE progress_settings
    DROP COLUMN cron_last_scheduled_at,
    DROP COLUMN cron_next_run_at;
