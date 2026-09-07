ALTER TABLE progress_settings
    ADD COLUMN enabled_event_types JSONB NOT NULL DEFAULT '["agent.run.completed","article.build.completed","artifact.available","context.confirmed","experiment.archived","model.snapshot.created","progress.milestone.created","progress.milestone.updated","progress.task.created","progress.task.deleted","progress.task.updated","repo.commit.created","repo.commit.detected"]'::JSONB;

ALTER TABLE progress_settings
    ADD CONSTRAINT progress_settings_enabled_event_types_array
    CHECK (jsonb_typeof(enabled_event_types) = 'array');
