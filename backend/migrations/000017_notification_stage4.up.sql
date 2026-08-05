CREATE TABLE IF NOT EXISTS notification_intents (
    notification_id UUID PRIMARY KEY,
    source_event_id UUID NOT NULL UNIQUE,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    type_key TEXT NOT NULL CHECK (type_key = 'progress.reminder.due'),
    reminder_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'accepted' CHECK (status IN ('accepted', 'failed')),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS notification_intents_project_created_idx
    ON notification_intents (project_id, created_at DESC, notification_id DESC);
