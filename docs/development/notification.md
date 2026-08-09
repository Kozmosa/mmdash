# Notification module

`backend/internal/notification` is the single authority for notification
facts, recipients, a user's Inbox state, external routing, Delivery attempts,
and safe delivery diagnostics. Source modules publish registered domain events
or call a typed use case; they do not write Notification tables and do not call
Feishu or Webhook directly.

## Routing model

```text
domain event
  -> registered Notification Type and controlled renderer
  -> Notification fact + Recipient
       -> Inbox Item when the Type policy requires/defaults it
       -> external Delivery when the Type allows it and a Project Rule opts in
```

Inbox and external delivery are sibling channels over the same Notification
fact. Their state is independent:

- Inbox owns read/unread, archive, and business outcome;
- Delivery owns pending/sending/retrying/delivered/failed/cancelled and attempts;
- reading or archiving never resolves the source business object;
- accepting, revoking, or expiring an invitation never rewrites read/archive;
- an external failure never removes or blocks an Inbox item.

The Type Registry owns `inbox_policy`, `external_allowed`, source event/schema
versions, recipient resolver, template version, field allow-list, priority, and
lifecycle mapping. Project Rules are external-only and contain
`project_id / type_key / external_enabled / channel_keys / minimum_priority /
version`. Future user preferences may override `default_on`; Project settings
must not impersonate that user choice.

## Browser surfaces

The global `/inbox` route is available from an icon and unread badge on both
the project list and project workspace chrome. It defaults to unread and
unarchived items, and provides unread/all/processed views, project/type/time
filters, cursor pagination, scoped mark-all-read, archive, typed actions, and a
detail route at `/inbox/[inboxItemId]`.

Project Notification settings explain the Inbox policy but edit only external
channels and routing. Owners and maintainers can configure/test/delete
channels, update the Progress external rule, inspect safe Delivery diagnostics,
and retry with an explicit reason. Other members retain their personal Inbox
but do not receive Rule or Delivery diagnostics.

## Security and persistence

- Inbox endpoints are human-only and every item is scoped to its recipient.
- Inbox text uses a versioned controlled snapshot; arbitrary HTML and raw event
  rendering are forbidden.
- Channel secrets remain in encrypted Settings and are never stored on a
  Delivery.
- Rules use optimistic versions; Delivery stores Rule/Settings snapshots.
- PostgreSQL leases and `FOR UPDATE SKIP LOCKED` remain the queue mechanism.
- Migration `000024_notification_routing_model` removes the stale Project Rule
  Inbox switch and backfills controlled render snapshots.

## Focused verification

```powershell
go test ./backend/internal/notification
pnpm --filter @mmdash/web-bff test
pnpm --filter @mmdash/web test
pnpm --filter @mmdash/web build
pnpm contracts:check
pnpm api:check
```
