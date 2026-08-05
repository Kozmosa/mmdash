ALTER TABLE notification_notifications
    ADD COLUMN IF NOT EXISTS action_type TEXT,
    ADD COLUMN IF NOT EXISTS action_resource_id TEXT,
    ADD COLUMN IF NOT EXISTS action_route TEXT;

ALTER TABLE notification_deliveries
    ADD COLUMN IF NOT EXISTS recipient_id UUID REFERENCES notification_recipients(recipient_id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS target_key TEXT,
    ADD COLUMN IF NOT EXISTS rule_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 5,
    ADD COLUMN IF NOT EXISTS provider_message_id TEXT,
    ADD COLUMN IF NOT EXISTS response_summary TEXT;

UPDATE notification_deliveries
SET target_key = 'project-channel:' || channel_key
WHERE target_key IS NULL OR target_key = '';

ALTER TABLE notification_deliveries
    ALTER COLUMN target_key SET NOT NULL,
    ADD CONSTRAINT notification_deliveries_max_attempts_check CHECK (max_attempts > 0);

ALTER TABLE notification_deliveries
    DROP CONSTRAINT IF EXISTS notification_deliveries_notification_id_channel_key_delivery_key_key;

ALTER TABLE notification_deliveries
    ADD CONSTRAINT notification_deliveries_notification_channel_target_key
    UNIQUE (notification_id, channel_key, target_key, delivery_key);

ALTER TABLE notification_delivery_attempts
    ADD COLUMN IF NOT EXISTS response_summary TEXT;

CREATE INDEX IF NOT EXISTS notification_recipients_user_idx
    ON notification_recipients (user_id, created_at DESC)
    WHERE user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS notification_notifications_resource_idx
    ON notification_notifications (resource_type, resource_id, type_key);
