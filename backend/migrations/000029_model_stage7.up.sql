CREATE TABLE model_sources (
    source_id UUID PRIMARY KEY,
    project_id UUID NOT NULL UNIQUE REFERENCES projects(project_id) ON DELETE CASCADE,
    notion_root_page_id UUID NOT NULL,
    notion_root_page_url TEXT NOT NULL,
    notion_root_title TEXT NOT NULL DEFAULT '',
    auto_sync_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    auto_sync_interval_seconds INTEGER NOT NULL DEFAULT 300
        CHECK (auto_sync_interval_seconds BETWEEN 60 AND 86400),
    next_sync_at TIMESTAMPTZ,
    scheduler_lease_owner TEXT NOT NULL DEFAULT '',
    scheduler_lease_until TIMESTAMPTZ,
    sync_status TEXT NOT NULL DEFAULT 'idle'
        CHECK (sync_status IN ('idle','queued','running','succeeded','unchanged','failed','cancelled','timed_out')),
    last_sync_id UUID,
    last_synced_at TIMESTAMPTZ,
    last_error_code TEXT NOT NULL DEFAULT '',
    last_error_message TEXT NOT NULL DEFAULT '',
    discovered_page_count INTEGER NOT NULL DEFAULT 0 CHECK (discovered_page_count >= 0),
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    updated_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX model_sources_due_idx
    ON model_sources (next_sync_at, source_id)
    WHERE auto_sync_enabled AND next_sync_at IS NOT NULL;

CREATE TABLE model_source_pages (
    source_id UUID NOT NULL REFERENCES model_sources(source_id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    notion_page_id UUID NOT NULL,
    parent_page_id UUID,
    title TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 255),
    page_url TEXT NOT NULL,
    depth INTEGER NOT NULL CHECK (depth BETWEEN 1 AND 64),
    has_children BOOLEAN NOT NULL DEFAULT FALSE,
    last_seen_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (source_id, notion_page_id)
);

CREATE INDEX model_source_pages_project_depth_idx
    ON model_source_pages (project_id, depth, title, notion_page_id);

CREATE TABLE model_questions (
    question_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    source_id UUID NOT NULL REFERENCES model_sources(source_id) ON DELETE CASCADE,
    code TEXT NOT NULL CHECK (code ~ '^[A-Za-z][A-Za-z0-9_-]{0,31}$'),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 255),
    notion_page_id UUID NOT NULL,
    notion_page_url TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
    latest_snapshot_id UUID,
    snapshot_count INTEGER NOT NULL DEFAULT 0 CHECK (snapshot_count >= 0),
    sync_status TEXT NOT NULL DEFAULT 'idle'
        CHECK (sync_status IN ('idle','queued','running','succeeded','unchanged','failed','cancelled','timed_out')),
    last_sync_id UUID,
    last_synced_at TIMESTAMPTZ,
    last_error_code TEXT NOT NULL DEFAULT '',
    last_error_message TEXT NOT NULL DEFAULT '',
    archived_at TIMESTAMPTZ,
    created_by UUID NOT NULL REFERENCES auth_users(user_id),
    updated_by UUID NOT NULL REFERENCES auth_users(user_id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX model_questions_project_code_active_idx
    ON model_questions (project_id, lower(code)) WHERE archived_at IS NULL;
CREATE UNIQUE INDEX model_questions_source_page_active_idx
    ON model_questions (source_id, notion_page_id) WHERE archived_at IS NULL;
CREATE INDEX model_questions_project_position_idx
    ON model_questions (project_id, position, created_at, question_id)
    WHERE archived_at IS NULL;

CREATE TABLE model_syncs (
    sync_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    source_id UUID NOT NULL REFERENCES model_sources(source_id) ON DELETE CASCADE,
    question_id UUID REFERENCES model_questions(question_id) ON DELETE SET NULL,
    scope TEXT NOT NULL CHECK (scope IN ('source','question')),
    trigger TEXT NOT NULL CHECK (trigger IN ('manual','scheduled','settings')),
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued','running','succeeded','unchanged','failed','cancelled','timed_out')),
    job_id UUID NOT NULL UNIQUE REFERENCES jobs(job_id) ON DELETE RESTRICT,
    requested_by UUID NOT NULL REFERENCES auth_users(user_id),
    requested_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_snapshot_id UUID,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK ((scope = 'question') = (question_id IS NOT NULL))
);

CREATE UNIQUE INDEX model_syncs_active_source_idx
    ON model_syncs (source_id)
    WHERE scope = 'source' AND status IN ('queued','running');
CREATE UNIQUE INDEX model_syncs_active_question_idx
    ON model_syncs (question_id)
    WHERE scope = 'question' AND status IN ('queued','running');
CREATE INDEX model_syncs_project_requested_idx
    ON model_syncs (project_id, requested_at DESC, sync_id DESC);

ALTER TABLE model_sources
    ADD CONSTRAINT model_sources_last_sync_fk
    FOREIGN KEY (last_sync_id) REFERENCES model_syncs(sync_id) ON DELETE SET NULL;
ALTER TABLE model_questions
    ADD CONSTRAINT model_questions_last_sync_fk
    FOREIGN KEY (last_sync_id) REFERENCES model_syncs(sync_id) ON DELETE SET NULL;

CREATE TABLE model_snapshots (
    snapshot_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    question_id UUID NOT NULL REFERENCES model_questions(question_id) ON DELETE RESTRICT,
    previous_snapshot_id UUID REFERENCES model_snapshots(snapshot_id) ON DELETE SET NULL,
    notion_page_id UUID NOT NULL,
    notion_page_url TEXT NOT NULL,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 255),
    outline JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(outline) = 'array'),
    blocks JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(blocks) = 'array'),
    content_markdown TEXT NOT NULL,
    content_text TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    tags JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(tags) = 'array'),
    version_note TEXT NOT NULL DEFAULT '' CHECK (length(version_note) <= 4000),
    captured_at TIMESTAMPTZ NOT NULL,
    triggered_by UUID NOT NULL REFERENCES auth_users(user_id),
    metadata_updated_by UUID NOT NULL REFERENCES auth_users(user_id),
    metadata_updated_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (question_id, content_hash)
);

CREATE INDEX model_snapshots_question_time_idx
    ON model_snapshots (question_id, captured_at DESC, snapshot_id DESC);

ALTER TABLE model_questions
    ADD CONSTRAINT model_questions_latest_snapshot_fk
    FOREIGN KEY (latest_snapshot_id) REFERENCES model_snapshots(snapshot_id) ON DELETE SET NULL;
ALTER TABLE model_syncs
    ADD CONSTRAINT model_syncs_created_snapshot_fk
    FOREIGN KEY (created_snapshot_id) REFERENCES model_snapshots(snapshot_id) ON DELETE SET NULL;

CREATE TABLE model_snapshot_assets (
    snapshot_id UUID NOT NULL REFERENCES model_snapshots(snapshot_id) ON DELETE CASCADE,
    source_block_id TEXT NOT NULL,
    artifact_id UUID NOT NULL REFERENCES artifact_artifacts(artifact_id) ON DELETE RESTRICT,
    artifact_version_id UUID NOT NULL REFERENCES artifact_versions(version_id) ON DELETE RESTRICT,
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (snapshot_id, source_block_id, artifact_id)
);

CREATE INDEX model_snapshot_assets_artifact_idx
    ON model_snapshot_assets (artifact_id, artifact_version_id);
