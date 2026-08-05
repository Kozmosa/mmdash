ALTER TABLE progress_proposals
    DROP CONSTRAINT IF EXISTS progress_proposals_task_reference_shapes_check,
    DROP CONSTRAINT IF EXISTS progress_proposals_target_shape_check;

ALTER TABLE progress_reminders
    DROP CONSTRAINT IF EXISTS progress_reminders_project_milestone_fk,
    DROP CONSTRAINT IF EXISTS progress_reminders_project_task_fk,
    ADD CONSTRAINT progress_reminders_task_id_fkey
        FOREIGN KEY (task_id)
        REFERENCES progress_tasks(task_id)
        ON DELETE CASCADE,
    ADD CONSTRAINT progress_reminders_milestone_id_fkey
        FOREIGN KEY (milestone_id)
        REFERENCES progress_milestones(milestone_id)
        ON DELETE CASCADE;

ALTER TABLE progress_dependencies
    DROP CONSTRAINT IF EXISTS progress_dependencies_project_prerequisite_fk,
    DROP CONSTRAINT IF EXISTS progress_dependencies_project_task_fk,
    ADD CONSTRAINT progress_dependencies_task_id_fkey
        FOREIGN KEY (task_id)
        REFERENCES progress_tasks(task_id)
        ON DELETE CASCADE,
    ADD CONSTRAINT progress_dependencies_depends_on_task_id_fkey
        FOREIGN KEY (depends_on_task_id)
        REFERENCES progress_tasks(task_id)
        ON DELETE CASCADE;

ALTER TABLE progress_tasks
    DROP CONSTRAINT IF EXISTS progress_tasks_project_assignee_fk,
    DROP CONSTRAINT IF EXISTS progress_tasks_project_milestone_fk,
    DROP CONSTRAINT IF EXISTS progress_tasks_related_object_ids_uuid_array_check,
    ADD CONSTRAINT progress_tasks_milestone_id_fkey
        FOREIGN KEY (milestone_id)
        REFERENCES progress_milestones(milestone_id)
        ON DELETE SET NULL,
    ADD CONSTRAINT progress_tasks_assignee_id_fkey
        FOREIGN KEY (assignee_id)
        REFERENCES auth_users(user_id)
        ON DELETE SET NULL,
    DROP CONSTRAINT IF EXISTS progress_tasks_project_task_unique;

ALTER TABLE progress_milestones
    DROP CONSTRAINT IF EXISTS progress_milestones_project_milestone_unique;

DROP FUNCTION IF EXISTS progress_jsonb_uuid_array_is_valid(JSONB);
DROP FUNCTION IF EXISTS progress_uuid_text_is_valid(TEXT);
