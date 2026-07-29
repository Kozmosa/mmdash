ALTER TABLE project_invitations
    DROP CONSTRAINT project_invitations_status_check;

ALTER TABLE project_invitations
    ADD CONSTRAINT project_invitations_status_check
    CHECK (status IN ('pending', 'accepted', 'revoked', 'declined', 'expired'));

ALTER TABLE project_invitations
    ADD COLUMN declined_at TIMESTAMPTZ;

WITH ranked_owners AS (
    SELECT
        member.project_id,
        member.user_id,
        ROW_NUMBER() OVER (
            PARTITION BY member.project_id
            ORDER BY
                (member.user_id = project.created_by) DESC,
                member.created_at,
                member.user_id
        ) AS owner_rank
    FROM project_members AS member
    JOIN projects AS project USING (project_id)
    WHERE member.role = 'owner'
)
UPDATE project_members AS member
SET role = 'maintainer',
    updated_at = CURRENT_TIMESTAMP
FROM ranked_owners
WHERE member.project_id = ranked_owners.project_id
  AND member.user_id = ranked_owners.user_id
  AND ranked_owners.owner_rank > 1;

CREATE UNIQUE INDEX project_members_single_owner_idx
    ON project_members (project_id)
    WHERE role = 'owner';
