ALTER TABLE auth_agent_tokens
    DROP CONSTRAINT IF EXISTS auth_agent_tokens_verification_evidence_check,
    DROP CONSTRAINT IF EXISTS auth_agent_tokens_pending_challenge_check,
    DROP CONSTRAINT IF EXISTS auth_agent_tokens_verification_challenge_hash_check;

ALTER TABLE auth_agent_tokens
    ADD COLUMN verified_by_token_id UUID REFERENCES auth_tokens(token_id);

-- Challenge verification has no legacy Gateway API-token principal to
-- restore. Keep its durable evidence readable while leaving the legacy
-- verifier column empty; a rolled-back Core will require a newly issued
-- pending credential before another activation.
ALTER TABLE auth_agent_tokens
    ADD CONSTRAINT auth_agent_tokens_verification_evidence_check
        CHECK (
            (
                verification_evidence_id IS NULL
                AND verification_method IS NULL
                AND verification_request_id IS NULL
                AND verification_session_id IS NULL
                AND verified_by_token_id IS NULL
                AND verified_at IS NULL
            ) OR (
                verification_evidence_id IS NOT NULL
                AND verification_method = 'tools/list'
                AND verification_request_id IS NOT NULL
                AND verification_session_id IS NOT NULL
                AND verified_at IS NOT NULL
            )
        );

ALTER TABLE auth_agent_tokens
    DROP COLUMN verification_challenge_hash;
