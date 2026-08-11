-- Progress is human-authoritative: automatic evaluation may update the
-- non-terminal work state, but creation, scheduling, and completion remain
-- reviewable proposals.

UPDATE progress_tasks SET status = 'todo' WHERE status = 'cancelled';
ALTER TABLE progress_tasks DROP CONSTRAINT IF EXISTS progress_tasks_status_check;
ALTER TABLE progress_tasks
    ADD CONSTRAINT progress_tasks_status_check
    CHECK (status IN ('todo', 'in_progress', 'blocked', 'done'));
ALTER TABLE progress_tasks
    ADD COLUMN IF NOT EXISTS work_state TEXT;
UPDATE progress_tasks
SET work_state = CASE WHEN status = 'done' THEN 'todo' ELSE status END
WHERE work_state IS NULL;
ALTER TABLE progress_tasks
    ALTER COLUMN work_state SET DEFAULT 'todo',
    ALTER COLUMN work_state SET NOT NULL;
ALTER TABLE progress_tasks
    DROP CONSTRAINT IF EXISTS progress_tasks_work_state_check;
ALTER TABLE progress_tasks
    ADD CONSTRAINT progress_tasks_work_state_check
    CHECK (work_state IN ('todo', 'in_progress', 'blocked'));

UPDATE progress_milestones SET status = 'planned' WHERE status = 'cancelled';
ALTER TABLE progress_milestones DROP CONSTRAINT IF EXISTS progress_milestones_status_check;
ALTER TABLE progress_milestones
    ADD CONSTRAINT progress_milestones_status_check
    CHECK (status IN ('planned', 'in_progress', 'completed'));

ALTER TABLE progress_milestones
    ADD COLUMN IF NOT EXISTS target_has_time BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE progress_proposals DROP CONSTRAINT IF EXISTS progress_proposals_proposal_type_check;
ALTER TABLE progress_proposals
    ADD CONSTRAINT progress_proposals_proposal_type_check
    CHECK (proposal_type IN (
        'milestone.create', 'milestone.update', 'milestone.complete',
        'task.create', 'task.update', 'task.complete'
    ));
ALTER TABLE progress_proposals DROP CONSTRAINT IF EXISTS progress_proposals_target_shape_check;
ALTER TABLE progress_proposals
    ADD CONSTRAINT progress_proposals_target_shape_check
    CHECK (
        status <> 'pending'
        OR (
            (proposal_type IN ('milestone.create', 'task.create') AND target_id IS NULL)
            OR
            (proposal_type IN ('milestone.update', 'milestone.complete', 'task.update', 'task.complete') AND target_id IS NOT NULL)
        )
    );

CREATE INDEX IF NOT EXISTS progress_proposals_evaluation_pending_idx
    ON progress_proposals (project_id, source_evaluation_id, created_at, proposal_id)
    WHERE status = 'pending';
