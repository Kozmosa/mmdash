ALTER TABLE auth_sessions
    ADD COLUMN refresh_token_hash TEXT;

CREATE UNIQUE INDEX auth_sessions_refresh_token_hash_unique_idx
    ON auth_sessions (refresh_token_hash)
    WHERE refresh_token_hash IS NOT NULL;

CREATE TABLE auth_device_authorizations (
    authorization_id UUID PRIMARY KEY,
    device_code_hash TEXT NOT NULL UNIQUE,
    user_code_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'denied', 'consumed')),
    user_id UUID REFERENCES auth_users(user_id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    approved_at TIMESTAMPTZ,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX auth_device_authorizations_active_idx
    ON auth_device_authorizations (expires_at, status)
    WHERE status IN ('pending', 'approved');
