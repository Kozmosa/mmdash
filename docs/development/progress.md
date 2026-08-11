# Stage 4 and Stage 6 Progress development

Progress is the Core authority for a Project's Milestone, Task, Dependency,
Reminder, and Progress Proposal records. The Web page, BFF routes, Data Hub
projections, and MCP `data.list`/`data.read` paths all read this boundary; no
consumer writes the Progress tables directly.

Stage 6 adds automatic, versioned evaluation without changing that ownership:
events and Cron create debounced requests, Core assembles a semantic snapshot,
the PostgreSQL Job Queue leases `progress.evaluate` to the Worker, and the
result returns through the Progress service transaction before any Task,
Proposal, risk, tracker state, Audit, or Outbox mutation is committed.

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

Migration `000028_progress_auto_tracking` owns:

- project settings for automatic/event/Cron tracking, debounce, minimum
  interval, selected Agent, and recoverable Hermes Cron reconciliation;
- debounced request and trigger history, including unique source-event replay
  protection and recoverable assembly leases;
- immutable evaluation input/output snapshots, SHA-256 input versions,
  attempts, safe failures, Agent Session/Run provenance, risks, and history;
- detected/effective tracker state and append-only human stage overrides;
- automatic Task/Proposal provenance and stable suggestion keys.

Migration `000037_progress_human_workbench` removes the cancelled Task and
Milestone domain states, adds Milestone date-versus-time precision, persists a
Task `work_state` independently from human completion, and adds explicit
`task.complete`/`milestone.complete` Proposal types plus an index for pending
evaluation review.

The request and Cron claim paths use `FOR UPDATE SKIP LOCKED`. Project-level
PostgreSQL advisory transaction locks serialize concurrent scheduling and
evaluation application. There is no Redis or second queue. A request merges
all triggers arriving inside the debounce window, respects the configured
minimum interval, and is re-claimable after an assembly lease expires. A
unique active input-version index merges queued, running, or successful
evaluations with identical semantic input.

Evaluation facts contain the Project problem, constraints, Data Hub objects
and activity, confirmed context, Milestones, Tasks, tracking settings, active
human override, and previous normalized output. Volatile timestamps, Progress
source Run IDs, and the evaluation/risk projections produced by Stage 6 itself
are excluded from the semantic version. Automatic Task updates are semantic
no-ops when protected/current fields already match; `task.create` suggestion
keys identify one logical Task, and identical pending Proposals are reused.
These rules let a real change converge instead of creating an evaluation loop.

Automatic trigger patterns include Repo commits, Model snapshots, archived
Experiments, completed Article builds, available Artifacts, confirmed Context,
ordinary Agent Runs, and human Progress Task/Milestone changes. Events carrying
`source_evaluation_id`, plus Agent Runs with `source=progress_evaluation`, are
ignored so evaluation output cannot recursively retrigger itself.

## Evaluator and failure lifecycle

`core_agent` is the production path. The Worker asks Core to execute the
evaluation; Core uses only the existing Agent/Hermes Session, Run, and Jobs
contracts, persists a `progress` Session and Run with
`source=progress_evaluation` plus a dedicated `source_evaluation_id`, and
returns the normalized JSON output. The parent-Run `source_run_id` foreign key
is never overloaded with an evaluation ID. No new or guessed Hermes API is
introduced. Hermes Cron reconciliation uses the existing
Jobs API and requires an active selected Agent.

`mock` is an explicit deterministic local/acceptance mode. It derives planning,
execution, or review from current Tasks and emits blocked-task risks without a
Hermes dependency. Event and manual evaluation work without an Agent in mock
mode; Hermes Cron remains disabled unless a real active Agent is selected.

The Worker validates an exact bounded output shape: stage, summary, changes,
completed/in-progress/blocked report items, risks, automatic `work_state_updates`,
reviewable suggestions, and pending questions.
Invalid JSON, unknown fields, invalid suggestion/reference types, oversized
output, provider failures, and exhausted retries become safe evaluation failure
codes/history. A human may retry only a terminal failed evaluation. Job lease,
retry, timeout, idempotency, and result completion remain owned by the existing
Core Job Queue.

## Mutation policy

- Browser sessions with `project.progress.manage` create and edit Milestones,
  Dependencies, Reminders, and review Proposals.
- A Milestone is never directly mutated by an Agent/API/Box identity. Such a
  caller submits `progress.proposals.create`; human review applies an accepted
  Proposal through the Progress service transaction.
- Agents never directly create, reschedule, or complete Tasks or Milestones.
  Every such evaluation result is a pending Proposal even when the legacy
  `auto_task_changes` setting is true. Direct non-session Task mutations return
  `PROGRESS_PROPOSAL_REQUIRED`.
- `todo`, `in_progress`, and `blocked` are automatic work-state assessments and
  apply without review. They are persisted in Task `work_state`; if a Task is
  human-completed, the assessment is retained without reopening it.
- `task.complete` and `milestone.complete` are completion suggestions, not
  completion facts. They become authoritative only after an individual or
  atomic batch human acceptance; rejection leaves the target incomplete.
- A human edit records the changed Task fields in `manual_override_fields`.
  Later evaluations may update only unprotected fields; a human edit also
  clears the current `source_evaluation_id` while preserving history.
- The Agent-detected stage is retained in evaluation history. A human stage
  override controls the effective Home/Progress stage and summary until
  explicitly cleared; later evaluations do not overwrite it.
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

Migration `000031_notification_routing_model` aligns routing with the Type
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
| `PROGRESS_TRACKING_POLL_INTERVAL` | `1s` | Idle request/Cron reconciliation scan interval |
| `PROGRESS_TRACKING_LEASE` | `2m` | Recoverable assembly/Cron claim lease |
| `PROGRESS_TRACKING_RETRY_DELAY` | `30s` | Retry delay after input assembly, queue, or Cron sync failure |
| `MMDASH_PROGRESS_EVALUATOR_MODE` | `core_agent` | `core_agent` or deterministic `mock` evaluator |

## HTTP and views

Core operations are under `/v1/projects/{projectId}/progress`; the browser-safe
one-to-one BFF routes are under `/api/projects/{projectId}/progress`.
`apps/web/src/app/projects/[projectId]/progress/page.tsx` renders one human
workbench with two views. Calendar supports day and cycling two/three/four-day
layouts, 15-minute drag/resize snapping, overlapping cards, a Milestone strip,
timed Milestone duplication in the grid, a centered current-time line, and a
detail drawer. Pointer movement renders a translucent drag ghost; top/bottom
resize changes the card geometry and time label continuously before the snapped
mutation is submitted. Completion, completion-Proposal review, drag, and resize
mutations update the local aggregate optimistically and roll back on failure,
so the normal interaction is not gated on a network round trip. Both copies of
a timed Milestone expose the same completion/review control.

TODO renders one waterfall with either date-only headings or date plus
morning/afternoon/evening/night headings. Calendar places the information rail
below; TODO places it to the right. The rail contains the latest report,
blockers, evaluation lifecycle, next eligible tracking time, today's open
count, the selected Progress Agent, manual evaluation, and atomic
approve/reject-all actions. Raw snapshots, hashes, Cron diagnostics, and
low-level Agent settings are not part of the normal Progress workspace.

## Data Hub and MCP

The following object types are projected and have authoritative readers:

| Object type         | Owner    |
| ------------------- | -------- |
| `milestone`         | Progress |
| `task`              | Progress |
| `progress_proposal` | Progress |
| `progress_evaluation` | Progress |
| `progress_risk`       | Progress |

Dependency and Reminder events remain domain events but are not projected as
Data Hub objects because Stage 4 does not expose standalone full-content
readers for them. MCP clients read the supported Progress objects through the
existing Core-routed `data.list` and `data.read` tools.

Stage 6 additionally exposes direct `progress.get` and
`progress.recalculate` MCP Tools. Both route through the generated Core Client,
exact Tool grants, Project RBAC, and MCP/Core Audit. `progress.get` is read-only;
an Agent may recalculate only as non-forced `cron`, while a human CLI identity
may request `manual` and optionally force past the minimum interval. Proposal
review and human override changes are never exposed as Agent Tools.

## RBAC, Audit, Outbox, and metrics

`project.progress.read` covers state/history reads;
`project.progress.evaluate` covers scheduling and is granted to the Agent role;
`project.progress.manage` remains human owner/maintainer control for settings,
retry, Proposal review, and stage override. Exact `progress.get` and
`progress.recalculate` Agent Tool grants are required in addition to role
permission.

Evaluation lifecycle, automatic Task/Proposal/risk mutations, settings, and
stage overrides write append-only Audit events and transactional Outbox events.
Metrics expose only bounded outcomes through
`mmdash_progress_evaluations_total`; Project/evaluation IDs and provider text
are never labels.

## Focused checks

```bash
pnpm contracts:generate
pnpm contracts:check
go test ./backend/internal/progress ./backend/internal/notification ./backend/internal/datahub ./backend/internal/project
MMDASH_TEST_DATABASE_URL=... go test ./backend/internal/progress -count=1
MMDASH_TEST_DATABASE_URL=... MMDASH_TEST_CORE_URL=... go test ./backend/internal/notification -count=1
pnpm api:check
uv run --project workers/mmdash-worker pytest workers/mmdash-worker/tests
uv run --project workers/mmdash-worker ruff check workers/mmdash-worker
pnpm check
```

For the Docker acceptance path, use the repository's standard Compose build,
`pnpm smoke`, health/log review, and `docker compose ... down` without `-v`.
