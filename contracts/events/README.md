# Event contracts

`event-envelope.schema.json` is the mandatory transport wrapper for in-process
events, Outbox delivery, replay, and cross-process consumers. Domain modules
version their payload schemas independently while preserving the envelope.

Stage 1 Repo payloads are defined by:

- `repo.connected.schema.json`
- `repo.commit.created.schema.json`
- `repo.commit.detected.schema.json`

Stage 2 Artifact payloads are defined by:

- `artifact.created.schema.json`
- `artifact.available.schema.json`
- `artifact.deleted.schema.json`

Artifact event payloads contain stable IDs and immutable metadata only. They
never include signed URLs, provider upload IDs, object keys, credentials, file
content, or preview output.

The required fields are `event_id`, `event_type`, `schema_version`,
`occurred_at`, `producer`, `project_id`, `actor`, `correlation_id`,
`causation_id`, and `payload`.

Human-readable producer/consumer ownership lives in
[`docs/events/catalog.md`](../../docs/events/catalog.md). Durable publication,
idempotency, failures, and replay semantics live in
[`docs/api/events.md`](../../docs/api/events.md).
