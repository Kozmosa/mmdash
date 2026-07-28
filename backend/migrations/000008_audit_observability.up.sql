CREATE TABLE IF NOT EXISTS audit_events (
    audit_id UUID PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,
    request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
    actor_id UUID,
    actor_kind TEXT NOT NULL DEFAULT 'anonymous',
    project_id UUID,
    category TEXT NOT NULL CHECK (category ~ '^[a-z][a-z0-9-]*$'),
    action TEXT NOT NULL CHECK (action ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$'),
    outcome TEXT NOT NULL CHECK (outcome IN ('success', 'denied', 'error')),
    source TEXT NOT NULL CHECK (source ~ '^[a-z][a-z0-9-]*$'),
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    duration_ms BIGINT CHECK (duration_ms IS NULL OR duration_ms >= 0),
    error_code TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS audit_events_occurred_idx
    ON audit_events (occurred_at DESC, audit_id DESC);
CREATE INDEX IF NOT EXISTS audit_events_project_occurred_idx
    ON audit_events (project_id, occurred_at DESC, audit_id DESC);
CREATE INDEX IF NOT EXISTS audit_events_actor_occurred_idx
    ON audit_events (actor_id, occurred_at DESC, audit_id DESC);
CREATE INDEX IF NOT EXISTS audit_events_request_idx
    ON audit_events (request_id);
CREATE INDEX IF NOT EXISTS audit_events_category_action_idx
    ON audit_events (category, action, occurred_at DESC);

CREATE OR REPLACE FUNCTION reject_audit_event_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only';
END;
$$;

DROP TRIGGER IF EXISTS audit_events_append_only ON audit_events;
CREATE TRIGGER audit_events_append_only
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION reject_audit_event_mutation();
