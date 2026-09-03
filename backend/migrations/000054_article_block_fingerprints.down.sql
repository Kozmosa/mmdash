ALTER TABLE article_blocks
    DROP CONSTRAINT IF EXISTS article_blocks_content_fingerprint_check,
    DROP COLUMN IF EXISTS content_fingerprint;
