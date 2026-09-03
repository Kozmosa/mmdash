ALTER TABLE article_blocks
    ADD COLUMN content_fingerprint TEXT NOT NULL DEFAULT '';

ALTER TABLE article_blocks
    ADD CONSTRAINT article_blocks_content_fingerprint_check
    CHECK (content_fingerprint = '' OR content_fingerprint ~ '^[0-9a-f]{64}$');
