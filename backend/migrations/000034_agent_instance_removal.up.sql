ALTER TABLE agent_instances
    ADD COLUMN removed_at TIMESTAMPTZ;

CREATE INDEX agent_instances_visible_idx
    ON agent_instances (created_at, agent_instance_id)
    WHERE removed_at IS NULL;
