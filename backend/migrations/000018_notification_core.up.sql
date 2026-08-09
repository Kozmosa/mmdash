CREATE TABLE IF NOT EXISTS notification_notifications (
    notification_id UUID PRIMARY KEY,
    type_key TEXT NOT NULL,
    template_version INTEGER NOT NULL CHECK (template_version > 0),
    source_event_id UUID NOT NULL,
    project_id UUID REFERENCES projects(project_id) ON DELETE CASCADE,
    actor_id UUID REFERENCES auth_users(user_id) ON DELETE SET NULL,
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    data JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(data) = 'object'),
    rendered_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(rendered_snapshot) = 'object'),
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (source_event_id, type_key)
);

CREATE INDEX IF NOT EXISTS notification_notifications_project_created_idx
    ON notification_notifications (project_id, occurred_at DESC, notification_id DESC);

CREATE TABLE IF NOT EXISTS notification_recipients (
    recipient_id UUID PRIMARY KEY,
    notification_id UUID NOT NULL REFERENCES notification_notifications(notification_id) ON DELETE CASCADE,
    recipient_key TEXT NOT NULL,
    user_id UUID REFERENCES auth_users(user_id) ON DELETE SET NULL,
    normalized_email TEXT,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (notification_id, recipient_key),
    CHECK ((user_id IS NOT NULL) OR (normalized_email IS NOT NULL AND normalized_email <> ''))
);
CREATE INDEX IF NOT EXISTS notification_recipients_email_idx
    ON notification_recipients (normalized_email, expires_at)
    WHERE normalized_email IS NOT NULL;

CREATE TABLE IF NOT EXISTS notification_inbox_items (
    inbox_item_id UUID PRIMARY KEY,
    notification_id UUID NOT NULL REFERENCES notification_notifications(notification_id) ON DELETE CASCADE,
    recipient_id UUID NOT NULL REFERENCES notification_recipients(recipient_id) ON DELETE CASCADE,
    read_state TEXT NOT NULL DEFAULT 'unread' CHECK (read_state IN ('unread', 'read')),
    archived_at TIMESTAMPTZ,
    outcome TEXT NOT NULL DEFAULT 'active' CHECK (outcome IN ('active', 'resolved', 'revoked', 'expired')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (recipient_id)
);
CREATE INDEX IF NOT EXISTS notification_inbox_user_state_idx
    ON notification_inbox_items (read_state, archived_at, created_at DESC, inbox_item_id);

CREATE TABLE IF NOT EXISTS notification_rules (
    rule_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    type_key TEXT NOT NULL,
    inbox_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    external_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    channel_keys JSONB NOT NULL DEFAULT '[]'::JSONB,
    minimum_priority TEXT NOT NULL DEFAULT 'normal' CHECK (minimum_priority IN ('low', 'normal', 'high', 'urgent')),
    updated_by UUID NOT NULL REFERENCES auth_users(user_id),
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, type_key)
);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    delivery_id UUID PRIMARY KEY,
    notification_id UUID NOT NULL REFERENCES notification_notifications(notification_id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(project_id) ON DELETE CASCADE,
    channel_key TEXT NOT NULL,
    settings_version BIGINT NOT NULL DEFAULT 0,
    delivery_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'delivered', 'retrying', 'failed', 'cancelled')),
    available_at TIMESTAMPTZ NOT NULL,
    lease_expires_at TIMESTAMPTZ,
    locked_by TEXT,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    last_error_code TEXT NOT NULL DEFAULT '',
    last_error_message TEXT NOT NULL DEFAULT '',
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (notification_id, channel_key, delivery_key)
);
CREATE INDEX IF NOT EXISTS notification_deliveries_claim_idx
    ON notification_deliveries (status, available_at, delivery_id);
CREATE INDEX IF NOT EXISTS notification_deliveries_project_idx
    ON notification_deliveries (project_id, created_at DESC, delivery_id DESC);

CREATE TABLE IF NOT EXISTS notification_delivery_attempts (
    attempt_id UUID PRIMARY KEY,
    delivery_id UUID NOT NULL REFERENCES notification_deliveries(delivery_id) ON DELETE CASCADE,
    attempt_number INTEGER NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    outcome TEXT NOT NULL CHECK (outcome IN ('sending', 'delivered', 'retrying', 'failed', 'cancelled')),
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    provider_status INTEGER,
    UNIQUE (delivery_id, attempt_number)
);

INSERT INTO notification_notifications (
    notification_id, type_key, template_version, source_event_id, project_id,
    resource_type, resource_id, data, occurred_at, created_at
)
SELECT notification_id, type_key, 1, source_event_id, project_id, 'reminder', reminder_id,
       jsonb_build_object('reminder_id', reminder_id), created_at, created_at
FROM notification_intents
ON CONFLICT (source_event_id, type_key) DO NOTHING;
