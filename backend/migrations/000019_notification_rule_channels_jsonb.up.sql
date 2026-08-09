ALTER TABLE notification_rules
    ALTER COLUMN channel_keys DROP DEFAULT;

ALTER TABLE notification_rules
    ALTER COLUMN channel_keys TYPE JSONB
    USING to_jsonb(channel_keys);

ALTER TABLE notification_rules
    ALTER COLUMN channel_keys SET DEFAULT '[]'::JSONB;
