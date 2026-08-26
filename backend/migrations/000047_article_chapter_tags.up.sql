CREATE TABLE article_chapter_tags (
    chapter_tag_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    heading_block_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('unedited','unreviewed','reviewed','needs_review')),
    heading_block_type TEXT NOT NULL,
    heading_fingerprint TEXT NOT NULL,
    stale_reason TEXT,
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    reviewed_by TEXT,
    reviewed_at TIMESTAMPTZ,
    UNIQUE (project_id, heading_block_id)
);
CREATE INDEX article_chapter_tags_project_updated_idx ON article_chapter_tags(project_id, updated_at DESC);
