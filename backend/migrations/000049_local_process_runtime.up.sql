-- Add the trusted-host `local-process` Runtime to the append-only Runtime
-- value sets. Existing rows never contain the new value, so widening the CHECK
-- constraints is safe for both fresh and existing databases. `auto` remains a
-- scheduling policy, not a Runtime: only E2B and Local Docker satisfy it.

ALTER TABLE box_tasks
    DROP CONSTRAINT box_tasks_actual_runtime_check;
ALTER TABLE box_tasks
    ADD CONSTRAINT box_tasks_actual_runtime_check
        CHECK (actual_runtime IS NULL OR actual_runtime IN ('local-docker', 'e2b', 'local-process'));

ALTER TABLE experiments
    DROP CONSTRAINT experiments_actual_runtime_check;
ALTER TABLE experiments
    ADD CONSTRAINT experiments_actual_runtime_check
        CHECK (actual_runtime IS NULL OR actual_runtime IN ('local-docker', 'e2b', 'local-process'));

ALTER TABLE experiments
    DROP CONSTRAINT experiments_requested_runtime_policy_check;
ALTER TABLE experiments
    ADD CONSTRAINT experiments_requested_runtime_policy_check
        CHECK (requested_runtime_policy IN ('auto', 'local-docker', 'e2b', 'local-process'));

ALTER TABLE experiment_project_settings
    DROP CONSTRAINT experiment_project_settings_default_runtime_policy_check;
ALTER TABLE experiment_project_settings
    ADD CONSTRAINT experiment_project_settings_default_runtime_policy_check
        CHECK (default_runtime_policy IN ('auto', 'local-docker', 'e2b', 'local-process'));
