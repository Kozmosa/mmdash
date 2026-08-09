# Event documentation

This directory indexes domain events, schema versions, producers, consumers,
idempotency behavior, and replay rules. Machine-readable schemas live under
`contracts/events`.

Start with the searchable [event catalog](catalog.md), then use
[`docs/api/events.md`](../api/events.md) for Outbox state, delivery,
idempotency, failure, and replay semantics.

Event-specific payload schemas evolve with their owning domain modules. Every
catalog entry must identify a producer, schema version, project scope, and
known consumers.

Stage 4 Progress payload schemas are under `contracts/events` and cover the
Milestone, Task, Dependency, Reminder, and Proposal lifecycle. Notification
3.17 adds invitation lifecycle and registration schemas; its consumer persists
Inbox facts and queues external Delivery records without making the source
module call a provider.

Stage 7 Model adds `model.sync.requested`, `model.source.changed`,
`model.question.changed`, and `model.snapshot.created`. Their payloads contain
only stable identifiers and bounded metadata; Notion credentials, temporary
file URLs, and model document content are excluded.
