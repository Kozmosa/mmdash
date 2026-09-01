DROP INDEX IF EXISTS repo_repositories_reconcile_idx;
ALTER TABLE repo_repositories
    DROP CONSTRAINT IF EXISTS repo_repositories_sync_workspace_kinds_check,
    DROP COLUMN IF EXISTS next_reconcile_at,
    DROP COLUMN IF EXISTS sync_workspace_kinds;
