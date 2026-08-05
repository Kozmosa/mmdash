CREATE INDEX IF NOT EXISTS project_invitations_pending_expiry_idx
    ON project_invitations (expires_at, invitation_id)
    WHERE status = 'pending';
