ALTER TABLE progress_settings
    DROP CONSTRAINT IF EXISTS progress_settings_enabled_event_types_array,
    DROP COLUMN IF EXISTS enabled_event_types;
