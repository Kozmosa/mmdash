ALTER TABLE auth_users
    ADD COLUMN system_role TEXT NOT NULL DEFAULT 'member'
        CHECK (system_role IN ('admin', 'member'));

UPDATE auth_users
SET system_role = 'admin',
    updated_at = NOW()
WHERE user_id = (
    SELECT user_id
    FROM auth_users
    ORDER BY created_at, user_id
    LIMIT 1
);

CREATE TABLE settings_values (
    setting_id UUID PRIMARY KEY,
    scope_type TEXT NOT NULL
        CHECK (scope_type IN ('system', 'project')),
    scope_id TEXT NOT NULL,
    type_key TEXT NOT NULL,
    public_values JSONB NOT NULL DEFAULT '{}'::JSONB,
    encrypted_secrets JSONB NOT NULL DEFAULT '{}'::JSONB,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (scope_type, scope_id, type_key)
);

CREATE INDEX settings_values_scope_idx
    ON settings_values (scope_type, scope_id, type_key);
