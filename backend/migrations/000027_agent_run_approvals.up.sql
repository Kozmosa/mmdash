CREATE TABLE agent_run_approvals (
    run_id UUID NOT NULL REFERENCES agent_runs(run_id) ON DELETE CASCADE,
    approval_id TEXT NOT NULL CHECK (
        char_length(approval_id) BETWEEN 1 AND 500
    ),
    approval_order BIGINT GENERATED ALWAYS AS IDENTITY,
    status TEXT NOT NULL CHECK (
        status IN ('pending', 'responding', 'resolved', 'expired')
    ),
    claim_id UUID,
    requested_at TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (run_id, approval_id),
    CHECK (
        (status = 'pending' AND claim_id IS NULL AND resolved_at IS NULL)
        OR (status = 'responding' AND claim_id IS NOT NULL AND resolved_at IS NULL)
        OR (status = 'resolved' AND resolved_at IS NOT NULL)
        OR (status = 'expired' AND claim_id IS NULL AND resolved_at IS NOT NULL)
    )
);

CREATE INDEX agent_run_approvals_pending_idx
    ON agent_run_approvals (run_id, approval_order)
    WHERE status IN ('pending', 'responding');

COMMENT ON COLUMN agent_run_approvals.claim_id IS
    'Short-lived Core response claim. It is retained on resolved rows so the claiming request can complete idempotently when the Hermes SSE response wins the race; NULL means no Core request owns the approval.';

COMMENT ON COLUMN agent_run_approvals.approval_order IS
    'Insertion order used to map mmdash stable approval IDs onto Hermes v2026.8.3 oldest-pending resolution semantics.';
