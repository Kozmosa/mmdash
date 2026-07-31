DROP TRIGGER IF EXISTS artifact_previews_blob_reference_count ON artifact_previews;
DROP TRIGGER IF EXISTS artifact_versions_blob_reference_count ON artifact_versions;
DROP FUNCTION IF EXISTS maintain_artifact_blob_reference_count();
DROP TRIGGER IF EXISTS artifact_versions_immutable ON artifact_versions;
DROP FUNCTION IF EXISTS reject_available_artifact_version_mutation();
DROP TABLE IF EXISTS artifact_relations;
DROP TABLE IF EXISTS artifact_registry_entries;
DROP TABLE IF EXISTS artifact_previews;
DROP TABLE IF EXISTS artifact_upload_parts;
DROP TABLE IF EXISTS artifact_uploads;
ALTER TABLE artifact_artifacts
    DROP CONSTRAINT IF EXISTS artifact_artifacts_current_version_fk;
DROP TABLE IF EXISTS artifact_versions;
DROP TABLE IF EXISTS artifact_blobs;
DROP TABLE IF EXISTS artifact_artifacts;
