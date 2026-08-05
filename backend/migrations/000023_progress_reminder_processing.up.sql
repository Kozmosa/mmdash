ALTER TABLE progress_reminders
    DROP CONSTRAINT IF EXISTS progress_reminders_status_check;

ALTER TABLE progress_reminders
    ADD COLUMN available_at TIMESTAMPTZ,
    ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 5,
    ADD COLUMN locked_by TEXT,
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN last_error_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN last_error_message TEXT NOT NULL DEFAULT '';

UPDATE progress_reminders
SET available_at = remind_at
WHERE available_at IS NULL;

ALTER TABLE progress_reminders
    ALTER COLUMN available_at SET NOT NULL,
    ADD CONSTRAINT progress_reminders_status_check
        CHECK (status IN ('pending', 'processing', 'triggered', 'failed', 'cancelled')),
    ADD CONSTRAINT progress_reminders_attempts_check
        CHECK (attempts >= 0 AND attempts <= max_attempts),
    ADD CONSTRAINT progress_reminders_max_attempts_check
        CHECK (max_attempts BETWEEN 1 AND 100),
    ADD CONSTRAINT progress_reminders_lease_check
        CHECK (
            (status = 'processing' AND locked_by IS NOT NULL AND lease_expires_at IS NOT NULL)
            OR
            (status <> 'processing' AND locked_by IS NULL AND lease_expires_at IS NULL)
        ),
    ADD CONSTRAINT progress_reminders_error_code_check
        CHECK (length(last_error_code) <= 100),
    ADD CONSTRAINT progress_reminders_error_message_check
        CHECK (length(last_error_message) <= 500);

CREATE INDEX progress_reminders_queue_idx
    ON progress_reminders (available_at, remind_at, reminder_id)
    WHERE status = 'pending';

CREATE INDEX progress_reminders_lease_idx
    ON progress_reminders (lease_expires_at, reminder_id)
    WHERE status = 'processing';
