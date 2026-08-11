DROP INDEX IF EXISTS agent_instances_visible_idx;

ALTER TABLE agent_instances
    DROP COLUMN IF EXISTS removed_at;
