CREATE TABLE artifact_folders (
    folder_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    parent_folder_id UUID,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 255 AND name !~ '[\r\n]'),
    position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (project_id, folder_id),
    FOREIGN KEY (project_id, parent_folder_id)
        REFERENCES artifact_folders(project_id, folder_id)
        ON DELETE RESTRICT
);

CREATE UNIQUE INDEX artifact_folders_project_parent_name_idx
    ON artifact_folders (
        project_id,
        COALESCE(parent_folder_id, '00000000-0000-0000-0000-000000000000'::uuid),
        lower(name)
    );
CREATE INDEX artifact_folders_project_parent_position_idx
    ON artifact_folders (project_id, parent_folder_id, position, folder_id);

ALTER TABLE artifact_artifacts
    ADD COLUMN folder_id UUID;
ALTER TABLE artifact_artifacts
    ADD CONSTRAINT artifact_artifacts_folder_fk
    FOREIGN KEY (project_id, folder_id)
    REFERENCES artifact_folders(project_id, folder_id)
    ON DELETE SET NULL;
CREATE INDEX artifact_artifacts_project_folder_idx
    ON artifact_artifacts (project_id, folder_id, updated_at DESC, artifact_id DESC)
    WHERE status <> 'trashed';
