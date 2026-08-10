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

Notification 3.17 invitation lifecycle payloads are defined by:

- `project.member.invited.schema.json`
- `project.member.joined.schema.json`
- `project.invitation.revoked.schema.json`
- `project.invitation.expired.schema.json`
- `user.registered.schema.json`

Stage 5 Context Proposal payloads are defined by:

- `context.proposal.created.schema.json`
- `context.confirmed.schema.json`
- `context.proposal.rejected.schema.json`

Stage 5 Agent lifecycle payloads use the `agent.*.schema.json` files. They
cover instance and Prompt changes, Session lifecycle, Run terminal state, and
Agent Token issue, activation, rotation failure, and revocation. Agent event
payloads contain IDs, bounded status values, and safe error codes only; they
never contain Agent Token plaintext or hashes, Hermes or Dashboard
credentials, provider bodies, URLs, messages, Tool arguments/results, or
reasoning.

`agent.token.rotation_failed.old_token_remains_active` is `false` only when
initial provisioning fails before an active Token exists. If a replacement
Token fails while an old active Token exists, producers must emit `true`; the
old Token and its prior Tool scope remain valid.

Stage 6 automatic Progress tracking adds:

- `progress.evaluation.requested.schema.json`
- `progress.evaluation.queued.schema.json`
- `progress.evaluation.started.schema.json`
- `progress.evaluation.completed.schema.json`
- `progress.evaluation.failed.schema.json`
- `progress.risk.detected.schema.json`
- `progress.settings.updated.schema.json`
- `progress.stage.overridden.schema.json`
- `progress.stage.override_cleared.schema.json`

Automatic Task and Proposal events may carry `source_evaluation_id`; Agent Run
events may carry `source=progress_evaluation` and the originating evaluation
as `source_evaluation_id`. `source_run_id` remains reserved for parent Run
provenance. These references provide traceability and are also the
explicit loop-prevention boundary for the automatic event consumer.

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
