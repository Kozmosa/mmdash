ALTER TABLE auth_agent_tokens DROP CONSTRAINT auth_agent_tokens_project_id_fkey;
ALTER TABLE auth_agent_tokens ADD CONSTRAINT auth_agent_tokens_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id);
ALTER TABLE agent_sessions DROP CONSTRAINT agent_sessions_project_id_fkey;
ALTER TABLE agent_sessions ADD CONSTRAINT agent_sessions_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id);
ALTER TABLE agent_project_grants DROP CONSTRAINT agent_project_grants_project_id_fkey;
ALTER TABLE agent_project_grants ADD CONSTRAINT agent_project_grants_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id);

ALTER TABLE artifact_preview_transfers DROP CONSTRAINT artifact_preview_transfers_project_id_fkey;
ALTER TABLE artifact_preview_transfers ADD CONSTRAINT artifact_preview_transfers_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE RESTRICT;
ALTER TABLE artifact_relations DROP CONSTRAINT artifact_relations_project_id_fkey;
ALTER TABLE artifact_relations ADD CONSTRAINT artifact_relations_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE RESTRICT;
ALTER TABLE artifact_registry_entries DROP CONSTRAINT artifact_registry_entries_project_id_fkey;
ALTER TABLE artifact_registry_entries ADD CONSTRAINT artifact_registry_entries_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE RESTRICT;
ALTER TABLE artifact_previews DROP CONSTRAINT artifact_previews_project_id_fkey;
ALTER TABLE artifact_previews ADD CONSTRAINT artifact_previews_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE RESTRICT;
ALTER TABLE artifact_uploads DROP CONSTRAINT artifact_uploads_project_id_fkey;
ALTER TABLE artifact_uploads ADD CONSTRAINT artifact_uploads_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE RESTRICT;
ALTER TABLE artifact_versions DROP CONSTRAINT artifact_versions_project_id_fkey;
ALTER TABLE artifact_versions ADD CONSTRAINT artifact_versions_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE RESTRICT;
ALTER TABLE artifact_blobs DROP CONSTRAINT artifact_blobs_project_id_fkey;
ALTER TABLE artifact_blobs ADD CONSTRAINT artifact_blobs_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE RESTRICT;
ALTER TABLE artifact_artifacts DROP CONSTRAINT artifact_artifacts_project_id_fkey;
ALTER TABLE artifact_artifacts ADD CONSTRAINT artifact_artifacts_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE RESTRICT;
ALTER TABLE repo_repositories DROP CONSTRAINT repo_repositories_project_id_fkey;
ALTER TABLE repo_repositories ADD CONSTRAINT repo_repositories_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(project_id) ON DELETE RESTRICT;

DROP TRIGGER projects_stage8_purge_schedule ON projects;
DROP FUNCTION enqueue_stage8_project_purge();
DROP TABLE project_stage8_purges;
