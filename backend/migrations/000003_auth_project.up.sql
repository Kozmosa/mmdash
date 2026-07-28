CREATE TABLE auth_users (
    user_id UUID PRIMARY KEY,
    email TEXT NOT NULL,
    display_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX auth_users_email_unique_idx
    ON auth_users (LOWER(email));

CREATE TABLE auth_sessions (
    session_id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES auth_users(user_id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX auth_sessions_active_idx
    ON auth_sessions (user_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE projects (
    project_id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    problem_title TEXT NOT NULL DEFAULT '',
    problem_summary TEXT NOT NULL DEFAULT '',
    project_constraints JSONB NOT NULL DEFAULT '[]'::JSONB,
    source_artifact_ids JSONB NOT NULL DEFAULT '[]'::JSONB,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE project_members (
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES auth_users(user_id) ON DELETE CASCADE,
    role TEXT NOT NULL
        CHECK (role IN ('owner', 'maintainer', 'editor', 'viewer', 'agent', 'box')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX project_members_user_idx
    ON project_members (user_id, project_id);

CREATE TABLE auth_tokens (
    token_id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES auth_users(user_id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(project_id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('api', 'agent', 'box')),
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX auth_tokens_active_idx
    ON auth_tokens (user_id, kind)
    WHERE revoked_at IS NULL;
