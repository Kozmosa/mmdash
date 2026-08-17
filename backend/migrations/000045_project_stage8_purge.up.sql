CREATE TABLE project_stage8_purges (
    project_id UUID PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    cursor JSONB NOT NULL DEFAULT '{}'::jsonb,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error_code TEXT,
    last_error_message TEXT,
    requested_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (completed_at IS NULL OR status = 'completed')
);
CREATE INDEX project_stage8_purges_claim_idx
    ON project_stage8_purges (status, updated_at, project_id)
    WHERE status IN ('pending', 'running', 'failed');

ALTER TABLE repo_repositories DROP CONSTRAINT repo_repositories_project_id_fkey;
ALTER TABLE repo_repositories ADD CONSTRAINT repo_repositories_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;

ALTER TABLE artifact_artifacts DROP CONSTRAINT artifact_artifacts_project_id_fkey;
ALTER TABLE artifact_artifacts ADD CONSTRAINT artifact_artifacts_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;
ALTER TABLE artifact_blobs DROP CONSTRAINT artifact_blobs_project_id_fkey;
ALTER TABLE artifact_blobs ADD CONSTRAINT artifact_blobs_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;
ALTER TABLE artifact_versions DROP CONSTRAINT artifact_versions_project_id_fkey;
ALTER TABLE artifact_versions ADD CONSTRAINT artifact_versions_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;
ALTER TABLE artifact_uploads DROP CONSTRAINT artifact_uploads_project_id_fkey;
ALTER TABLE artifact_uploads ADD CONSTRAINT artifact_uploads_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;
ALTER TABLE artifact_previews DROP CONSTRAINT artifact_previews_project_id_fkey;
ALTER TABLE artifact_previews ADD CONSTRAINT artifact_previews_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;
ALTER TABLE artifact_registry_entries DROP CONSTRAINT artifact_registry_entries_project_id_fkey;
ALTER TABLE artifact_registry_entries ADD CONSTRAINT artifact_registry_entries_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;
ALTER TABLE artifact_relations DROP CONSTRAINT artifact_relations_project_id_fkey;
ALTER TABLE artifact_relations ADD CONSTRAINT artifact_relations_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;
ALTER TABLE artifact_preview_transfers DROP CONSTRAINT artifact_preview_transfers_project_id_fkey;
ALTER TABLE artifact_preview_transfers ADD CONSTRAINT artifact_preview_transfers_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;

ALTER TABLE agent_project_grants DROP CONSTRAINT agent_project_grants_project_id_fkey;
ALTER TABLE agent_project_grants ADD CONSTRAINT agent_project_grants_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;
ALTER TABLE agent_sessions DROP CONSTRAINT agent_sessions_project_id_fkey;
ALTER TABLE agent_sessions ADD CONSTRAINT agent_sessions_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;
ALTER TABLE auth_agent_tokens DROP CONSTRAINT auth_agent_tokens_project_id_fkey;
ALTER TABLE auth_agent_tokens ADD CONSTRAINT auth_agent_tokens_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE CASCADE;

CREATE OR REPLACE FUNCTION enqueue_stage8_project_purge()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.deleted_at IS NOT NULL
       AND NEW.purge_at IS NOT NULL
       AND (OLD.purge_at IS DISTINCT FROM NEW.purge_at OR OLD.deleted_at IS NULL) THEN
        INSERT INTO project_stage8_purges (
            project_id, status, cursor, attempts, requested_at, updated_at
        ) VALUES (
            NEW.project_id, 'pending', '{}'::jsonb, 0, NEW.purge_at, CURRENT_TIMESTAMP
        )
        ON CONFLICT (project_id) DO UPDATE
        SET status = 'pending',
            cursor = '{}'::jsonb,
            attempts = 0,
            last_error_code = NULL,
            last_error_message = NULL,
            requested_at = EXCLUDED.requested_at,
            started_at = NULL,
            completed_at = NULL,
            updated_at = CURRENT_TIMESTAMP;
    ELSIF NEW.deleted_at IS NULL AND OLD.deleted_at IS NOT NULL THEN
        DELETE FROM project_stage8_purges WHERE project_id = NEW.project_id;
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER projects_stage8_purge_schedule
    AFTER UPDATE OF deleted_at, purge_at ON projects
    FOR EACH ROW EXECUTE FUNCTION enqueue_stage8_project_purge();

INSERT INTO project_stage8_purges (
    project_id, status, cursor, attempts, requested_at, updated_at
)
SELECT project_id, 'pending', '{}'::jsonb, 0, purge_at, CURRENT_TIMESTAMP
FROM projects
WHERE deleted_at IS NOT NULL AND purge_at IS NOT NULL
ON CONFLICT (project_id) DO NOTHING;
