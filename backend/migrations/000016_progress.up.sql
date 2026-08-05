CREATE TABLE IF NOT EXISTS progress_settings (
    project_id UUID PRIMARY KEY REFERENCES projects(project_id) ON DELETE CASCADE,
    auto_task_changes BOOLEAN NOT NULL DEFAULT TRUE,
    updated_by UUID NOT NULL REFERENCES auth_users(user_id),
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS progress_milestones (
    milestone_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 255),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned', 'in_progress', 'completed', 'cancelled')),
    critical BOOLEAN NOT NULL DEFAULT FALSE,
    start_at TIMESTAMPTZ,
    target_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    source TEXT NOT NULL CHECK (source IN ('human', 'proposal')),
    source_run_id TEXT NOT NULL DEFAULT '',
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    updated_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (target_at IS NULL OR start_at IS NULL OR target_at >= start_at),
    CHECK ((status = 'completed') = (completed_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS progress_milestones_project_target_idx
    ON progress_milestones (project_id, target_at, milestone_id);

CREATE TABLE IF NOT EXISTS progress_tasks (
    task_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    milestone_id UUID REFERENCES progress_milestones(milestone_id) ON DELETE SET NULL,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 255),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'todo'
        CHECK (status IN ('todo', 'in_progress', 'blocked', 'done', 'cancelled')),
    assignee_id UUID REFERENCES auth_users(user_id) ON DELETE SET NULL,
    start_at TIMESTAMPTZ,
    due_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    source TEXT NOT NULL CHECK (source IN ('human', 'agent', 'proposal')),
    source_run_id TEXT NOT NULL DEFAULT '',
    related_object_ids JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(related_object_ids) = 'array'),
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    updated_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (due_at IS NULL OR start_at IS NULL OR due_at >= start_at),
    CHECK ((status = 'done') = (completed_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS progress_tasks_project_status_due_idx
    ON progress_tasks (project_id, status, due_at, task_id);
CREATE INDEX IF NOT EXISTS progress_tasks_project_milestone_idx
    ON progress_tasks (project_id, milestone_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS progress_dependencies (
    dependency_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES progress_tasks(task_id) ON DELETE CASCADE,
    depends_on_task_id UUID NOT NULL REFERENCES progress_tasks(task_id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'blocks' CHECK (kind IN ('blocks', 'relates_to')),
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (task_id, depends_on_task_id),
    CHECK (task_id <> depends_on_task_id)
);

CREATE INDEX IF NOT EXISTS progress_dependencies_project_idx
    ON progress_dependencies (project_id, task_id, dependency_id);

CREATE TABLE IF NOT EXISTS progress_reminders (
    reminder_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    task_id UUID REFERENCES progress_tasks(task_id) ON DELETE CASCADE,
    milestone_id UUID REFERENCES progress_milestones(milestone_id) ON DELETE CASCADE,
    remind_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'triggered', 'cancelled')),
    note TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL CHECK (source IN ('human', 'system')),
    triggered_at TIMESTAMPTZ,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK ((task_id IS NOT NULL) <> (milestone_id IS NOT NULL)),
    CHECK ((status = 'triggered') = (triggered_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS progress_reminders_due_idx
    ON progress_reminders (project_id, status, remind_at, reminder_id);

CREATE TABLE IF NOT EXISTS progress_proposals (
    proposal_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    proposal_type TEXT NOT NULL CHECK (proposal_type IN (
        'milestone.create', 'milestone.update', 'task.create', 'task.update'
    )),
    target_id UUID,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 255),
    rationale TEXT NOT NULL DEFAULT '',
    changes JSONB NOT NULL CHECK (jsonb_typeof(changes) = 'object'),
    source TEXT NOT NULL CHECK (source IN ('agent', 'system')),
    source_run_id TEXT NOT NULL DEFAULT '',
    proposed_by UUID NOT NULL REFERENCES auth_users(user_id),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'rejected')),
    reviewed_by UUID REFERENCES auth_users(user_id),
    reviewed_at TIMESTAMPTZ,
    review_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS progress_proposals_project_status_idx
    ON progress_proposals (project_id, status, created_at DESC, proposal_id DESC);
