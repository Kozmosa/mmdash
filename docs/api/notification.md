# Notification 3.17 API

Notification is the authority for user Inbox state and project external
delivery state. The source Project and Progress modules only publish stable
events.

The routing model is deliberately asymmetric. A registered Notification Type
owns its `required / default_on / optional / disabled` Inbox policy and whether
external delivery is allowed. A Project Notification Rule controls only the
additional external route (`external_enabled`, `channel_keys`,
`minimum_priority`, and `version`); it cannot enable or disable Inbox delivery.
`project.invitation.received` is required in the recipient's Inbox and cannot
be sent to a project group channel. `progress.reminder.due` is default-on in
Inbox and may additionally use an enabled external channel.

The Inbox API is human-only: Agent and Box identities receive `403`, and an
Inbox item owned by another user is indistinguishable from not found. Read,
archive, and business outcome are separate fields. Invitation outcomes are
updated by invitation ID and never change a user's read or archive state.
The list supports unread/read, active/resolved/revoked/expired, the named
`processed` outcome group, archived state, project, type, occurrence-time
range, and cursor pagination. `mark-all-read` accepts project and type scope.

Inbox copy is read from the versioned, controlled `rendered_snapshot`; the Web
does not turn arbitrary event data into user-facing HTML or copy. The first
version stores a plain-text `title` and `body` snapshot.

Project channel routes are a safe composition over Settings. Secrets are
encrypted at rest and returned only as redacted values. A channel that has no
saved Settings row yet is not an error: it reads as `enabled: false` and
`configured: false` with version `0`, so the Web can show "尚未配置凭据" instead
of a read failure. An unknown channel type still returns `404`. Delivery
diagnostics contain status, retry counters, bounded error codes/messages,
channel key, and Settings version; they never expose a URL query, secret, or
complete provider response.
Rule reads/updates, Delivery diagnostics, and explicit retries require Project
`settings.manage` (owner or maintainer). Retry requires a non-empty human
reason and records Audit metadata.

The first registered type is `project.invitation.received`, sourced from
`project.member.invited`. `progress.reminder.due` is also persisted through the
same registry. External delivery is opt-in through a Project Notification Rule
and the `notification.feishu_webhook` or `notification.generic_webhook`
Settings type. Sends are performed by the Go Core Delivery Processor with
PostgreSQL leases and provider classification; Python Worker and Progress do
not participate.

Invitation Inbox records carry the typed browser-safe action
`project.invitation.accept` with the invitation ID only. The Web action calls
Project's protected invitation command; Notification never mutates Project
membership itself. Revoked and expired invitations also cancel pending or
retrying deliveries while preserving Inbox read/archive state.

Rule updates include the version returned by the preceding GET (use `0` when
creating the first rule). A stale version returns `409 NOTIFICATION_RULE_CONFLICT`;
the update never overwrites a newer Rule. A rule that has not been materialized
yet returns version `0` and omits update metadata until its first PUT.

Delivery diagnostics retain the project target key, Rule and Settings snapshot
versions, bounded attempt count, and safe response summary metadata. Explicit
retry creates a new `retry:{id}` delivery key; it never reopens the original
delivery.

Migration `000024_notification_routing_model` removes the obsolete
`notification_rules.inbox_enabled` column and backfills controlled Inbox
render snapshots for existing development rows. The column removal is an
intentional contract correction to the v0.1 design baseline, not a user-level
preference implementation.
