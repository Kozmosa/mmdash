DROP INDEX IF EXISTS progress_proposals_evaluation_pending_idx;

UPDATE progress_proposals
SET proposal_type = 'task.update',
    changes = COALESCE(changes, '{}'::jsonb) || '{"status":"done"}'::jsonb
WHERE proposal_type = 'task.complete';
UPDATE progress_proposals
SET proposal_type = 'milestone.update',
    changes = COALESCE(changes, '{}'::jsonb) || '{"status":"completed"}'::jsonb
WHERE proposal_type = 'milestone.complete';

ALTER TABLE progress_proposals DROP CONSTRAINT IF EXISTS progress_proposals_proposal_type_check;
ALTER TABLE progress_proposals
    ADD CONSTRAINT progress_proposals_proposal_type_check
    CHECK (proposal_type IN (
        'milestone.create', 'milestone.update', 'task.create', 'task.update'
    ));
ALTER TABLE progress_proposals DROP CONSTRAINT IF EXISTS progress_proposals_target_shape_check;
ALTER TABLE progress_proposals
    ADD CONSTRAINT progress_proposals_target_shape_check
    CHECK (
        status <> 'pending'
        OR (
            (proposal_type IN ('milestone.create', 'task.create') AND target_id IS NULL)
            OR
            (proposal_type IN ('milestone.update', 'task.update') AND target_id IS NOT NULL)
        )
    );

ALTER TABLE progress_milestones DROP COLUMN IF EXISTS target_has_time;

ALTER TABLE progress_tasks DROP CONSTRAINT IF EXISTS progress_tasks_work_state_check;
ALTER TABLE progress_tasks DROP COLUMN IF EXISTS work_state;

ALTER TABLE progress_milestones DROP CONSTRAINT IF EXISTS progress_milestones_status_check;
ALTER TABLE progress_milestones
    ADD CONSTRAINT progress_milestones_status_check
    CHECK (status IN ('planned', 'in_progress', 'completed', 'cancelled'));

ALTER TABLE progress_tasks DROP CONSTRAINT IF EXISTS progress_tasks_status_check;
ALTER TABLE progress_tasks
    ADD CONSTRAINT progress_tasks_status_check
    CHECK (status IN ('todo', 'in_progress', 'blocked', 'done', 'cancelled'));
