ALTER TABLE repo_repositories
    DROP CONSTRAINT repo_repositories_provider_check;

UPDATE repo_repositories
SET provider = 'server_existing'
WHERE provider = 'local';

ALTER TABLE repo_repositories
    ADD CONSTRAINT repo_repositories_provider_check
        CHECK (provider IN ('managed', 'github', 'server_existing'));

UPDATE settings_values
SET public_values = jsonb_set(
        public_values,
        '{provider}',
        '"server_existing"'::jsonb,
        false
    ),
    updated_at = NOW()
WHERE scope_type = 'project'
  AND type_key = 'repo.connection'
  AND public_values ->> 'provider' = 'local';
