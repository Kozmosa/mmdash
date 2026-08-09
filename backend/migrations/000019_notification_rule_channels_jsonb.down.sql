ALTER TABLE notification_rules
    ALTER COLUMN channel_keys DROP DEFAULT;

ALTER TABLE notification_rules
    ALTER COLUMN channel_keys TYPE TEXT[]
    USING ARRAY(
        SELECT jsonb_array_elements_text(channel_keys)
    );

ALTER TABLE notification_rules
    ALTER COLUMN channel_keys SET DEFAULT ARRAY[]::TEXT[];
