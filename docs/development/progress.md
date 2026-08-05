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
- Every mutation records an Audit event, including rejected policy attempts.

## Notification boundary

Progress publishes `progress.reminder.due` and never calls Feishu, Webhook, or
another external channel. Migration `000017_notification_stage4` provides the
minimal Stage 4 `NotificationAdapter` seam: its consumer accepts the stable
event and stores an idempotent notification intent keyed by `source_event_id`.

Full Notification 3.17 remains pending: Inbox recipients/read state, type and
template registration, Project routing rules, channel credentials, retries,
delivery attempts, Feishu/Webhook adapters, and their APIs are intentionally not
implemented in Stage 4.

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

| Object type | Owner |
| --- | --- |
| `milestone` | Progress |
| `task` | Progress |
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
pnpm api:check
pnpm check
```

For the Docker acceptance path, use the repository's standard Compose build,
`pnpm smoke`, health/log review, and `docker compose ... down` without `-v`.
