UPDATE project_invitations
SET status = 'revoked',
    revoked_at = COALESCE(revoked_at, declined_at)
WHERE status = 'declined';

DROP INDEX IF EXISTS project_members_single_owner_idx;

ALTER TABLE project_invitations
    DROP COLUMN declined_at;

ALTER TABLE project_invitations
    DROP CONSTRAINT project_invitations_status_check;

ALTER TABLE project_invitations
    ADD CONSTRAINT project_invitations_status_check
    CHECK (status IN ('pending', 'accepted', 'revoked', 'expired'));
