DROP INDEX IF EXISTS artifact_uploads_agent_instance_idx;

ALTER TABLE artifact_uploads
    DROP COLUMN IF EXISTS agent_instance_id;

ALTER TABLE artifact_registry_entries
    DROP CONSTRAINT artifact_registry_entries_source_check;

ALTER TABLE artifact_registry_entries
    ADD CONSTRAINT artifact_registry_entries_source_check CHECK (source IN (
        'user_upload', 'experiment', 'model', 'article', 'system'
    ));

ALTER TABLE artifact_artifacts
    DROP CONSTRAINT artifact_artifacts_kind_check,
    DROP CONSTRAINT artifact_artifacts_source_check;

ALTER TABLE artifact_artifacts
    ADD CONSTRAINT artifact_artifacts_kind_check CHECK (kind IN (
        'problem', 'attachment', 'experiment_result',
        'model_file', 'article_build', 'other'
    )),
    ADD CONSTRAINT artifact_artifacts_source_check CHECK (source IN (
        'user_upload', 'experiment', 'model', 'article', 'system'
    ));
