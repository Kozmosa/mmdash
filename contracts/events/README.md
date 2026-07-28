# Event contracts

`event-envelope.schema.json` is the mandatory transport wrapper for in-process
events, Outbox delivery, replay, and cross-process consumers. Domain modules
version their payload schemas independently while preserving the envelope.

The required fields are `event_id`, `event_type`, `schema_version`,
`occurred_at`, `producer`, `project_id`, `actor`, `correlation_id`,
`causation_id`, and `payload`.
