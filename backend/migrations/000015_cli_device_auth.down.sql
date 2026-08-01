DROP TABLE IF EXISTS auth_device_authorizations;
DROP INDEX IF EXISTS auth_sessions_refresh_token_hash_unique_idx;
ALTER TABLE auth_sessions DROP COLUMN IF EXISTS refresh_token_hash;
