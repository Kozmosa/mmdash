ALTER TABLE article_builds
    ADD COLUMN progress_percent INTEGER NOT NULL DEFAULT 0
        CHECK (progress_percent BETWEEN 0 AND 100),
    ADD COLUMN progress_stage TEXT NOT NULL DEFAULT 'queued';

UPDATE article_builds
SET progress_percent = CASE
        WHEN status = 'succeeded' THEN 100
        WHEN status IN ('failed', 'superseded') THEN 100
        WHEN status = 'running' THEN 10
        ELSE 0
    END,
    progress_stage = CASE
        WHEN status = 'succeeded' THEN 'completed'
        WHEN status = 'failed' THEN 'failed'
        WHEN status = 'superseded' THEN 'superseded'
        WHEN status = 'running' THEN 'preparing'
        ELSE 'queued'
    END;
