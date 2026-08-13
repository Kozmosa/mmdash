ALTER TABLE agent_sessions DROP CONSTRAINT IF EXISTS agent_sessions_session_type_check;
ALTER TABLE agent_sessions ADD CONSTRAINT agent_sessions_session_type_check
    CHECK (session_type IN ('main','progress','experiment','article'));
ALTER TABLE agent_runs DROP CONSTRAINT IF EXISTS agent_runs_source_check;
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_source_check
    CHECK (source IN ('message','regenerate','rerun','progress_evaluation','artifact_semantic'));

CREATE TABLE article_drafts (
    project_id UUID PRIMARY KEY REFERENCES projects(project_id) ON DELETE CASCADE,
    revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
    yjs_update TEXT NOT NULL DEFAULT '',
    state_vector TEXT NOT NULL DEFAULT '',
    tiptap_json JSONB NOT NULL DEFAULT '{"type":"doc","content":[]}'::jsonb,
    manuscript_markdown TEXT NOT NULL DEFAULT '',
    references_bib TEXT NOT NULL DEFAULT '',
    manifest JSONB NOT NULL DEFAULT '{"schema_version":"1.0"}'::jsonb,
    actor_kind TEXT NOT NULL DEFAULT 'human' CHECK (actor_kind IN ('human','ai','restore')),
    provenance JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by TEXT,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE article_blocks (
    block_id TEXT NOT NULL,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    draft_revision BIGINT NOT NULL CHECK (draft_revision >= 1),
    position INTEGER NOT NULL CHECK (position >= 0),
    block_type TEXT NOT NULL,
    text_content TEXT NOT NULL DEFAULT '',
    attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (project_id, block_id)
);
CREATE INDEX article_blocks_revision_idx ON article_blocks(project_id, draft_revision, position);

CREATE TABLE article_patches (
    patch_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    base_revision BIGINT NOT NULL CHECK (base_revision >= 0),
    status TEXT NOT NULL CHECK (status IN ('proposed','accepted','rejected','superseded')),
    accepted_revision BIGINT,
    patch JSONB NOT NULL,
    rationale TEXT NOT NULL,
    provenance JSONB NOT NULL DEFAULT '{}'::jsonb,
    proposed_by TEXT NOT NULL,
    reviewed_by TEXT,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX article_patches_project_status_idx ON article_patches(project_id, status, created_at DESC);

CREATE TABLE article_references (
    reference_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    reference_type TEXT NOT NULL CHECK (reference_type IN ('problem','model_snapshot','experiment_result','artifact','zotero')),
    source_object_id TEXT NOT NULL,
    source_version_id TEXT NOT NULL,
    title TEXT NOT NULL,
    citation_key TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, reference_type, source_object_id, source_version_id)
);
CREATE UNIQUE INDEX article_references_citation_key_idx ON article_references(project_id, citation_key) WHERE citation_key IS NOT NULL;

CREATE TABLE article_commits (
    commit_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    draft_revision BIGINT NOT NULL CHECK (draft_revision >= 1),
    state_vector TEXT NOT NULL,
    yjs_update TEXT NOT NULL,
    tiptap_json JSONB NOT NULL,
    git_commit_sha TEXT NOT NULL,
    previous_git_commit_sha TEXT NOT NULL,
    message TEXT NOT NULL,
    manuscript_sha256 TEXT NOT NULL,
    references_sha256 TEXT NOT NULL,
    manifest_sha256 TEXT NOT NULL,
    frozen_references JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, git_commit_sha),
    UNIQUE (project_id, draft_revision, manuscript_sha256, references_sha256, manifest_sha256)
);
CREATE INDEX article_commits_project_created_idx ON article_commits(project_id, created_at DESC);

CREATE TABLE article_templates (
    template_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    artifact_id UUID NOT NULL REFERENCES artifact_artifacts(artifact_id) ON DELETE RESTRICT,
    artifact_version_id UUID NOT NULL REFERENCES artifact_versions(version_id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    template_version TEXT NOT NULL,
    manifest JSONB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('validating','ready','rejected')),
    test_build_id UUID,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, artifact_version_id)
);
CREATE INDEX article_templates_project_status_idx ON article_templates(project_id, status, updated_at DESC);

CREATE TABLE article_builds (
    build_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    build_kind TEXT NOT NULL CHECK (build_kind IN ('formal','preview','template_test')),
    status TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','superseded','cancelled')),
    commit_id UUID REFERENCES article_commits(commit_id) ON DELETE RESTRICT,
    draft_revision BIGINT,
    template_id UUID NOT NULL REFERENCES article_templates(template_id) ON DELETE RESTRICT,
    template_version_id UUID NOT NULL REFERENCES artifact_versions(version_id) ON DELETE RESTRICT,
    engine TEXT NOT NULL CHECK (engine IN ('auto','pdflatex','xelatex','lualatex')),
    bibliography_tool TEXT NOT NULL CHECK (bibliography_tool IN ('auto','bibtex','biber','none')),
    job_id UUID UNIQUE REFERENCES jobs(job_id) ON DELETE SET NULL,
    idempotency_key TEXT,
    toolchain JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code TEXT,
    error_message TEXT,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK ((build_kind = 'formal' AND commit_id IS NOT NULL AND draft_revision IS NULL) OR (build_kind <> 'formal' AND commit_id IS NULL AND draft_revision IS NOT NULL)),
    UNIQUE (project_id, build_kind, idempotency_key)
);
CREATE INDEX article_builds_project_created_idx ON article_builds(project_id, created_at DESC);
CREATE INDEX article_builds_commit_idx ON article_builds(commit_id, created_at DESC) WHERE commit_id IS NOT NULL;
CREATE UNIQUE INDEX article_preview_active_idx ON article_builds(project_id) WHERE build_kind='preview' AND status IN ('queued','running');

ALTER TABLE article_templates
    ADD CONSTRAINT article_templates_test_build_fk
    FOREIGN KEY (test_build_id) REFERENCES article_builds(build_id) ON DELETE SET NULL;

CREATE TABLE article_build_outputs (
    build_id UUID NOT NULL REFERENCES article_builds(build_id) ON DELETE CASCADE,
    output_role TEXT NOT NULL CHECK (output_role IN ('pdf','tex_source','source_zip','build_report','log','synctex')),
    artifact_id UUID NOT NULL REFERENCES artifact_artifacts(artifact_id) ON DELETE RESTRICT,
    artifact_version_id UUID NOT NULL REFERENCES artifact_versions(version_id) ON DELETE RESTRICT,
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (build_id, output_role)
);

CREATE TABLE article_releases (
    release_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    commit_id UUID NOT NULL REFERENCES article_commits(commit_id) ON DELETE RESTRICT,
    build_id UUID NOT NULL REFERENCES article_builds(build_id) ON DELETE RESTRICT,
    template_id UUID NOT NULL REFERENCES article_templates(template_id) ON DELETE RESTRICT,
    template_version_id UUID NOT NULL REFERENCES artifact_versions(version_id) ON DELETE RESTRICT,
    tag TEXT NOT NULL,
    title TEXT NOT NULL,
    notes TEXT NOT NULL,
    error_code TEXT,
    output_versions JSONB NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, tag),
    UNIQUE (project_id, commit_id, build_id)
);
CREATE INDEX article_releases_project_created_idx ON article_releases(project_id, created_at DESC);

CREATE FUNCTION article_releases_reject_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Article releases are immutable' USING ERRCODE = '55000';
END;
$$;
CREATE TRIGGER article_releases_immutable
    BEFORE UPDATE ON article_releases
    FOR EACH ROW EXECUTE FUNCTION article_releases_reject_update();

CREATE TABLE article_publications (
    publication_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('building','released','failed')),
    commit_id UUID NOT NULL REFERENCES article_commits(commit_id) ON DELETE RESTRICT,
    build_id UUID NOT NULL REFERENCES article_builds(build_id) ON DELETE RESTRICT,
    release_id UUID REFERENCES article_releases(release_id) ON DELETE RESTRICT,
    tag TEXT NOT NULL,
    title TEXT NOT NULL,
    notes TEXT NOT NULL,
    error_code TEXT,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, idempotency_key),
    UNIQUE (project_id, tag)
);

CREATE TABLE article_zotero_bindings (
    project_id UUID PRIMARY KEY REFERENCES projects(project_id) ON DELETE CASCADE,
    library_type TEXT NOT NULL CHECK (library_type IN ('user','group')),
    library_id TEXT NOT NULL,
    collection_key TEXT,
    secret_setting_key TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
