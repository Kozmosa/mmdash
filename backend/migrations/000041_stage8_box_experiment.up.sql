CREATE TABLE box_nodes (
    box_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('registering','online','offline','revoked')),
    version TEXT NOT NULL,
    capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    runtimes JSONB NOT NULL DEFAULT '[]'::jsonb,
    limits JSONB NOT NULL,
    load JSONB NOT NULL DEFAULT '{}'::jsonb,
    token_id UUID NOT NULL UNIQUE REFERENCES auth_tokens(token_id) ON DELETE RESTRICT,
    idempotency_key TEXT NOT NULL,
    last_heartbeat_at TIMESTAMPTZ,
    disconnected_at TIMESTAMPTZ,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, idempotency_key)
);
CREATE INDEX box_nodes_project_status_idx ON box_nodes(project_id, status, updated_at DESC);

CREATE TABLE box_project_bindings (
    project_id UUID PRIMARY KEY REFERENCES projects(project_id) ON DELETE CASCADE,
    box_id UUID NOT NULL REFERENCES box_nodes(box_id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX box_project_bindings_box_idx ON box_project_bindings(box_id);

CREATE TABLE experiments (
    experiment_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('created','queued','preparing','running','succeeded','failed','canceled','archived')),
    source_commit TEXT NOT NULL,
    entrypoint TEXT NOT NULL,
    parameters JSONB NOT NULL DEFAULT '{}'::jsonb,
    environment JSONB NOT NULL DEFAULT '{}'::jsonb,
    inputs JSONB NOT NULL DEFAULT '{}'::jsonb,
    runtime TEXT NOT NULL CHECK (runtime IN ('local-docker','e2b')),
    limits JSONB NOT NULL,
    idempotency_key TEXT NOT NULL,
    max_attempts INTEGER NOT NULL DEFAULT 1 CHECK (max_attempts BETWEEN 1 AND 5),
    box_id UUID REFERENCES box_nodes(box_id) ON DELETE SET NULL,
    task_id UUID UNIQUE,
    exit_code INTEGER,
    failure_code TEXT,
    failure_message TEXT,
    resource_usage JSONB NOT NULL DEFAULT '{}'::jsonb,
    summary TEXT,
    result_manifest JSONB,
    result_artifact JSONB,
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, idempotency_key)
);
CREATE INDEX experiments_project_updated_idx ON experiments(project_id, updated_at DESC, experiment_id DESC);
CREATE INDEX experiments_project_status_idx ON experiments(project_id, status, updated_at DESC);

CREATE TABLE box_tasks (
    task_id UUID PRIMARY KEY,
    experiment_id UUID NOT NULL UNIQUE REFERENCES experiments(experiment_id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    box_id UUID REFERENCES box_nodes(box_id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK (status IN ('queued','preparing','running','succeeded','failed','canceled','timed_out')),
    attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 1 CHECK (max_attempts BETWEEN 1 AND 5),
    run_spec JSONB NOT NULL,
    lease_expires_at TIMESTAMPTZ,
    cancel_requested_at TIMESTAMPTZ,
    exit_code INTEGER,
    error_code TEXT,
    error_message TEXT,
    resource_usage JSONB NOT NULL DEFAULT '{}'::jsonb,
    summary TEXT,
    result_manifest JSONB,
    result_artifact JSONB,
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX box_tasks_claim_idx ON box_tasks(status, created_at, task_id);
CREATE INDEX box_tasks_box_status_idx ON box_tasks(box_id, status, lease_expires_at);

CREATE TABLE box_task_logs (
    log_id UUID PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES box_tasks(task_id) ON DELETE CASCADE,
    experiment_id UUID NOT NULL REFERENCES experiments(experiment_id) ON DELETE CASCADE,
    level TEXT NOT NULL CHECK (level IN ('debug','info','warning','error')),
    message TEXT NOT NULL,
    fields JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX box_task_logs_task_idx ON box_task_logs(task_id, occurred_at, log_id);
