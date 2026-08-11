DROP INDEX IF EXISTS progress_evaluations_active_input_unique_idx;

CREATE UNIQUE INDEX progress_evaluations_active_input_unique_idx
    ON progress_evaluations (project_id, input_version)
    WHERE status IN ('queued', 'running', 'succeeded');
