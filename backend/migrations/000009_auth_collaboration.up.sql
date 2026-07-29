CREATE TABLE project_invitations (
    invitation_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    role TEXT NOT NULL
        CHECK (role IN ('owner', 'maintainer', 'editor', 'viewer', 'agent', 'box')),
    token_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'revoked', 'expired')),
    invited_by UUID NOT NULL REFERENCES auth_users(user_id),
    accepted_by UUID REFERENCES auth_users(user_id),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX project_invitations_pending_email_idx
    ON project_invitations (project_id, LOWER(email))
    WHERE status = 'pending';

CREATE INDEX project_invitations_token_idx
    ON project_invitations (token_hash)
    WHERE status = 'pending';

CREATE INDEX project_invitations_project_idx
    ON project_invitations (project_id, created_at DESC);
