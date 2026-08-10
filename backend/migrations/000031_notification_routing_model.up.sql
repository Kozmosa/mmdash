UPDATE notification_notifications
SET rendered_snapshot = CASE type_key
    WHEN 'project.invitation.received' THEN jsonb_build_object(
        'title', CASE
            WHEN COALESCE(data->>'project_name', '') <> ''
                THEN '加入“' || (data->>'project_name') || '”的邀请'
            ELSE '项目邀请'
        END,
        'body', CASE
            WHEN COALESCE(data->>'role', '') <> ''
                THEN '你被邀请以 ' || (data->>'role') || ' 身份加入项目。'
            ELSE '你收到了一条项目邀请。'
        END
    )
    WHEN 'progress.reminder.due' THEN jsonb_build_object(
        'title', COALESCE(NULLIF(data->>'title', ''), 'Progress 提醒到期'),
        'body', '项目中有一项需要你关注的 Progress 提醒。'
    )
    ELSE jsonb_build_object(
        'title', '需要关注的通知',
        'body', '项目中有一项需要你处理的消息。'
    )
END;

ALTER TABLE notification_rules
    DROP COLUMN IF EXISTS inbox_enabled;
