ALTER TABLE progress_settings
    ADD COLUMN auto_tracking_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN event_triggers_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN cron_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN cron_schedule TEXT NOT NULL DEFAULT '0 */6 * * *',
    ADD COLUMN debounce_seconds INTEGER NOT NULL DEFAULT 60
        CHECK (debounce_seconds BETWEEN 0 AND 3600),
    ADD COLUMN min_interval_seconds INTEGER NOT NULL DEFAULT 300
        CHECK (min_interval_seconds BETWEEN 0 AND 86400),
    ADD COLUMN agent_instance_id UUID REFERENCES agent_instances(agent_instance_id),
    ADD COLUMN cron_remote_job_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN cron_sync_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (cron_sync_status IN ('pending', 'syncing', 'ready', 'failed', 'disabled')),
    ADD COLUMN cron_error_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN cron_synced_at TIMESTAMPTZ,
    ADD COLUMN cron_retry_at TIMESTAMPTZ,
    ADD COLUMN cron_lease_owner TEXT NOT NULL DEFAULT '',
    ADD COLUMN cron_lease_expires_at TIMESTAMPTZ;

ALTER TABLE progress_tasks
    ADD COLUMN manual_override_fields JSONB NOT NULL DEFAULT '[]'::JSONB
        CHECK (jsonb_typeof(manual_override_fields) = 'array'),
    ADD COLUMN source_evaluation_id UUID,
    ADD COLUMN source_key TEXT NOT NULL DEFAULT '';

ALTER TABLE progress_proposals
    ADD COLUMN source_evaluation_id UUID,
    ADD COLUMN source_key TEXT NOT NULL DEFAULT '';

CREATE TABLE progress_evaluation_requests (
    request_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    trigger_kind TEXT NOT NULL
        CHECK (trigger_kind IN ('event', 'manual', 'cron', 'retry')),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'assembling', 'queued', 'merged', 'failed')),
    scheduled_for TIMESTAMPTZ NOT NULL,
    actor_id UUID NOT NULL REFERENCES auth_users(user_id),
    requested_by_kind TEXT NOT NULL
        CHECK (requested_by_kind IN ('session', 'agent', 'system')),
    force BOOLEAN NOT NULL DEFAULT FALSE,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_expires_at TIMESTAMPTZ,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    merged_into_evaluation_id UUID,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX progress_evaluation_requests_one_active_project_idx
    ON progress_evaluation_requests (project_id)
    WHERE status IN ('pending', 'assembling');
CREATE INDEX progress_evaluation_requests_due_idx
    ON progress_evaluation_requests (scheduled_for, created_at, request_id)
    WHERE status = 'pending';
CREATE INDEX progress_evaluation_requests_lease_idx
    ON progress_evaluation_requests (lease_expires_at)
    WHERE status = 'assembling';

CREATE TABLE progress_evaluation_triggers (
    trigger_id UUID PRIMARY KEY,
    request_id UUID NOT NULL REFERENCES progress_evaluation_requests(request_id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    trigger_type TEXT NOT NULL,
    source_event_id UUID,
    source_event_type TEXT NOT NULL DEFAULT '',
    source_resource_id TEXT NOT NULL DEFAULT '',
    source_version TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::JSONB
        CHECK (jsonb_typeof(payload) = 'object'),
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX progress_evaluation_triggers_event_unique_idx
    ON progress_evaluation_triggers (source_event_id)
    WHERE source_event_id IS NOT NULL;
CREATE INDEX progress_evaluation_triggers_request_idx
    ON progress_evaluation_triggers (request_id, occurred_at, trigger_id);

CREATE TABLE progress_evaluations (
    evaluation_id UUID PRIMARY KEY,
    request_id UUID NOT NULL UNIQUE
        REFERENCES progress_evaluation_requests(request_id) ON DELETE RESTRICT,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    job_id UUID UNIQUE REFERENCES jobs(job_id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    input_version CHAR(64) NOT NULL,
    input_snapshot JSONB NOT NULL CHECK (jsonb_typeof(input_snapshot) = 'object'),
    output_snapshot JSONB CHECK (output_snapshot IS NULL OR jsonb_typeof(output_snapshot) = 'object'),
    detected_stage TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    changes_since_last JSONB NOT NULL DEFAULT '[]'::JSONB
        CHECK (jsonb_typeof(changes_since_last) = 'array'),
    completed_items JSONB NOT NULL DEFAULT '[]'::JSONB
        CHECK (jsonb_typeof(completed_items) = 'array'),
    in_progress_items JSONB NOT NULL DEFAULT '[]'::JSONB
        CHECK (jsonb_typeof(in_progress_items) = 'array'),
    blockers JSONB NOT NULL DEFAULT '[]'::JSONB
        CHECK (jsonb_typeof(blockers) = 'array'),
    pending_questions JSONB NOT NULL DEFAULT '[]'::JSONB
        CHECK (jsonb_typeof(pending_questions) = 'array'),
    source_event_ids JSONB NOT NULL DEFAULT '[]'::JSONB
        CHECK (jsonb_typeof(source_event_ids) = 'array'),
    trigger_kind TEXT NOT NULL,
    agent_instance_id UUID REFERENCES agent_instances(agent_instance_id),
    agent_session_id UUID REFERENCES agent_sessions(session_id),
    agent_run_id UUID REFERENCES agent_runs(run_id),
    evaluator_mode TEXT NOT NULL DEFAULT 'core_agent'
        CHECK (evaluator_mode IN ('core_agent', 'mock')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    requested_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE progress_evaluation_requests
    ADD CONSTRAINT progress_evaluation_requests_merged_evaluation_fk
    FOREIGN KEY (merged_into_evaluation_id)
    REFERENCES progress_evaluations(evaluation_id) ON DELETE SET NULL;

CREATE UNIQUE INDEX progress_evaluations_active_input_unique_idx
    ON progress_evaluations (project_id, input_version)
    WHERE status IN ('queued', 'running', 'succeeded');
CREATE INDEX progress_evaluations_project_history_idx
    ON progress_evaluations (project_id, created_at DESC, evaluation_id DESC);
CREATE INDEX progress_evaluations_status_idx
    ON progress_evaluations (status, updated_at, evaluation_id);

ALTER TABLE progress_tasks
    ADD CONSTRAINT progress_tasks_source_evaluation_fk
    FOREIGN KEY (source_evaluation_id) REFERENCES progress_evaluations(evaluation_id)
    ON DELETE SET NULL;

CREATE UNIQUE INDEX progress_tasks_automatic_source_unique_idx
    ON progress_tasks (project_id, source_key)
    WHERE source_key <> '';

ALTER TABLE progress_proposals
    ADD CONSTRAINT progress_proposals_source_evaluation_fk
    FOREIGN KEY (source_evaluation_id) REFERENCES progress_evaluations(evaluation_id)
    ON DELETE SET NULL;

CREATE UNIQUE INDEX progress_proposals_evaluation_source_unique_idx
    ON progress_proposals (source_evaluation_id, source_key)
    WHERE source_evaluation_id IS NOT NULL AND source_key <> '';

CREATE TABLE progress_stage_overrides (
    override_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    stage TEXT NOT NULL CHECK (length(stage) BETWEEN 1 AND 100),
    summary TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    cleared_by UUID REFERENCES auth_users(user_id),
    cleared_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX progress_stage_overrides_one_active_project_idx
    ON progress_stage_overrides (project_id)
    WHERE active;
CREATE INDEX progress_stage_overrides_history_idx
    ON progress_stage_overrides (project_id, created_at DESC, override_id DESC);

CREATE TABLE progress_tracker_state (
    project_id UUID PRIMARY KEY REFERENCES projects(project_id) ON DELETE CASCADE,
    last_evaluation_id UUID REFERENCES progress_evaluations(evaluation_id) ON DELETE SET NULL,
    detected_stage TEXT NOT NULL DEFAULT '',
    effective_stage TEXT NOT NULL DEFAULT '',
    stage_overridden BOOLEAN NOT NULL DEFAULT FALSE,
    summary TEXT NOT NULL DEFAULT '',
    changes_since_last JSONB NOT NULL DEFAULT '[]'::JSONB,
    completed_items JSONB NOT NULL DEFAULT '[]'::JSONB,
    in_progress_items JSONB NOT NULL DEFAULT '[]'::JSONB,
    blockers JSONB NOT NULL DEFAULT '[]'::JSONB,
    pending_questions JSONB NOT NULL DEFAULT '[]'::JSONB,
    last_evaluated_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (jsonb_typeof(changes_since_last) = 'array'),
    CHECK (jsonb_typeof(completed_items) = 'array'),
    CHECK (jsonb_typeof(in_progress_items) = 'array'),
    CHECK (jsonb_typeof(blockers) = 'array'),
    CHECK (jsonb_typeof(pending_questions) = 'array')
);

CREATE TABLE progress_evaluation_risks (
    risk_id UUID PRIMARY KEY,
    evaluation_id UUID NOT NULL REFERENCES progress_evaluations(evaluation_id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    risk_key TEXT NOT NULL,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 255),
    severity TEXT NOT NULL CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'mitigated', 'accepted')),
    detail TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (evaluation_id, risk_key)
);

CREATE INDEX progress_evaluation_risks_project_idx
    ON progress_evaluation_risks (project_id, created_at DESC, risk_id DESC);

ALTER TABLE agent_runs DROP CONSTRAINT IF EXISTS agent_runs_source_check;
ALTER TABLE agent_runs
    ADD CONSTRAINT agent_runs_source_check
    CHECK (source IN ('message', 'regenerate', 'rerun', 'progress_evaluation'));
