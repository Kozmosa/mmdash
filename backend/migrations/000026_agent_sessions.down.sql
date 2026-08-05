-- Revert Stage 5 Agent sessions and resource-scoped Agent settings.
DELETE FROM data_context_entries
WHERE proposed_by IS NULL;

ALTER TABLE data_context_entries
    DROP CONSTRAINT IF EXISTS data_context_entries_actor_kind_check,
    DROP COLUMN IF EXISTS proposed_by_actor_kind,
    DROP COLUMN IF EXISTS proposed_by_actor_id;

ALTER TABLE data_context_entries
    ALTER COLUMN proposed_by SET NOT NULL;

DELETE FROM data_context_proposals
WHERE proposed_by IS NULL;

ALTER TABLE data_context_proposals
    DROP CONSTRAINT IF EXISTS data_context_proposals_actor_kind_check,
    DROP COLUMN IF EXISTS agent_run_id,
    DROP COLUMN IF EXISTS agent_session_id,
    DROP COLUMN IF EXISTS proposed_by_actor_kind,
    DROP COLUMN IF EXISTS proposed_by_actor_id;

ALTER TABLE data_context_proposals
    ALTER COLUMN proposed_by SET NOT NULL;

DROP TABLE IF EXISTS agent_token_rotations;
DROP TABLE IF EXISTS auth_agent_tokens;
DROP TABLE IF EXISTS agent_tool_calls;
DROP TABLE IF EXISTS agent_runs;

ALTER TABLE agent_project_grants
    DROP CONSTRAINT IF EXISTS agent_project_grants_default_session_fk;

DROP TABLE IF EXISTS agent_sessions;
DROP TABLE IF EXISTS agent_project_grants;
DROP TABLE IF EXISTS agent_instances;

DROP INDEX IF EXISTS settings_values_scope_idx;

ALTER TABLE settings_values
    DROP CONSTRAINT IF EXISTS settings_values_scope_resource_key,
    DROP COLUMN IF EXISTS resource_id;

ALTER TABLE settings_values
    ADD CONSTRAINT settings_values_scope_type_scope_id_type_key_key
    UNIQUE (scope_type, scope_id, type_key);

CREATE INDEX settings_values_scope_idx
    ON settings_values (scope_type, scope_id, type_key);
