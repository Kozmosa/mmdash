ALTER TABLE progress_settings
    ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT 'medium'
        CHECK (reasoning_effort IN ('none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'));
