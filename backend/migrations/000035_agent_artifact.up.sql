ALTER TABLE artifact_artifacts
    DROP CONSTRAINT artifact_artifacts_kind_check,
    DROP CONSTRAINT artifact_artifacts_source_check;

ALTER TABLE artifact_artifacts
    ADD CONSTRAINT artifact_artifacts_kind_check CHECK (kind IN (
        'problem', 'attachment', 'experiment_result',
        'model_file', 'article_build', 'agent', 'other'
    )),
    ADD CONSTRAINT artifact_artifacts_source_check CHECK (source IN (
        'user_upload', 'experiment', 'model', 'article', 'agent', 'system'
    ));

ALTER TABLE artifact_registry_entries
    DROP CONSTRAINT artifact_registry_entries_source_check;

ALTER TABLE artifact_registry_entries
    ADD CONSTRAINT artifact_registry_entries_source_check CHECK (source IN (
        'user_upload', 'experiment', 'model', 'article', 'agent', 'system'
    ));

ALTER TABLE artifact_uploads
    ADD COLUMN agent_instance_id UUID
        REFERENCES agent_instances(agent_instance_id) ON DELETE RESTRICT;

CREATE INDEX artifact_uploads_agent_instance_idx
    ON artifact_uploads(agent_instance_id)
    WHERE agent_instance_id IS NOT NULL;
