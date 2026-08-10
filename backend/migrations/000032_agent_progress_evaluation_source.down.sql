DROP INDEX IF EXISTS agent_runs_source_evaluation_idx;

ALTER TABLE agent_runs
    DROP COLUMN IF EXISTS source_evaluation_id;
