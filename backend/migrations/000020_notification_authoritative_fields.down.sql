DROP INDEX IF EXISTS notification_notifications_resource_idx;
DROP INDEX IF EXISTS notification_recipients_user_idx;

ALTER TABLE notification_delivery_attempts
    DROP COLUMN IF EXISTS response_summary;

ALTER TABLE notification_deliveries
    DROP CONSTRAINT IF EXISTS notification_deliveries_notification_channel_target_key;

ALTER TABLE notification_deliveries
    ADD CONSTRAINT notification_deliveries_notification_id_channel_key_deliver_key
    UNIQUE (notification_id, channel_key, delivery_key);

ALTER TABLE notification_deliveries
    DROP COLUMN IF EXISTS response_summary,
    DROP COLUMN IF EXISTS provider_message_id,
    DROP COLUMN IF EXISTS max_attempts,
    DROP COLUMN IF EXISTS rule_version,
    DROP COLUMN IF EXISTS target_key,
    DROP COLUMN IF EXISTS recipient_id;

ALTER TABLE notification_notifications
    DROP COLUMN IF EXISTS action_route,
    DROP COLUMN IF EXISTS action_resource_id,
    DROP COLUMN IF EXISTS action_type;
