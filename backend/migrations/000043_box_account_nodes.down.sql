DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM box_project_bindings
        WHERE force_unbound_at IS NULL
        GROUP BY project_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot downgrade: a Project has multiple active Box bindings';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM box_nodes AS node
        WHERE node.legacy_project_id IS NULL
          AND NOT EXISTS (
              SELECT 1
              FROM box_project_bindings AS binding
              WHERE binding.box_id = node.box_id
                AND binding.force_unbound_at IS NULL
          )
    ) THEN
        RAISE EXCEPTION 'cannot downgrade: an account Box has no legacy Project binding';
    END IF;
END
$$;

DROP INDEX box_task_logs_task_sequence_idx;
DROP INDEX box_task_logs_epoch_sequence_idx;
ALTER TABLE box_task_logs
    DROP COLUMN late_after_failure,
    DROP COLUMN received_at,
    DROP COLUMN stream,
    DROP COLUMN sequence,
    DROP COLUMN execution_epoch;

DROP INDEX box_tasks_execution_epoch_idx;
ALTER TABLE box_tasks
    DROP CONSTRAINT box_tasks_log_truncation_check,
    DROP COLUMN resume_state,
    DROP COLUMN logs_truncated_at,
    DROP COLUMN logs_truncated,
    DROP COLUMN last_log_sequence,
    DROP COLUMN last_callback_at,
    DROP COLUMN claimed_at,
    DROP COLUMN runtime_version,
    DROP COLUMN actual_runtime,
    DROP COLUMN execution_epoch;

UPDATE box_nodes AS node
SET legacy_project_id = binding.project_id
FROM box_project_bindings AS binding
WHERE binding.box_id = node.box_id
  AND binding.force_unbound_at IS NULL
  AND node.legacy_project_id IS NULL;

DROP INDEX box_project_bindings_box_active_idx;
DROP INDEX box_project_bindings_project_active_idx;
ALTER TABLE box_project_bindings
    DROP CONSTRAINT box_project_bindings_pkey,
    DROP COLUMN force_unbound_at,
    DROP COLUMN assigned_at,
    DROP COLUMN assigned_by,
    ADD PRIMARY KEY (project_id);

DROP INDEX box_nodes_offline_idx;
DROP INDEX box_nodes_owner_status_idx;
ALTER TABLE box_nodes
    DROP CONSTRAINT box_nodes_revocation_state_check,
    DROP CONSTRAINT box_nodes_owner_installation_key,
    DROP CONSTRAINT box_nodes_status_check;
UPDATE box_nodes SET status = 'revoked' WHERE status = 'draining';
ALTER TABLE box_nodes
    DROP COLUMN legacy_reauthorization_required,
    DROP COLUMN revoked_at,
    DROP COLUMN drain_requested_at,
    DROP COLUMN installation_id,
    DROP COLUMN owner_user_id,
    ALTER COLUMN legacy_project_id SET NOT NULL,
    ADD CONSTRAINT box_nodes_status_check
        CHECK (status IN ('registering', 'online', 'offline', 'revoked')),
    ADD CONSTRAINT box_nodes_project_id_idempotency_key_key
        UNIQUE (legacy_project_id, idempotency_key);
ALTER TABLE box_nodes
    RENAME COLUMN offline_since TO disconnected_at;
ALTER TABLE box_nodes
    RENAME COLUMN legacy_project_id TO project_id;
CREATE INDEX box_nodes_project_status_idx
    ON box_nodes (project_id, status, updated_at DESC);

DROP TABLE auth_box_registration_grants;
ALTER TABLE auth_device_authorizations
    DROP COLUMN client_kind;
