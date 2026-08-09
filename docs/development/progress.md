# Stage 4 Progress development

Progress is the Core authority for a Project's Milestone, Task, Dependency,
Reminder, and Progress Proposal records. The Web page, BFF routes, Data Hub
projections, and MCP `data.list`/`data.read` paths all read this boundary; no
consumer writes the Progress tables directly.

## Persistence

Migration `000016_progress` owns:

- `progress_milestones`, including human-owned critical milestones;
- `progress_tasks`, including source, `source_run_id`, assignee, dates, and
  related Data Hub object references;
- `progress_dependencies`, `progress_reminders`, `progress_proposals`, and
  `progress_settings`.

The source and source run are stored with every automatic Task or Proposal
change. Domain state and its Outbox event are written in the same PostgreSQL
transaction. PostgreSQL remains the queue and delivery backend.

Migration `000023_progress_reminder_processing` adds Reminder processing
leases, retry availability, bounded attempts, safe error diagnostics, and
cross-Project partial queue indexes. The state machine is:

```text
pending -> processing -> triggered
              |
              +-> pending (lease expiry or retryable event-write failure)
              +-> failed  (max attempts exhausted)
pending -> cancelled
```

The Core Reminder Processor claims globally due rows with
`FOR UPDATE SKIP LOCKED`, so multiple Core replicas can safely share the same
PostgreSQL queue. Claim is a short transaction. Completion updates the Reminder
to `triggered` and writes `progress.reminder.due` to `system_outbox` in one
transaction. The stable due event ID is the Reminder UUID. A crash after claim
leaves a finite lease; the next automatic scan or manual trigger recovers an
expired lease. Manual trigger uses the same claim/complete path and cannot race
the automatic processor into a duplicate event.

Migration `000024_progress_project_references` makes Project scope part of the
database identity for every Progress reference that PostgreSQL can express:
Task Milestones and assignees, both Dependency Task ends, and Reminder
Task/Milestone targets. Task assignees reference current `project_members`;
removing a member clears only `assignee_id`. Deleting a Milestone likewise
clears only `milestone_id`, while the Task's `project_id` remains unchanged.
The migration explicitly aborts on legacy cross-Project or malformed data and
does not repair or reassign records.

Task `related_object_ids` remain Data Hub stable IDs rather than foreign keys.
Progress validates their UUID shape and asks the Data Hub persistence boundary
to confirm that each object is in the same Project and is not `hidden`, using
the Progress mutation transaction. Duplicate IDs remain allowed by the current
OpenAPI contract. A later hide does not rewrite historical Tasks, so unrelated
Task updates remain possible; explicitly submitting the hidden ID again is
rejected.

## Mutation policy

- Browser sessions with `project.progress.manage` create and edit Milestones,
  Dependencies, Reminders, and review Proposals.
- A Milestone is never directly mutated by an Agent/API/Box identity. Such a
  caller submits `progress.proposals.create`; human review applies an accepted
  Proposal through the Progress service transaction.
- Agents may create or update ordinary Tasks only when `auto_task_changes` is
  enabled. The service requires a non-empty `source_run_id` for those changes.
- With `auto_task_changes=false`, ordinary automatic Task changes return
  `PROGRESS_PROPOSAL_REQUIRED` and must use a Proposal.
- Task deletion, Reminder/Dependency mutation, settings changes, and Proposal
  review remain human-session operations.
- Direct Task, Dependency, and Reminder mutations reject missing,
  cross-Project, nonmember, malformed, or hidden references with the single
  safe `PROGRESS_REFERENCE_INVALID` error. The response does not disclose
  whether a referenced record exists in another Project.
- Pending Progress Proposals validate their polymorphic target plus Task
  milestone, assignee, and `related_object_ids` changes at creation. Acceptance
  repeats the validation inside the review transaction, so deleted, hidden, or
  revoked references cannot be applied after human review begins.
- Every mutation records an Audit event, including rejected policy attempts.

## Notification boundary

Progress publishes `progress.reminder.due` and never calls Feishu, Webhook, or
another external channel. Migration `000017_notification_stage4` remains as a
compatibility bridge for existing development data; `000018_notification_core`
adds the canonical Notification, Recipient, Inbox, Rule, Delivery, and Delivery
Attempt tables. Notification consumes invitation lifecycle, registration, and
reminder events idempotently by `source_event_id + type_key`, claims pending
email recipients after registration, and preserves read/archive state while
applying invitation outcomes. Project channel secrets continue to use the
encrypted Settings boundary, and external sends run in the Core Delivery
Processor through the Feishu/Generic Webhook adapter registry.
Migration `000019_notification_rule_channels_jsonb` upgrades the originally
applied development `text[]` Rule channel column to the design-baseline JSONB
shape; Rule PUT carries a version and rejects stale updates with `409`.
Migrations `000020_notification_authoritative_fields` and
`000021_notification_delivery_unique_target` persist typed browser-safe
invitation Actions, target-aware Delivery idempotency, Rule/Settings snapshots,
bounded retry limits, safe provider diagnostics, and Notification metrics.

Migration `000024_notification_routing_model` aligns routing with the Type
Registry: the Reminder Type remains default-on in Inbox, while the Project Rule
can only opt into Feishu or Generic Webhook delivery. Progress never owns or
changes a user's Inbox preference.


Migration `000022_notification_invitation_outcomes` adds the durable
Invitation lifecycle serialization row used by both invitation creation and
terminal outcome consumers, so out-of-order or concurrent delivery cannot
recreate an active Inbox item after an invitation has ended.

The automatic due event uses the Reminder creator as the event actor, which
preserves the existing Notification recipient resolution without adding
recipient data to the payload. Processor metrics use fixed counters only, and
processor logs never include Reminder note content.

## Reminder processor configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `PROGRESS_REMINDER_POLL_INTERVAL` | `1s` | Idle scan interval |
| `PROGRESS_REMINDER_BATCH_SIZE` | `20` | Maximum rows claimed per scan |
| `PROGRESS_REMINDER_LEASE` | `30s` | Recoverable processing lease |
| `PROGRESS_REMINDER_RETRY_DELAY` | `2s` | Delay after an event-write failure |

## HTTP and views

Core operations are under `/v1/projects/{projectId}/progress`; the browser-safe
one-to-one BFF routes are under `/api/projects/{projectId}/progress`.
`apps/web/src/app/projects/[projectId]/progress/page.tsx` renders the same Core
aggregate as board, list, Gantt, today/overdue/blocked, reminder, and Proposal
review views. Project Home uses the same aggregate for real Milestone and open
Task counts. Model, Experiment, Article, and Agent cards stay typed Empty
States until their owning stages exist.

## Data Hub and MCP

The following object types are projected and have authoritative readers:

| Object type         | Owner    |
| ------------------- | -------- |
| `milestone`         | Progress |
| `task`              | Progress |
| `progress_proposal` | Progress |

Dependency and Reminder events remain domain events but are not projected as
Data Hub objects because Stage 4 does not expose standalone full-content
readers for them. MCP clients read the supported Progress objects through the
existing Core-routed `data.list` and `data.read` tools.

## Focused checks

```bash
pnpm contracts:generate
pnpm contracts:check
go test ./backend/internal/progress ./backend/internal/notification ./backend/internal/datahub ./backend/internal/project
MMDASH_TEST_DATABASE_URL=... go test ./backend/internal/progress -count=1
MMDASH_TEST_DATABASE_URL=... MMDASH_TEST_CORE_URL=... go test ./backend/internal/notification -count=1
pnpm api:check
pnpm check
```

For the Docker acceptance path, use the repository's standard Compose build,
`pnpm smoke`, health/log review, and `docker compose ... down` without `-v`.
