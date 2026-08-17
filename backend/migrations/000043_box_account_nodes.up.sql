ALTER TABLE auth_device_authorizations
    ADD COLUMN client_kind TEXT NOT NULL DEFAULT 'cli'
        CHECK (client_kind IN ('cli', 'box'));

CREATE TABLE auth_box_registration_grants (
    grant_id UUID PRIMARY KEY,
    grant_hash TEXT NOT NULL UNIQUE,
    user_id UUID NOT NULL REFERENCES auth_users(user_id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (expires_at > created_at),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);
CREATE INDEX auth_box_registration_grants_active_idx
    ON auth_box_registration_grants (expires_at, grant_id)
    WHERE consumed_at IS NULL;

ALTER TABLE box_nodes
    RENAME COLUMN project_id TO legacy_project_id;
ALTER TABLE box_nodes
    RENAME COLUMN disconnected_at TO offline_since;
ALTER TABLE box_nodes
    DROP CONSTRAINT box_nodes_project_id_idempotency_key_key;
ALTER TABLE box_nodes
    DROP CONSTRAINT box_nodes_status_check;
ALTER TABLE box_nodes
    ALTER COLUMN legacy_project_id DROP NOT NULL,
    ADD COLUMN owner_user_id UUID REFERENCES auth_users(user_id) ON DELETE CASCADE,
    ADD COLUMN installation_id TEXT,
    ADD COLUMN drain_requested_at TIMESTAMPTZ,
    ADD COLUMN revoked_at TIMESTAMPTZ,
    ADD COLUMN legacy_reauthorization_required BOOLEAN NOT NULL DEFAULT false;

UPDATE box_nodes
SET owner_user_id = created_by,
    installation_id = idempotency_key,
    legacy_reauthorization_required = status <> 'revoked';

ALTER TABLE box_nodes
    ALTER COLUMN owner_user_id SET NOT NULL,
    ALTER COLUMN installation_id SET NOT NULL,
    ADD CONSTRAINT box_nodes_status_check
        CHECK (status IN ('registering', 'online', 'offline', 'draining', 'revoked')),
    ADD CONSTRAINT box_nodes_owner_installation_key
        UNIQUE (owner_user_id, installation_id),
    ADD CONSTRAINT box_nodes_revocation_state_check
        CHECK (
            (status = 'draining' AND drain_requested_at IS NOT NULL)
            OR status <> 'draining'
        );

DROP INDEX box_nodes_project_status_idx;
CREATE INDEX box_nodes_owner_status_idx
    ON box_nodes (owner_user_id, status, updated_at DESC);
CREATE INDEX box_nodes_offline_idx
    ON box_nodes (offline_since, box_id)
    WHERE status = 'offline';

ALTER TABLE box_project_bindings
    DROP CONSTRAINT box_project_bindings_pkey;
ALTER TABLE box_project_bindings
    ADD COLUMN assigned_by UUID REFERENCES auth_users(user_id),
    ADD COLUMN assigned_at TIMESTAMPTZ,
    ADD COLUMN force_unbound_at TIMESTAMPTZ;

UPDATE box_project_bindings AS binding
SET assigned_by = node.created_by,
    assigned_at = binding.created_at
FROM box_nodes AS node
WHERE node.box_id = binding.box_id;

ALTER TABLE box_project_bindings
    ALTER COLUMN assigned_by SET NOT NULL,
    ALTER COLUMN assigned_at SET NOT NULL,
    ADD PRIMARY KEY (project_id, box_id);
CREATE INDEX box_project_bindings_project_active_idx
    ON box_project_bindings (project_id, assigned_at, box_id)
    WHERE force_unbound_at IS NULL;
CREATE INDEX box_project_bindings_box_active_idx
    ON box_project_bindings (box_id, assigned_at, project_id)
    WHERE force_unbound_at IS NULL;

ALTER TABLE box_tasks
    ADD COLUMN execution_epoch UUID,
    ADD COLUMN actual_runtime TEXT
        CHECK (actual_runtime IS NULL OR actual_runtime IN ('local-docker', 'e2b')),
    ADD COLUMN runtime_version TEXT,
    ADD COLUMN claimed_at TIMESTAMPTZ,
    ADD COLUMN last_callback_at TIMESTAMPTZ,
    ADD COLUMN last_log_sequence BIGINT NOT NULL DEFAULT 0 CHECK (last_log_sequence >= 0),
    ADD COLUMN logs_truncated BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN logs_truncated_at TIMESTAMPTZ,
    ADD COLUMN resume_state JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT box_tasks_log_truncation_check
        CHECK (NOT logs_truncated OR logs_truncated_at IS NOT NULL);
CREATE UNIQUE INDEX box_tasks_execution_epoch_idx
    ON box_tasks (task_id, execution_epoch)
    WHERE execution_epoch IS NOT NULL;

ALTER TABLE box_task_logs
    ADD COLUMN execution_epoch UUID,
    ADD COLUMN sequence BIGINT CHECK (sequence IS NULL OR sequence > 0),
    ADD COLUMN stream TEXT CHECK (stream IS NULL OR stream IN ('stdout', 'stderr', 'system')),
    ADD COLUMN received_at TIMESTAMPTZ,
    ADD COLUMN late_after_failure BOOLEAN NOT NULL DEFAULT false;

WITH ordered AS (
    SELECT log_id,
           ROW_NUMBER() OVER (PARTITION BY task_id ORDER BY occurred_at, log_id) AS sequence
    FROM box_task_logs
)
UPDATE box_task_logs AS log
SET sequence = ordered.sequence,
    stream = 'system',
    received_at = log.occurred_at
FROM ordered
WHERE ordered.log_id = log.log_id;

CREATE UNIQUE INDEX box_task_logs_epoch_sequence_idx
    ON box_task_logs (task_id, execution_epoch, sequence)
    WHERE execution_epoch IS NOT NULL;
CREATE INDEX box_task_logs_task_sequence_idx
    ON box_task_logs (task_id, sequence, log_id);
