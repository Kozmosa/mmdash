CREATE TABLE IF NOT EXISTS notification_invitation_outcomes (
    invitation_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    outcome TEXT NOT NULL DEFAULT 'active'
        CHECK (outcome IN ('active', 'resolved', 'revoked', 'expired')),
    source_event_id UUID NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
