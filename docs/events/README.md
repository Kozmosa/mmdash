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
