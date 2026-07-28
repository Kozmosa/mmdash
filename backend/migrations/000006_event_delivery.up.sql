ALTER TABLE system_outbox
    ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'publishing', 'published', 'failed')),
    ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 10
        CHECK (max_attempts BETWEEN 1 AND 100),
    ADD COLUMN locked_by TEXT,
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN failed_at TIMESTAMPTZ;

UPDATE system_outbox
SET status = 'published'
WHERE published_at IS NOT NULL;

DROP INDEX IF EXISTS system_outbox_pending_idx;

CREATE INDEX system_outbox_dispatch_idx
    ON system_outbox (available_at, occurred_at, event_id)
    WHERE status = 'pending';

CREATE INDEX system_outbox_lease_idx
    ON system_outbox (lease_expires_at)
    WHERE status = 'publishing';

CREATE TABLE system_event_deliveries (
    delivery_id UUID PRIMARY KEY,
    event_id UUID NOT NULL REFERENCES system_outbox(event_id) ON DELETE CASCADE,
    consumer_name TEXT NOT NULL,
    delivery_key TEXT NOT NULL DEFAULT 'live',
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'succeeded', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 5
        CHECK (max_attempts BETWEEN 1 AND 100),
    available_at TIMESTAMPTZ NOT NULL,
    locked_by TEXT,
    lease_expires_at TIMESTAMPTZ,
    last_error TEXT,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (event_id, consumer_name, delivery_key)
);

CREATE INDEX system_event_deliveries_dispatch_idx
    ON system_event_deliveries (available_at, created_at, delivery_id)
    WHERE status = 'pending';

CREATE INDEX system_event_deliveries_lease_idx
    ON system_event_deliveries (lease_expires_at)
    WHERE status = 'processing';

CREATE TABLE system_event_consumptions (
    event_id UUID NOT NULL REFERENCES system_outbox(event_id) ON DELETE CASCADE,
    consumer_name TEXT NOT NULL,
    delivery_key TEXT NOT NULL,
    delivery_id UUID NOT NULL
        REFERENCES system_event_deliveries(delivery_id) ON DELETE CASCADE,
    consumed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (event_id, consumer_name, delivery_key)
);

CREATE TABLE system_event_failures (
    failure_id UUID PRIMARY KEY,
    delivery_id UUID NOT NULL
        REFERENCES system_event_deliveries(delivery_id) ON DELETE CASCADE,
    event_id UUID NOT NULL REFERENCES system_outbox(event_id) ON DELETE CASCADE,
    consumer_name TEXT NOT NULL,
    delivery_key TEXT NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    error_message TEXT NOT NULL,
    failed_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX system_event_failures_event_idx
    ON system_event_failures (event_id, failed_at, failure_id);

CREATE TABLE system_event_replays (
    replay_id UUID PRIMARY KEY,
    event_id UUID NOT NULL REFERENCES system_outbox(event_id) ON DELETE CASCADE,
    consumer_name TEXT,
    requested_by UUID NOT NULL REFERENCES auth_users(user_id),
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX system_event_replays_event_idx
    ON system_event_replays (event_id, created_at, replay_id);
