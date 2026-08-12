ALTER TABLE artifact_uploads
    ADD COLUMN agent_session_id UUID,
    ADD COLUMN agent_run_id UUID,
    ADD CONSTRAINT artifact_uploads_agent_run_pair_check CHECK (
        (agent_session_id IS NULL AND agent_run_id IS NULL)
        OR (agent_session_id IS NOT NULL AND agent_run_id IS NOT NULL)
    ),
    ADD CONSTRAINT artifact_uploads_agent_session_fk
        FOREIGN KEY (agent_session_id) REFERENCES agent_sessions(session_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT artifact_uploads_agent_run_fk
        FOREIGN KEY (agent_run_id) REFERENCES agent_runs(run_id)
        ON DELETE RESTRICT;

ALTER TABLE artifact_relations
    DROP CONSTRAINT artifact_relations_target_type_check;

ALTER TABLE artifact_relations
    ADD CONSTRAINT artifact_relations_target_type_check
        CHECK (target_type IN ('project', 'experiment', 'model', 'article', 'agent_run'));
