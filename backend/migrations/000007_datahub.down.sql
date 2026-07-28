ALTER TABLE IF EXISTS data_context_proposals
    DROP CONSTRAINT IF EXISTS data_context_proposals_promoted_context_fk;
DROP TABLE IF EXISTS data_context_entries;
DROP TABLE IF EXISTS data_context_proposals;
DROP TABLE IF EXISTS data_activity;
DROP TABLE IF EXISTS data_objects;
