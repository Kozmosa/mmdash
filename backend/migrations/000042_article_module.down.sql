DROP TABLE IF EXISTS article_zotero_bindings;
DROP TABLE IF EXISTS article_publications;
DROP TABLE IF EXISTS article_releases;
DROP FUNCTION IF EXISTS article_releases_reject_update();
DROP TABLE IF EXISTS article_build_outputs;
ALTER TABLE article_templates DROP CONSTRAINT IF EXISTS article_templates_test_build_fk;
DROP TABLE IF EXISTS article_builds;
DROP TABLE IF EXISTS article_templates;
DROP TABLE IF EXISTS article_commits;
DROP TABLE IF EXISTS article_references;
DROP TABLE IF EXISTS article_patches;
DROP TABLE IF EXISTS article_blocks;
DROP TABLE IF EXISTS article_drafts;
ALTER TABLE agent_runs DROP CONSTRAINT IF EXISTS agent_runs_source_check;
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_source_check
    CHECK (source IN ('message','regenerate','rerun','progress_evaluation')) NOT VALID;
ALTER TABLE agent_sessions DROP CONSTRAINT IF EXISTS agent_sessions_session_type_check;
ALTER TABLE agent_sessions ADD CONSTRAINT agent_sessions_session_type_check
    CHECK (session_type IN ('main','progress','experiment')) NOT VALID;
