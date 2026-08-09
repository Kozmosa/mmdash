CREATE TABLE model_notion_oauth_authorizations (
    authorization_id UUID PRIMARY KEY,
    state_hash TEXT NOT NULL UNIQUE CHECK (state_hash ~ '^[0-9a-f]{64}$'),
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES auth_users(user_id) ON DELETE CASCADE,
    root_page_id UUID NOT NULL,
    root_page_url TEXT NOT NULL,
    auto_sync_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    auto_sync_interval_seconds INTEGER NOT NULL DEFAULT 300
        CHECK (auto_sync_interval_seconds BETWEEN 60 AND 86400),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','exchanging','succeeded','denied','failed')),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX model_notion_oauth_authorizations_active_idx
    ON model_notion_oauth_authorizations (expires_at, status)
    WHERE status = 'pending';
