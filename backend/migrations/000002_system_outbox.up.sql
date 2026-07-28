CREATE TABLE IF NOT EXISTS system_outbox (
    event_id UUID PRIMARY KEY,
    event_type TEXT NOT NULL,
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    occurred_at TIMESTAMPTZ NOT NULL,
    producer TEXT NOT NULL,
    project_id TEXT,
    actor JSONB,
    correlation_id TEXT,
    causation_id TEXT,
    payload JSONB NOT NULL,
    published_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT
);

CREATE INDEX IF NOT EXISTS system_outbox_pending_idx
    ON system_outbox (available_at, occurred_at)
    WHERE published_at IS NULL;
