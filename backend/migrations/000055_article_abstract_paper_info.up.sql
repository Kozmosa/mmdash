-- Abstract is an independent collaborative Markdown document with its own
-- revision; paper info is a structured per-article document. Both freeze into
-- commits and builds alongside the existing body revision.

ALTER TABLE article_drafts
    ADD COLUMN abstract_markdown TEXT NOT NULL DEFAULT '',
    ADD COLUMN abstract_tiptap_json JSONB NOT NULL DEFAULT '{"type":"doc","content":[]}'::jsonb,
    ADD COLUMN abstract_yjs_update TEXT NOT NULL DEFAULT '',
    ADD COLUMN abstract_state_vector TEXT NOT NULL DEFAULT '',
    ADD COLUMN abstract_revision BIGINT NOT NULL DEFAULT 0 CHECK (abstract_revision >= 0),
    ADD COLUMN paper_info JSONB NOT NULL DEFAULT '{"schema_version":"1.0","fields":{}}'::jsonb,
    ADD COLUMN paper_info_revision BIGINT NOT NULL DEFAULT 0 CHECK (paper_info_revision >= 0);

ALTER TABLE article_commits
    ADD COLUMN abstract_markdown TEXT NOT NULL DEFAULT '',
    ADD COLUMN abstract_revision BIGINT NOT NULL DEFAULT 0 CHECK (abstract_revision >= 0),
    ADD COLUMN abstract_sha256 TEXT NOT NULL
        CHECK (abstract_sha256 ~ '^[0-9a-f]{64}$'),
    ADD COLUMN abstract_tiptap_json JSONB NOT NULL DEFAULT '{"type":"doc","content":[]}'::jsonb,
    ADD COLUMN abstract_state_vector TEXT NOT NULL DEFAULT '',
    ADD COLUMN abstract_yjs_update TEXT NOT NULL DEFAULT '',
    ADD COLUMN paper_info JSONB NOT NULL DEFAULT '{"schema_version":"1.0","fields":{}}'::jsonb,
    ADD COLUMN paper_info_revision BIGINT NOT NULL DEFAULT 0 CHECK (paper_info_revision >= 0),
    ADD COLUMN paper_info_sha256 TEXT NOT NULL
        CHECK (paper_info_sha256 ~ '^[0-9a-f]{64}$');

ALTER TABLE article_commit_operations
    ADD COLUMN abstract_markdown TEXT NOT NULL DEFAULT '',
    ADD COLUMN abstract_revision BIGINT NOT NULL DEFAULT 0 CHECK (abstract_revision >= 0),
    ADD COLUMN abstract_sha256 TEXT NOT NULL
        CHECK (abstract_sha256 ~ '^[0-9a-f]{64}$'),
    ADD COLUMN abstract_tiptap_json JSONB NOT NULL DEFAULT '{"type":"doc","content":[]}'::jsonb,
    ADD COLUMN abstract_state_vector TEXT NOT NULL DEFAULT '',
    ADD COLUMN abstract_yjs_update TEXT NOT NULL DEFAULT '',
    ADD COLUMN paper_info JSONB NOT NULL DEFAULT '{"schema_version":"1.0","fields":{}}'::jsonb,
    ADD COLUMN paper_info_revision BIGINT NOT NULL DEFAULT 0 CHECK (paper_info_revision >= 0),
    ADD COLUMN paper_info_sha256 TEXT NOT NULL
        CHECK (paper_info_sha256 ~ '^[0-9a-f]{64}$');
