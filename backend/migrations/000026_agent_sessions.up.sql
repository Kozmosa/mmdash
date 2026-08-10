-- Stage 5 Agent sessions and resource-scoped Agent settings.
ALTER TABLE settings_values
    ADD COLUMN resource_id TEXT NOT NULL DEFAULT '';

ALTER TABLE settings_values
    DROP CONSTRAINT settings_values_scope_type_scope_id_type_key_key;

ALTER TABLE settings_values
    ADD CONSTRAINT settings_values_scope_resource_key
    UNIQUE (scope_type, scope_id, type_key, resource_id);

DROP INDEX settings_values_scope_idx;

CREATE INDEX settings_values_scope_idx
    ON settings_values (scope_type, scope_id, type_key, resource_id);

CREATE TABLE agent_instances (
    agent_instance_id UUID PRIMARY KEY,
    adapter_type TEXT NOT NULL CHECK (adapter_type IN ('hermes')),
    display_name TEXT NOT NULL,
    management_mode TEXT NOT NULL CHECK (management_mode IN ('manual', 'auto')),
    runtime_url TEXT NOT NULL,
    dashboard_url TEXT,
    status TEXT NOT NULL CHECK (
        status IN ('setup_pending', 'configuring', 'active', 'degraded', 'disabled')
    ),
    management_path TEXT NOT NULL DEFAULT 'unreachable' CHECK (
        management_path IN (
            'direct', 'cloudflare_access', 'unreachable', 'unsupported_auth'
        )
    ),
    capabilities JSONB NOT NULL DEFAULT '{}'::JSONB,
    runtime_check JSONB NOT NULL DEFAULT '{}'::JSONB,
    management_check JSONB NOT NULL DEFAULT '{}'::JSONB,
    project_access_check JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    disabled_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0)
);

CREATE TABLE agent_project_grants (
    grant_id UUID PRIMARY KEY,
    agent_instance_id UUID NOT NULL REFERENCES agent_instances(agent_instance_id),
    project_id UUID NOT NULL REFERENCES projects(project_id),
    role TEXT NOT NULL DEFAULT 'agent' CHECK (role = 'agent'),
    status TEXT NOT NULL CHECK (status IN ('active', 'revoked')),
    allowed_tools JSONB NOT NULL DEFAULT '[]'::JSONB,
    remote_access_id TEXT,
    prompt_override TEXT,
    default_session_id UUID,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    last_access_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (agent_instance_id, project_id)
);

CREATE INDEX agent_project_grants_project_idx
    ON agent_project_grants (project_id, status, updated_at DESC);

ALTER TABLE agent_project_grants
    ADD CONSTRAINT agent_project_grants_token_binding_unique
    UNIQUE (grant_id, agent_instance_id, project_id);

CREATE TABLE agent_sessions (
    session_id UUID PRIMARY KEY,
    grant_id UUID NOT NULL REFERENCES agent_project_grants(grant_id),
    agent_instance_id UUID NOT NULL REFERENCES agent_instances(agent_instance_id),
    project_id UUID NOT NULL REFERENCES projects(project_id),
    remote_session_id TEXT NOT NULL,
    session_type TEXT NOT NULL CHECK (
        session_type IN ('main', 'progress', 'experiment')
    ),
    title TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'ended')),
    parent_session_id UUID REFERENCES agent_sessions(session_id),
    end_reason TEXT,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    last_message_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (agent_instance_id, remote_session_id),
    CONSTRAINT agent_sessions_grant_binding_fk
    FOREIGN KEY (grant_id, agent_instance_id, project_id)
        REFERENCES agent_project_grants(grant_id, agent_instance_id, project_id)
);

ALTER TABLE agent_project_grants
    ADD CONSTRAINT agent_project_grants_default_session_fk
    FOREIGN KEY (default_session_id) REFERENCES agent_sessions(session_id)
    ON DELETE SET NULL;

CREATE INDEX agent_sessions_project_idx
    ON agent_sessions (project_id, updated_at DESC, session_id DESC);

CREATE TABLE agent_runs (
    run_id UUID PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES agent_sessions(session_id),
    remote_run_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (
        status IN (
            'queued', 'running', 'waiting_for_approval', 'stopping',
            'completed', 'failed', 'stopped'
        )
    ),
    source TEXT NOT NULL CHECK (
        source IN ('message', 'regenerate', 'rerun')
    ),
    source_run_id UUID REFERENCES agent_runs(run_id),
    safe_error_code TEXT,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (session_id, remote_run_id)
);

CREATE INDEX agent_runs_session_idx
    ON agent_runs (session_id, created_at DESC, run_id DESC);

CREATE TABLE agent_tool_calls (
    tool_call_id UUID PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES agent_runs(run_id) ON DELETE CASCADE,
    remote_tool_call_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (
        status IN ('queued', 'running', 'completed', 'failed')
    ),
    safe_preview TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (run_id, remote_tool_call_id)
);

CREATE INDEX agent_tool_calls_run_idx
    ON agent_tool_calls (run_id, started_at, tool_call_id);

CREATE TABLE auth_agent_tokens (
    token_id UUID PRIMARY KEY,
    agent_instance_id UUID NOT NULL REFERENCES agent_instances(agent_instance_id),
    grant_id UUID NOT NULL REFERENCES agent_project_grants(grant_id),
    project_id UUID NOT NULL REFERENCES projects(project_id),
    issued_by UUID NOT NULL REFERENCES auth_users(user_id),
    name TEXT NOT NULL,
    token_hash CHAR(64) NOT NULL UNIQUE,
    allowed_tools JSONB NOT NULL DEFAULT '[]'::JSONB,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'revoked')),
    expires_at TIMESTAMPTZ,
    activated_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    verification_evidence_id UUID,
    verification_method TEXT,
    verification_request_id TEXT,
    verification_session_id TEXT,
    verified_by_token_id UUID REFERENCES auth_tokens(token_id),
    verified_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    replaces_token_id UUID REFERENCES auth_agent_tokens(token_id),
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT auth_agent_tokens_grant_binding_fk
    FOREIGN KEY (grant_id, agent_instance_id, project_id)
        REFERENCES agent_project_grants(grant_id, agent_instance_id, project_id),
    CHECK (jsonb_typeof(allowed_tools) = 'array'),
    CHECK (
        (
            verification_evidence_id IS NULL
            AND verification_method IS NULL
            AND verification_request_id IS NULL
            AND verification_session_id IS NULL
            AND verified_by_token_id IS NULL
            AND verified_at IS NULL
        ) OR (
            verification_evidence_id IS NOT NULL
            AND verification_method = 'tools/list'
            AND verification_request_id IS NOT NULL
            AND verification_session_id IS NOT NULL
            AND verified_by_token_id IS NOT NULL
            AND verified_at IS NOT NULL
        )
    )
);

CREATE INDEX auth_agent_tokens_lookup_idx
    ON auth_agent_tokens (token_hash)
    WHERE revoked_at IS NULL;

CREATE UNIQUE INDEX auth_agent_tokens_one_active_grant_idx
    ON auth_agent_tokens (grant_id)
    WHERE status = 'active' AND revoked_at IS NULL;

CREATE UNIQUE INDEX auth_agent_tokens_one_pending_grant_idx
    ON auth_agent_tokens (grant_id)
    WHERE status = 'pending' AND revoked_at IS NULL;

CREATE TABLE agent_token_rotations (
    rotation_id UUID PRIMARY KEY,
    grant_id UUID NOT NULL REFERENCES agent_project_grants(grant_id),
    old_token_id UUID REFERENCES auth_agent_tokens(token_id),
    new_token_id UUID NOT NULL REFERENCES auth_agent_tokens(token_id),
    management_mode TEXT NOT NULL CHECK (management_mode IN ('manual', 'auto')),
    status TEXT NOT NULL CHECK (
        status IN (
            'pending', 'awaiting_user', 'configuring', 'verifying',
            'completed', 'failed', 'cancelled'
        )
    ),
    safe_error_code TEXT,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ
);

CREATE INDEX agent_token_rotations_grant_idx
    ON agent_token_rotations (grant_id, created_at DESC);

ALTER TABLE data_context_proposals
    ADD COLUMN proposed_by_actor_id UUID,
    ADD COLUMN proposed_by_actor_kind TEXT,
    ADD COLUMN agent_session_id UUID REFERENCES agent_sessions(session_id),
    ADD COLUMN agent_run_id UUID REFERENCES agent_runs(run_id);

UPDATE data_context_proposals
SET proposed_by_actor_id = proposed_by,
    proposed_by_actor_kind = 'session'
WHERE proposed_by_actor_id IS NULL;

ALTER TABLE data_context_proposals
    ALTER COLUMN proposed_by DROP NOT NULL,
    ALTER COLUMN proposed_by_actor_id SET NOT NULL,
    ALTER COLUMN proposed_by_actor_kind SET NOT NULL,
    ADD CONSTRAINT data_context_proposals_actor_kind_check
        CHECK (proposed_by_actor_kind IN ('session', 'api', 'agent'));

ALTER TABLE data_context_entries
    ADD COLUMN proposed_by_actor_id UUID,
    ADD COLUMN proposed_by_actor_kind TEXT;

UPDATE data_context_entries
SET proposed_by_actor_id = proposed_by,
    proposed_by_actor_kind = 'session'
WHERE proposed_by_actor_id IS NULL;

ALTER TABLE data_context_entries
    ALTER COLUMN proposed_by DROP NOT NULL,
    ALTER COLUMN proposed_by_actor_id SET NOT NULL,
    ALTER COLUMN proposed_by_actor_kind SET NOT NULL,
    ADD CONSTRAINT data_context_entries_actor_kind_check
        CHECK (proposed_by_actor_kind IN ('session', 'api', 'agent'));
