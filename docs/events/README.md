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

Stage 5 adds Context Proposal schemas and Agent lifecycle schemas for instance,
Prompt, Session, Run, and Agent Token transitions. Hermes streaming events are
not domain events: they use the normalized browser SSE contract in Core and
Web BFF OpenAPI. Agent events contain no message bodies, Tool inputs/results,
provider errors, URLs, or credential material.

A Token provisioning or rotation failure reports whether an old active Token
still exists. Initial auto provisioning has no old Token and reports `false`;
an actual replacement failure must preserve the old Token and report `true`.

Stage 9 Article schemas cover draft, Patch, Commit, Build, and Release
lifecycle. Article source bodies and generated build files never enter event
payloads; consumers resolve controlled Data Hub projections or immutable
Artifact Version pointers.
