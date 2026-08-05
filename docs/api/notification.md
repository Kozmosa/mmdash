# Notification 3.17 API

Notification is the authority for user Inbox state and project external
delivery state. The source Project and Progress modules only publish stable
events.

The Inbox API is human-only: Agent and Box identities receive `403`, and an
Inbox item owned by another user is indistinguishable from not found. Read,
archive, and business outcome are separate fields. Invitation outcomes are
updated by invitation ID and never change a user's read or archive state.

Project channel routes are a safe composition over Settings. Secrets are
encrypted at rest and returned only as redacted values. Delivery diagnostics
contain status, retry counters, bounded error codes/messages, channel key, and
Settings version; they never expose a URL query, secret, or complete provider
response.

The first registered type is `project.invitation.received`, sourced from
`project.member.invited`. `progress.reminder.due` is also persisted through the
same registry. External delivery is opt-in through a Project Notification Rule
and the `notification.feishu_webhook` or `notification.generic_webhook`
Settings type. Sends are performed by the Go Core Delivery Processor with
PostgreSQL leases and provider classification; Python Worker and Progress do
not participate.

Webhook configuration is validated before Settings persistence, during the
safe connection test, and again before delivery. HTTPS is required by default.
The deployment-only `NOTIFICATION_WEBHOOK_ALLOW_HTTP_LOOPBACK` exception is
disabled by default and permits HTTP only for explicit loopback hostnames or IP
literals in local development. Embedded URL credentials and fragments are
rejected, query strings remain supported for provider endpoints, and redirects
are returned as rejected provider responses rather than followed.

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
versions, bounded attempt count, and safe response summary metadata. Only a
`failed` Delivery can be manually retried. The retry creates a new
`retry:{sourceDeliveryId}` delivery key and never reopens the original
Delivery. Repeated or concurrent requests for the same failed Delivery are
idempotent and return the same retry Delivery. A non-failed Delivery returns
`409 NOTIFICATION_DELIVERY_RETRY_CONFLICT`; a missing Delivery returns `404`.
