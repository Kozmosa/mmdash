DELETE FROM artifact_relations WHERE target_type = 'agent_run';

ALTER TABLE artifact_relations
    DROP CONSTRAINT artifact_relations_target_type_check;

ALTER TABLE artifact_relations
    ADD CONSTRAINT artifact_relations_target_type_check
        CHECK (target_type IN ('project', 'experiment', 'model', 'article'));

ALTER TABLE artifact_uploads
    DROP CONSTRAINT artifact_uploads_agent_run_fk,
    DROP CONSTRAINT artifact_uploads_agent_session_fk,
    DROP CONSTRAINT artifact_uploads_agent_run_pair_check,
    DROP COLUMN agent_run_id,
    DROP COLUMN agent_session_id;
