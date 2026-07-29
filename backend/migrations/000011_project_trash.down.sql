DROP INDEX IF EXISTS projects_trash_expiry_idx;

ALTER TABLE projects
    DROP CONSTRAINT projects_trash_window_check;

UPDATE projects
SET archived_at = COALESCE(archived_at, deleted_at)
WHERE deleted_at IS NOT NULL;

ALTER TABLE projects
    DROP COLUMN purge_at,
    DROP COLUMN deleted_at;
