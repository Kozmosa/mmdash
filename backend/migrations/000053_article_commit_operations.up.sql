CREATE TABLE article_commit_operations (
    operation_id UUID PRIMARY KEY,
    commit_id UUID NOT NULL,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    operation_kind TEXT NOT NULL CHECK (operation_kind IN ('commit','publication')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    publication_id UUID,
    publication_key TEXT,
    template_id UUID REFERENCES article_templates(template_id) ON DELETE RESTRICT,
    engine TEXT,
    bibliography_tool TEXT,
    tag TEXT,
    title TEXT,
    notes TEXT,
    request_sha256 TEXT NOT NULL CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
    draft_revision BIGINT NOT NULL CHECK (draft_revision > 0),
    expected_head_sha TEXT NOT NULL
        CHECK (expected_head_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    state_vector TEXT NOT NULL,
    yjs_update TEXT NOT NULL,
    tiptap_json JSONB NOT NULL,
    manuscript TEXT NOT NULL,
    references_bib TEXT NOT NULL,
    manifest_bytes BYTEA NOT NULL,
    frozen_references JSONB NOT NULL DEFAULT '[]'::jsonb,
    message TEXT NOT NULL CHECK (length(message) BETWEEN 1 AND 500),
    manuscript_sha256 TEXT NOT NULL CHECK (manuscript_sha256 ~ '^[0-9a-f]{64}$'),
    references_sha256 TEXT NOT NULL CHECK (references_sha256 ~ '^[0-9a-f]{64}$'),
    manifest_sha256 TEXT NOT NULL CHECK (manifest_sha256 ~ '^[0-9a-f]{64}$'),
    status TEXT NOT NULL
        CHECK (status IN ('queued','running','retry_wait','succeeded','failed')),
    stage TEXT NOT NULL
        CHECK (stage IN ('queued','committing','publishing','completed','failed')),
    commit_sha TEXT
        CHECK (commit_sha IS NULL OR commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    previous_commit_sha TEXT
        CHECK (previous_commit_sha IS NULL OR previous_commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    error_code TEXT,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 10 CHECK (max_attempts BETWEEN 1 AND 20),
    next_attempt_at TIMESTAMPTZ NOT NULL,
    locked_by TEXT,
    lease_expires_at TIMESTAMPTZ,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    UNIQUE (project_id, idempotency_key),
    CHECK (
        (operation_kind = 'commit' AND publication_id IS NULL)
        OR
        (operation_kind = 'publication'
         AND publication_id IS NOT NULL
         AND length(publication_key) BETWEEN 1 AND 200
         AND template_id IS NOT NULL
         AND length(engine) > 0
         AND length(bibliography_tool) > 0
         AND length(tag) BETWEEN 1 AND 100
         AND length(title) BETWEEN 1 AND 255
         AND notes IS NOT NULL)
    )
);

CREATE INDEX article_commit_operations_claim_idx
    ON article_commit_operations (next_attempt_at, created_at)
    WHERE status IN ('queued','retry_wait','running');
