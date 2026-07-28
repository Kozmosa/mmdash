CREATE TABLE jobs (
    job_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    job_type TEXT NOT NULL
        CHECK (job_type ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$'),
    payload JSONB NOT NULL DEFAULT '{}'::JSONB,
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN (
            'queued', 'running', 'succeeded', 'failed', 'cancelled', 'timed_out'
        )),
    priority INTEGER NOT NULL DEFAULT 0,
    idempotency_key TEXT,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts BETWEEN 1 AND 100),
    available_at TIMESTAMPTZ NOT NULL,
    timeout_seconds INTEGER NOT NULL DEFAULT 900
        CHECK (timeout_seconds BETWEEN 1 AND 86400),
    timeout_at TIMESTAMPTZ,
    locked_by TEXT,
    lease_expires_at TIMESTAMPTZ,
    cancel_requested_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    result JSONB,
    error_code TEXT,
    error_message TEXT,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX jobs_idempotency_unique_idx
    ON jobs (project_id, job_type, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX jobs_claim_idx
    ON jobs (priority DESC, available_at, created_at, job_id)
    WHERE status = 'queued';

CREATE INDEX jobs_project_idx
    ON jobs (project_id, created_at DESC, job_id);

CREATE INDEX jobs_running_lease_idx
    ON jobs (lease_expires_at)
    WHERE status = 'running';

CREATE TABLE job_logs (
    job_log_id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(job_id) ON DELETE CASCADE,
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    level TEXT NOT NULL CHECK (level IN ('debug', 'info', 'warning', 'error')),
    message TEXT NOT NULL,
    fields JSONB NOT NULL DEFAULT '{}'::JSONB,
    worker_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX job_logs_job_idx
    ON job_logs (job_id, created_at, job_log_id);

CREATE TABLE worker_heartbeats (
    worker_id TEXT PRIMARY KEY,
    version TEXT NOT NULL,
    capabilities JSONB NOT NULL DEFAULT '[]'::JSONB,
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    last_seen_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
