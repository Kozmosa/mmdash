DELETE FROM data_objects
WHERE source_module = 'repo';
ALTER TABLE data_objects
    DROP CONSTRAINT data_objects_object_type_check;
ALTER TABLE data_objects
    ADD CONSTRAINT data_objects_object_type_check
    CHECK (object_type ~ '^[a-z][a-z0-9-]*$');

DROP TABLE IF EXISTS repo_commit_requests;
DROP TABLE IF EXISTS repo_checkouts;
DROP TABLE IF EXISTS repo_webhook_deliveries;
DROP TABLE IF EXISTS repo_commit_events;
DROP TABLE IF EXISTS repo_commits;
DROP TABLE IF EXISTS repo_workspaces;
DROP TABLE IF EXISTS repo_repositories;
