DROP TABLE IF EXISTS system_event_replays;
DROP TABLE IF EXISTS system_event_failures;
DROP TABLE IF EXISTS system_event_consumptions;
DROP TABLE IF EXISTS system_event_deliveries;

DROP INDEX IF EXISTS system_outbox_lease_idx;
DROP INDEX IF EXISTS system_outbox_dispatch_idx;

ALTER TABLE system_outbox
    DROP COLUMN IF EXISTS failed_at,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS locked_by,
    DROP COLUMN IF EXISTS max_attempts,
    DROP COLUMN IF EXISTS status;

CREATE INDEX IF NOT EXISTS system_outbox_pending_idx
    ON system_outbox (available_at, occurred_at)
    WHERE published_at IS NULL;
