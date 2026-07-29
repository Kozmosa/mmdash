ALTER TABLE projects
    ADD COLUMN deleted_at TIMESTAMPTZ,
    ADD COLUMN purge_at TIMESTAMPTZ;

UPDATE projects
SET deleted_at = archived_at,
    purge_at = archived_at + INTERVAL '30 days',
    archived_at = NULL
WHERE archived_at IS NOT NULL;

ALTER TABLE projects
    ADD CONSTRAINT projects_trash_window_check
    CHECK (
        (deleted_at IS NULL AND purge_at IS NULL)
        OR (
            deleted_at IS NOT NULL
            AND purge_at IS NOT NULL
            AND purge_at > deleted_at
        )
    );

CREATE INDEX projects_trash_expiry_idx
    ON projects (purge_at)
    WHERE deleted_at IS NOT NULL;
