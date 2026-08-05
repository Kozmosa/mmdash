LOCK TABLE progress_milestones,
           progress_tasks,
           progress_dependencies,
           progress_reminders,
           progress_proposals,
           project_members,
           data_objects
IN SHARE ROW EXCLUSIVE MODE;

CREATE OR REPLACE FUNCTION progress_uuid_text_is_valid(value TEXT)
RETURNS BOOLEAN
LANGUAGE SQL
IMMUTABLE
STRICT
AS $$
    SELECT value ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
$$;

CREATE OR REPLACE FUNCTION progress_jsonb_uuid_array_is_valid(value JSONB)
RETURNS BOOLEAN
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    element JSONB;
    identifier TEXT;
BEGIN
    IF jsonb_typeof(value) <> 'array' THEN
        RETURN FALSE;
    END IF;

    FOR element IN SELECT item FROM jsonb_array_elements(value) AS items(item)
    LOOP
        IF jsonb_typeof(element) <> 'string' THEN
            RETURN FALSE;
        END IF;
        identifier := element #>> '{}';
        IF NOT progress_uuid_text_is_valid(identifier) THEN
            RETURN FALSE;
        END IF;
    END LOOP;

    RETURN TRUE;
END
$$;

DO $$
DECLARE
    dirty_count BIGINT;
BEGIN
    SELECT COUNT(*) INTO dirty_count
    FROM progress_tasks AS task
    LEFT JOIN progress_milestones AS milestone
      ON milestone.project_id = task.project_id
     AND milestone.milestone_id = task.milestone_id
    WHERE task.milestone_id IS NOT NULL
      AND milestone.milestone_id IS NULL;
    IF dirty_count > 0 THEN
        RAISE EXCEPTION 'progress migration blocked: task milestone references (% rows)', dirty_count;
    END IF;

    SELECT COUNT(*) INTO dirty_count
    FROM progress_tasks AS task
    LEFT JOIN project_members AS member
      ON member.project_id = task.project_id
     AND member.user_id = task.assignee_id
    WHERE task.assignee_id IS NOT NULL
      AND member.user_id IS NULL;
    IF dirty_count > 0 THEN
        RAISE EXCEPTION 'progress migration blocked: task assignee references (% rows)', dirty_count;
    END IF;

    SELECT COUNT(*) INTO dirty_count
    FROM progress_dependencies AS dependency
    LEFT JOIN progress_tasks AS task
      ON task.project_id = dependency.project_id
     AND task.task_id = dependency.task_id
    LEFT JOIN progress_tasks AS prerequisite
      ON prerequisite.project_id = dependency.project_id
     AND prerequisite.task_id = dependency.depends_on_task_id
    WHERE task.task_id IS NULL OR prerequisite.task_id IS NULL;
    IF dirty_count > 0 THEN
        RAISE EXCEPTION 'progress migration blocked: dependency task references (% rows)', dirty_count;
    END IF;

    SELECT COUNT(*) INTO dirty_count
    FROM progress_reminders AS reminder
    LEFT JOIN progress_tasks AS task
      ON task.project_id = reminder.project_id
     AND task.task_id = reminder.task_id
    LEFT JOIN progress_milestones AS milestone
      ON milestone.project_id = reminder.project_id
     AND milestone.milestone_id = reminder.milestone_id
    WHERE (reminder.task_id IS NOT NULL AND task.task_id IS NULL)
       OR (reminder.milestone_id IS NOT NULL AND milestone.milestone_id IS NULL);
    IF dirty_count > 0 THEN
        RAISE EXCEPTION 'progress migration blocked: reminder target references (% rows)', dirty_count;
    END IF;

    SELECT COUNT(*) INTO dirty_count
    FROM progress_tasks
    WHERE NOT progress_jsonb_uuid_array_is_valid(related_object_ids);
    IF dirty_count > 0 THEN
        RAISE EXCEPTION 'progress migration blocked: task related object shapes (% rows)', dirty_count;
    END IF;

    SELECT COUNT(*) INTO dirty_count
    FROM progress_tasks AS task
    WHERE EXISTS (
        SELECT 1
        FROM jsonb_array_elements_text(task.related_object_ids) AS related(object_id)
        LEFT JOIN data_objects AS object
          ON object.project_id = task.project_id
         AND object.object_id = related.object_id::UUID
        WHERE object.object_id IS NULL
    );
    IF dirty_count > 0 THEN
        RAISE EXCEPTION 'progress migration blocked: task related object references (% rows)', dirty_count;
    END IF;

    SELECT COUNT(*) INTO dirty_count
    FROM progress_proposals AS proposal
    WHERE proposal.status = 'pending'
      AND (
        (proposal.proposal_type IN ('milestone.create', 'task.create') AND proposal.target_id IS NOT NULL)
        OR
        (proposal.proposal_type IN ('milestone.update', 'task.update') AND proposal.target_id IS NULL)
        OR
        (proposal.proposal_type = 'milestone.update' AND NOT EXISTS (
            SELECT 1
            FROM progress_milestones AS milestone
            WHERE milestone.project_id = proposal.project_id
              AND milestone.milestone_id = proposal.target_id
        ))
        OR
        (proposal.proposal_type = 'task.update' AND NOT EXISTS (
            SELECT 1
            FROM progress_tasks AS task
            WHERE task.project_id = proposal.project_id
              AND task.task_id = proposal.target_id
        ))
      );
    IF dirty_count > 0 THEN
        RAISE EXCEPTION 'progress migration blocked: pending proposal targets (% rows)', dirty_count;
    END IF;

    SELECT COUNT(*) INTO dirty_count
    FROM progress_proposals AS proposal
    WHERE proposal.status = 'pending'
      AND proposal.proposal_type IN ('task.create', 'task.update')
      AND (
        (proposal.changes ? 'milestone_id' AND (
            jsonb_typeof(proposal.changes -> 'milestone_id') <> 'string'
            OR (
                proposal.changes ->> 'milestone_id' <> ''
                AND NOT progress_uuid_text_is_valid(proposal.changes ->> 'milestone_id')
            )
        ))
        OR
        (proposal.changes ? 'assignee_id' AND (
            jsonb_typeof(proposal.changes -> 'assignee_id') <> 'string'
            OR (
                proposal.changes ->> 'assignee_id' <> ''
                AND NOT progress_uuid_text_is_valid(proposal.changes ->> 'assignee_id')
            )
        ))
        OR
        (proposal.changes ? 'related_object_ids'
         AND NOT progress_jsonb_uuid_array_is_valid(proposal.changes -> 'related_object_ids'))
      );
    IF dirty_count > 0 THEN
        RAISE EXCEPTION 'progress migration blocked: pending proposal reference shapes (% rows)', dirty_count;
    END IF;

    SELECT COUNT(*) INTO dirty_count
    FROM progress_proposals AS proposal
    WHERE proposal.status = 'pending'
      AND proposal.proposal_type IN ('task.create', 'task.update')
      AND (
        (proposal.changes ? 'milestone_id'
         AND proposal.changes ->> 'milestone_id' <> ''
         AND NOT EXISTS (
             SELECT 1
             FROM progress_milestones AS milestone
             WHERE milestone.project_id = proposal.project_id
               AND milestone.milestone_id = (proposal.changes ->> 'milestone_id')::UUID
         ))
        OR
        (proposal.changes ? 'assignee_id'
         AND proposal.changes ->> 'assignee_id' <> ''
         AND NOT EXISTS (
             SELECT 1
             FROM project_members AS member
             WHERE member.project_id = proposal.project_id
               AND member.user_id = (proposal.changes ->> 'assignee_id')::UUID
         ))
        OR
        (proposal.changes ? 'related_object_ids' AND EXISTS (
            SELECT 1
            FROM jsonb_array_elements_text(proposal.changes -> 'related_object_ids') AS related(object_id)
            LEFT JOIN data_objects AS object
              ON object.project_id = proposal.project_id
             AND object.object_id = related.object_id::UUID
            WHERE object.object_id IS NULL
        ))
      );
    IF dirty_count > 0 THEN
        RAISE EXCEPTION 'progress migration blocked: pending proposal references (% rows)', dirty_count;
    END IF;
END
$$;

ALTER TABLE progress_milestones
    ADD CONSTRAINT progress_milestones_project_milestone_unique
        UNIQUE (project_id, milestone_id);

ALTER TABLE progress_tasks
    ADD CONSTRAINT progress_tasks_project_task_unique
        UNIQUE (project_id, task_id),
    ADD CONSTRAINT progress_tasks_related_object_ids_uuid_array_check
        CHECK (progress_jsonb_uuid_array_is_valid(related_object_ids)),
    DROP CONSTRAINT progress_tasks_milestone_id_fkey,
    DROP CONSTRAINT progress_tasks_assignee_id_fkey,
    ADD CONSTRAINT progress_tasks_project_milestone_fk
        FOREIGN KEY (project_id, milestone_id)
        REFERENCES progress_milestones(project_id, milestone_id)
        ON DELETE SET NULL (milestone_id),
    ADD CONSTRAINT progress_tasks_project_assignee_fk
        FOREIGN KEY (project_id, assignee_id)
        REFERENCES project_members(project_id, user_id)
        ON DELETE SET NULL (assignee_id);

ALTER TABLE progress_dependencies
    DROP CONSTRAINT progress_dependencies_task_id_fkey,
    DROP CONSTRAINT progress_dependencies_depends_on_task_id_fkey,
    ADD CONSTRAINT progress_dependencies_project_task_fk
        FOREIGN KEY (project_id, task_id)
        REFERENCES progress_tasks(project_id, task_id)
        ON DELETE CASCADE,
    ADD CONSTRAINT progress_dependencies_project_prerequisite_fk
        FOREIGN KEY (project_id, depends_on_task_id)
        REFERENCES progress_tasks(project_id, task_id)
        ON DELETE CASCADE;

ALTER TABLE progress_reminders
    DROP CONSTRAINT progress_reminders_task_id_fkey,
    DROP CONSTRAINT progress_reminders_milestone_id_fkey,
    ADD CONSTRAINT progress_reminders_project_task_fk
        FOREIGN KEY (project_id, task_id)
        REFERENCES progress_tasks(project_id, task_id)
        ON DELETE CASCADE,
    ADD CONSTRAINT progress_reminders_project_milestone_fk
        FOREIGN KEY (project_id, milestone_id)
        REFERENCES progress_milestones(project_id, milestone_id)
        ON DELETE CASCADE;

ALTER TABLE progress_proposals
    ADD CONSTRAINT progress_proposals_target_shape_check
        CHECK (
            status <> 'pending'
            OR (
                (proposal_type IN ('milestone.create', 'task.create') AND target_id IS NULL)
                OR
                (proposal_type IN ('milestone.update', 'task.update') AND target_id IS NOT NULL)
            )
        ),
    ADD CONSTRAINT progress_proposals_task_reference_shapes_check
        CHECK (
            status <> 'pending'
            OR proposal_type NOT IN ('task.create', 'task.update')
            OR (
                (
                    NOT (changes ? 'milestone_id')
                    OR (
                        jsonb_typeof(changes -> 'milestone_id') = 'string'
                        AND (
                            changes ->> 'milestone_id' = ''
                            OR progress_uuid_text_is_valid(changes ->> 'milestone_id')
                        )
                    )
                )
                AND
                (
                    NOT (changes ? 'assignee_id')
                    OR (
                        jsonb_typeof(changes -> 'assignee_id') = 'string'
                        AND (
                            changes ->> 'assignee_id' = ''
                            OR progress_uuid_text_is_valid(changes ->> 'assignee_id')
                        )
                    )
                )
                AND
                (
                    NOT (changes ? 'related_object_ids')
                    OR progress_jsonb_uuid_array_is_valid(changes -> 'related_object_ids')
                )
            )
        );
