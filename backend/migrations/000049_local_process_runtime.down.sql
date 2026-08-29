-- Restore the pre-local-process Runtime value sets. The migration only ever
-- widens CHECK constraints, so this reversal is safe whenever no row stores
-- the new value.

ALTER TABLE box_tasks
    DROP CONSTRAINT box_tasks_actual_runtime_check;
ALTER TABLE box_tasks
    ADD CONSTRAINT box_tasks_actual_runtime_check
        CHECK (actual_runtime IS NULL OR actual_runtime IN ('local-docker', 'e2b'));

ALTER TABLE experiments
    DROP CONSTRAINT experiments_actual_runtime_check;
ALTER TABLE experiments
    ADD CONSTRAINT experiments_actual_runtime_check
        CHECK (actual_runtime IS NULL OR actual_runtime IN ('local-docker', 'e2b'));

ALTER TABLE experiments
    DROP CONSTRAINT experiments_requested_runtime_policy_check;
ALTER TABLE experiments
    ADD CONSTRAINT experiments_requested_runtime_policy_check
        CHECK (requested_runtime_policy IN ('auto', 'local-docker', 'e2b'));

ALTER TABLE experiment_project_settings
    DROP CONSTRAINT experiment_project_settings_default_runtime_policy_check;
ALTER TABLE experiment_project_settings
    ADD CONSTRAINT experiment_project_settings_default_runtime_policy_check
        CHECK (default_runtime_policy IN ('auto', 'local-docker', 'e2b'));
