DROP INDEX IF EXISTS artifact_artifacts_project_folder_idx;
ALTER TABLE artifact_artifacts DROP CONSTRAINT IF EXISTS artifact_artifacts_folder_fk;
ALTER TABLE artifact_artifacts DROP COLUMN IF EXISTS folder_id;
DROP INDEX IF EXISTS artifact_folders_project_parent_position_idx;
DROP INDEX IF EXISTS artifact_folders_project_parent_name_idx;
DROP TABLE IF EXISTS artifact_folders;
