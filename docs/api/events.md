# Events, Outbox, and replay

Search terms: Event Bus, event envelope, `system_outbox`,
`FOR UPDATE SKIP LOCKED`, delivery, consumer, retry, failure history,
consumption idempotency, replay, `delivery_key`, `events.replay`.

The canonical HTTP schemas live in
[`contracts/openapi/core.yaml`](../../contracts/openapi/core.yaml). The
cross-process envelope is independently defined by
[`contracts/events/event-envelope.schema.json`](../../contracts/events/event-envelope.schema.json).

## Administrator operations

All event operations require a system-administrator Session or API token.

| `operationId`           | Purpose                                                |
| ----------------------- | ------------------------------------------------------ |
| `events.test.emit`      | Commit `system.test.emitted` through the Outbox path   |
| `events.consumers.list` | Inspect the deterministic in-process consumer registry |
| `events.get`            | Inspect publication and all live/replay deliveries     |
| `events.replay`         | Create an explicit replay for one or all consumers     |

`events.test.emit` is an engineering endpoint. It uses the same Outbox writer
and background Processor as domain events, so it is suitable for development
and smoke checks without introducing a second delivery path.

## Stable envelope

Every in-process, durable, and replayed event uses:

```json
{
  "event_id": "00000000-0000-4000-8000-000000000001",
  "event_type": "project.created",
  "schema_version": 1,
  "occurred_at": "2026-07-28T12:00:00Z",
  "producer": "project",
  "project_id": "00000000-0000-4000-8000-000000000002",
  "actor": { "user_id": "00000000-0000-4000-8000-000000000003" },
  "correlation_id": null,
  "causation_id": null,
  "payload": { "name": "Example" }
}
```

`event_type` is a stable lowercase dotted name. Payload schemas evolve
independently through `schema_version`; envelope fields are not repurposed.

## State and publication

Important domain mutations call the Outbox Writer using the same PostgreSQL
transaction as their authoritative state. A rollback therefore removes both
the state change and its event.

The Processor claims pending Outbox rows with `FOR UPDATE SKIP LOCKED`.
Publication creates one `live` delivery for every matching registered consumer
and changes the Outbox state to `published`. Publisher leases recover after
process failure and retry up to the persisted attempt limit.

Outbox states:

`pending → publishing → published | failed`

An event with no matching consumers is still published with an empty delivery
list. A later consumer receives historical events only through explicit replay.

## Delivery, failures, and consumption idempotency

Each `(event_id, consumer_name, delivery_key)` has an independent durable
delivery:

`pending → processing → succeeded | failed`

Consumer errors create an append-only `system_event_failures` row and return the
delivery to `pending` while attempts remain. The delivery retains a safe
`last_error`; terminal exhaustion changes it to `failed`. Processing leases
recover automatically after Core restarts.

Successful completion writes `system_event_consumptions` in the same
transaction as the delivery success. Its primary key is:

`(event_id, consumer_name, delivery_key)`

The normal key is `live`. A replay uses its replay UUID, which preserves normal
idempotency while intentionally allowing the same envelope to run again.

Delivery is at least once around the consumer call. Consumers that perform
their own side effects must also use `event_id` plus `delivery_key` as an
idempotent upsert/transaction boundary. In-process projections should never
generate a new domain event ID for a retry.

## Replay

Replay does not edit, clone, or republish the original envelope. It creates:

- one `system_event_replays` record containing requester and reason;
- one pending delivery per selected matching consumer;
- a new replay UUID used as each delivery's idempotency key.

Example:

```json
{
  "consumer_name": "platform.system-test-receipt",
  "reason": "verify projection recovery after deployment"
}
```

Omit `consumer_name` to target every currently registered consumer matching the
event type. An unknown or non-matching consumer returns
`EVENT_HAS_NO_MATCHING_CONSUMERS`.

## Runtime configuration

| Variable                | Default | Purpose                   |
| ----------------------- | ------- | ------------------------- |
| `OUTBOX_POLL_INTERVAL`  | `500ms` | Idle Processor delay      |
| `OUTBOX_EVENT_LEASE`    | `30s`   | Publication lease         |
| `OUTBOX_DELIVERY_LEASE` | `30s`   | Consumer processing lease |
| `OUTBOX_RETRY_DELAY`    | `2s`    | Baseline retry delay      |

See the [event catalog](../events/catalog.md) for producers, patterns, and
current consumers.
