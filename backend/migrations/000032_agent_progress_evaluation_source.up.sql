ALTER TABLE agent_runs
    ADD COLUMN source_evaluation_id UUID
        REFERENCES progress_evaluations(evaluation_id) ON DELETE SET NULL;

CREATE INDEX agent_runs_source_evaluation_idx
    ON agent_runs (source_evaluation_id)
    WHERE source_evaluation_id IS NOT NULL;
