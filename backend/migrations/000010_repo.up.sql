CREATE TABLE repo_repositories (
    repository_id UUID PRIMARY KEY,
    project_id UUID NOT NULL UNIQUE
        REFERENCES projects(project_id) ON DELETE RESTRICT,
    provider TEXT NOT NULL CHECK (provider IN ('github', 'local')),
    canonical_remote_url TEXT NOT NULL CHECK (length(canonical_remote_url) > 0),
    display_name TEXT NOT NULL CHECK (length(display_name) > 0),
    storage_key UUID NOT NULL UNIQUE,
    default_branch TEXT NOT NULL CHECK (length(default_branch) > 0),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN (
            'pending', 'cloning', 'configuring', 'ready',
            'syncing', 'error', 'disconnected'
        )),
    settings_version BIGINT NOT NULL CHECK (settings_version > 0),
    webhook_id UUID NOT NULL UNIQUE,
    connected_at TIMESTAMPTZ,
    last_synced_at TIMESTAMPTZ,
    last_error_code TEXT,
    last_error_message TEXT,
    sync_requested_at TIMESTAMPTZ,
    sync_started_at TIMESTAMPTZ,
    sync_locked_by TEXT,
    sync_lease_expires_at TIMESTAMPTZ,
    sync_attempts INTEGER NOT NULL DEFAULT 0 CHECK (sync_attempts >= 0),
    sync_source TEXT NOT NULL DEFAULT 'manual'
        CHECK (sync_source IN ('manual', 'webhook', 'poll')),
    next_sync_at TIMESTAMPTZ,
    cleanup_after TIMESTAMPTZ,
    created_by UUID NOT NULL REFERENCES auth_users(user_id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX repo_repositories_sync_claim_idx
    ON repo_repositories (
        next_sync_at,
        sync_requested_at,
        sync_lease_expires_at
    )
    WHERE sync_requested_at IS NOT NULL
      AND status <> 'disconnected';

CREATE INDEX repo_repositories_cleanup_idx
    ON repo_repositories (cleanup_after)
    WHERE status = 'disconnected' AND cleanup_after IS NOT NULL;

CREATE TABLE repo_workspaces (
    repository_id UUID NOT NULL
        REFERENCES repo_repositories(repository_id) ON DELETE CASCADE,
    workspace_kind TEXT NOT NULL
        CHECK (workspace_kind IN ('code', 'article', 'result')),
    remote_branch TEXT NOT NULL CHECK (length(remote_branch) > 0),
    local_branch TEXT NOT NULL
        CHECK (local_branch IN ('mmdash/code', 'mmdash/article', 'mmdash/result')),
    head_commit_sha TEXT
        CHECK (head_commit_sha IS NULL OR head_commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    tree_sha TEXT
        CHECK (tree_sha IS NULL OR tree_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'ready', 'missing', 'dirty', 'error')),
    worktree_relpath TEXT NOT NULL CHECK (length(worktree_relpath) > 0),
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repository_id, workspace_kind),
    UNIQUE (repository_id, remote_branch),
    UNIQUE (repository_id, local_branch),
    UNIQUE (repository_id, worktree_relpath)
);

CREATE TABLE repo_commits (
    repository_id UUID NOT NULL
        REFERENCES repo_repositories(repository_id) ON DELETE CASCADE,
    commit_sha TEXT NOT NULL
        CHECK (commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    tree_sha TEXT NOT NULL
        CHECK (tree_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    parent_shas TEXT[] NOT NULL DEFAULT '{}',
    author_name TEXT NOT NULL,
    author_email TEXT NOT NULL,
    author_time TIMESTAMPTZ NOT NULL,
    committer_name TEXT NOT NULL,
    committer_email TEXT NOT NULL,
    committer_time TIMESTAMPTZ NOT NULL,
    message TEXT NOT NULL CHECK (length(message) <= 100000),
    source TEXT NOT NULL
        CHECK (source IN ('connect', 'webhook', 'sync', 'mmdash', 'reference')),
    first_seen_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repository_id, commit_sha)
);

CREATE INDEX repo_commits_timeline_idx
    ON repo_commits (repository_id, committer_time DESC, commit_sha DESC);

CREATE TABLE repo_commit_events (
    repository_id UUID NOT NULL,
    workspace_kind TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    event_type TEXT NOT NULL
        CHECK (event_type IN ('repo.commit.created', 'repo.commit.detected')),
    event_id UUID NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repository_id, workspace_kind, commit_sha, event_type),
    FOREIGN KEY (repository_id, workspace_kind)
        REFERENCES repo_workspaces(repository_id, workspace_kind)
        ON DELETE CASCADE,
    FOREIGN KEY (repository_id, commit_sha)
        REFERENCES repo_commits(repository_id, commit_sha)
        ON DELETE CASCADE
);

CREATE TABLE repo_webhook_deliveries (
    provider TEXT NOT NULL CHECK (provider = 'github'),
    delivery_id TEXT NOT NULL CHECK (length(delivery_id) > 0),
    repository_id UUID NOT NULL
        REFERENCES repo_repositories(repository_id) ON DELETE CASCADE,
    event_name TEXT NOT NULL CHECK (length(event_name) > 0),
    ref_name TEXT,
    before_sha TEXT
        CHECK (before_sha IS NULL OR before_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    after_sha TEXT
        CHECK (after_sha IS NULL OR after_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    payload_sha256 TEXT NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    status TEXT NOT NULL
        CHECK (status IN ('accepted', 'ignored', 'processed', 'failed')),
    error_code TEXT,
    received_at TIMESTAMPTZ NOT NULL,
    processed_at TIMESTAMPTZ,
    PRIMARY KEY (provider, delivery_id)
);

CREATE INDEX repo_webhook_deliveries_repository_idx
    ON repo_webhook_deliveries (repository_id, received_at DESC);

CREATE TABLE repo_checkouts (
    checkout_id UUID PRIMARY KEY,
    repository_id UUID NOT NULL
        REFERENCES repo_repositories(repository_id) ON DELETE CASCADE,
    commit_sha TEXT NOT NULL
        CHECK (commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    purpose TEXT NOT NULL CHECK (length(purpose) BETWEEN 1 AND 100),
    created_by TEXT NOT NULL CHECK (length(created_by) > 0),
    checkout_relpath TEXT NOT NULL,
    status TEXT NOT NULL
        CHECK (status IN ('active', 'released', 'expired', 'error')),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    released_at TIMESTAMPTZ,
    UNIQUE (repository_id, checkout_relpath)
);

CREATE INDEX repo_checkouts_expiry_idx
    ON repo_checkouts (expires_at)
    WHERE status = 'active';

CREATE TABLE repo_commit_requests (
    repository_id UUID NOT NULL,
    workspace_kind TEXT NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    expected_head_sha TEXT NOT NULL
        CHECK (expected_head_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    commit_sha TEXT
        CHECK (commit_sha IS NULL OR commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    status TEXT NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed')),
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repository_id, workspace_kind, idempotency_key),
    FOREIGN KEY (repository_id, workspace_kind)
        REFERENCES repo_workspaces(repository_id, workspace_kind)
        ON DELETE CASCADE
);
