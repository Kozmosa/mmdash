ALTER TABLE notification_deliveries
    ADD CONSTRAINT notification_deliveries_notification_id_channel_key_deliver_key
    UNIQUE (notification_id, channel_key, delivery_key);
