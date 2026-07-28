CREATE TABLE IF NOT EXISTS data_objects (
    object_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    object_type TEXT NOT NULL CHECK (object_type ~ '^[a-z][a-z0-9-]*$'),
    source_module TEXT NOT NULL CHECK (source_module ~ '^[a-z][a-z0-9-]*$'),
    source_id TEXT NOT NULL CHECK (length(source_id) > 0),
    title TEXT NOT NULL CHECK (length(title) > 0),
    summary TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(metadata) = 'object'),
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (source_module, object_type, source_id)
);

CREATE INDEX IF NOT EXISTS data_objects_project_updated_idx
    ON data_objects (project_id, updated_at DESC, object_id DESC);
CREATE INDEX IF NOT EXISTS data_objects_project_type_updated_idx
    ON data_objects (project_id, object_type, updated_at DESC, object_id DESC);

CREATE TABLE IF NOT EXISTS data_activity (
    activity_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    object_id UUID REFERENCES data_objects(object_id) ON DELETE SET NULL,
    event_id UUID,
    activity_type TEXT NOT NULL CHECK (activity_type ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$'),
    title TEXT NOT NULL CHECK (length(title) > 0),
    summary TEXT NOT NULL DEFAULT '',
    actor JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(actor) = 'object'),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(metadata) = 'object'),
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS data_activity_event_idx
    ON data_activity (event_id) WHERE event_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS data_activity_project_occurred_idx
    ON data_activity (project_id, occurred_at DESC, activity_id DESC);

CREATE TABLE IF NOT EXISTS data_context_proposals (
    proposal_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(title) > 0),
    content TEXT NOT NULL CHECK (length(content) > 0),
    context_type TEXT NOT NULL CHECK (length(context_type) > 0),
    source_object_ids JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(source_object_ids) = 'array'),
    rationale TEXT NOT NULL DEFAULT '',
    proposed_by UUID NOT NULL REFERENCES auth_users(user_id),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'rejected')),
    reviewed_by UUID REFERENCES auth_users(user_id),
    reviewed_at TIMESTAMPTZ,
    review_note TEXT NOT NULL DEFAULT '',
    promoted_context_id UUID,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS data_context_proposals_project_idx
    ON data_context_proposals (project_id, created_at DESC, proposal_id DESC);

CREATE TABLE IF NOT EXISTS data_context_entries (
    context_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(title) > 0),
    content TEXT NOT NULL CHECK (length(content) > 0),
    context_type TEXT NOT NULL CHECK (length(context_type) > 0),
    source_object_ids JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(source_object_ids) = 'array'),
    proposed_by UUID NOT NULL REFERENCES auth_users(user_id),
    confirmed_by UUID NOT NULL REFERENCES auth_users(user_id),
    confirmed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE data_context_proposals
    ADD CONSTRAINT data_context_proposals_promoted_context_fk
    FOREIGN KEY (promoted_context_id)
    REFERENCES data_context_entries(context_id);

CREATE INDEX IF NOT EXISTS data_context_entries_project_idx
    ON data_context_entries (project_id, confirmed_at DESC, context_id DESC);
