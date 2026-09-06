ALTER TABLE repo_repositories
    ADD COLUMN sync_workspace_kinds TEXT[] NOT NULL
        DEFAULT ARRAY['code','article','result']::TEXT[],
    ADD COLUMN next_reconcile_at TIMESTAMPTZ;

ALTER TABLE repo_repositories
    ADD CONSTRAINT repo_repositories_sync_workspace_kinds_check
    CHECK (
        cardinality(sync_workspace_kinds) BETWEEN 1 AND 3
        AND sync_workspace_kinds <@ ARRAY['code','article','result']::TEXT[]
    );

UPDATE repo_repositories
SET next_reconcile_at = NOW()
WHERE provider IN ('github', 'server_existing')
  AND status <> 'disconnected';

CREATE INDEX repo_repositories_reconcile_idx
    ON repo_repositories (next_reconcile_at, repository_id)
    WHERE provider IN ('github', 'server_existing')
      AND status <> 'disconnected';
