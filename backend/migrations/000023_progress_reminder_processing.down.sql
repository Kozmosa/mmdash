DROP INDEX IF EXISTS progress_reminders_lease_idx;
DROP INDEX IF EXISTS progress_reminders_queue_idx;

ALTER TABLE progress_reminders
    DROP CONSTRAINT IF EXISTS progress_reminders_error_message_check,
    DROP CONSTRAINT IF EXISTS progress_reminders_error_code_check,
    DROP CONSTRAINT IF EXISTS progress_reminders_lease_check,
    DROP CONSTRAINT IF EXISTS progress_reminders_max_attempts_check,
    DROP CONSTRAINT IF EXISTS progress_reminders_attempts_check,
    DROP CONSTRAINT IF EXISTS progress_reminders_status_check;

UPDATE progress_reminders
SET status = CASE WHEN status = 'failed' THEN 'cancelled' ELSE 'pending' END,
    triggered_at = NULL,
    locked_by = NULL,
    lease_expires_at = NULL
WHERE status IN ('processing', 'failed');

ALTER TABLE progress_reminders
    DROP COLUMN last_error_message,
    DROP COLUMN last_error_code,
    DROP COLUMN lease_expires_at,
    DROP COLUMN locked_by,
    DROP COLUMN max_attempts,
    DROP COLUMN attempts,
    DROP COLUMN available_at,
    ADD CONSTRAINT progress_reminders_status_check
        CHECK (status IN ('pending', 'triggered', 'cancelled'));
