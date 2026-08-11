ALTER TABLE auth_agent_tokens
    ADD COLUMN verification_challenge_hash CHAR(64);

-- Existing pending credentials were issued without challenge material and
-- cannot be completed under the new protocol. Revoke them without touching a
-- previously active credential; the normal rotation recovery flow can issue a
-- fresh pending credential and challenge.
UPDATE auth_agent_tokens
SET status = 'revoked',
    revoked_at = COALESCE(revoked_at, NOW())
WHERE status = 'pending'
  AND revoked_at IS NULL;

UPDATE agent_token_rotations
SET status = 'failed',
    safe_error_code = 'verification_challenge_reissue_required',
    updated_at = NOW()
WHERE status IN ('pending', 'awaiting_user', 'configuring', 'verifying');

DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    FOR constraint_name IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'auth_agent_tokens'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) LIKE '%verified_by_token_id%'
    LOOP
        EXECUTE format(
            'ALTER TABLE auth_agent_tokens DROP CONSTRAINT %I',
            constraint_name
        );
    END LOOP;
END $$;

ALTER TABLE auth_agent_tokens
    DROP COLUMN verified_by_token_id;

ALTER TABLE auth_agent_tokens
    ADD CONSTRAINT auth_agent_tokens_verification_challenge_hash_check
        CHECK (
            verification_challenge_hash IS NULL
            OR verification_challenge_hash ~ '^[0-9a-f]{64}$'
        ),
    ADD CONSTRAINT auth_agent_tokens_pending_challenge_check
        CHECK (
            status <> 'pending'
            OR verification_evidence_id IS NOT NULL
            OR verification_challenge_hash IS NOT NULL
        ),
    ADD CONSTRAINT auth_agent_tokens_verification_evidence_check
        CHECK (
            (
                verification_evidence_id IS NULL
                AND verification_method IS NULL
                AND verification_request_id IS NULL
                AND verification_session_id IS NULL
                AND verified_at IS NULL
            ) OR (
                verification_evidence_id IS NOT NULL
                AND verification_method = 'tools/list'
                AND verification_request_id IS NOT NULL
                AND verification_session_id IS NOT NULL
                AND verification_challenge_hash IS NULL
                AND verified_at IS NOT NULL
            )
        );
