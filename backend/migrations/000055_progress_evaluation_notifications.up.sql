-- Existing projects that already opted into external Progress notifications
-- should also receive the newly supported completed-evaluation summary.
INSERT INTO notification_rules (
    rule_id,
    project_id,
    type_key,
    external_enabled,
    channel_keys,
    minimum_priority,
    updated_by,
    version,
    created_at,
    updated_at
)
SELECT
    gen_random_uuid(),
    project_id,
    'progress.evaluation.completed',
    external_enabled,
    channel_keys,
    minimum_priority,
    updated_by,
    1,
    NOW(),
    NOW()
FROM notification_rules
WHERE type_key = 'progress.reminder.due'
  AND external_enabled = TRUE
ON CONFLICT (project_id, type_key) DO NOTHING;
