DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM repo_repositories WHERE provider = 'managed'
    ) OR EXISTS (
        SELECT 1
        FROM settings_values
        WHERE type_key = 'repo.connection'
          AND public_values ->> 'provider' = 'managed'
    ) THEN
        RAISE EXCEPTION 'cannot downgrade while managed repositories exist';
    END IF;
END
$$;

ALTER TABLE repo_repositories
    DROP CONSTRAINT repo_repositories_provider_check;

UPDATE repo_repositories
SET provider = 'local'
WHERE provider = 'server_existing';

ALTER TABLE repo_repositories
    ADD CONSTRAINT repo_repositories_provider_check
        CHECK (provider IN ('github', 'local'));

UPDATE settings_values
SET public_values = jsonb_set(
        public_values,
        '{provider}',
        '"local"'::jsonb,
        false
    ),
    updated_at = NOW()
WHERE scope_type = 'project'
  AND type_key = 'repo.connection'
  AND public_values ->> 'provider' = 'server_existing';
