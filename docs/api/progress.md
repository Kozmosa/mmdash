# Progress API

Progress is the authoritative Project workflow for milestones, tasks,
dependencies, reminders, and human-reviewed proposals. The machine-readable
contracts are in [`contracts/openapi/core.yaml`](../../contracts/openapi/core.yaml)
and [`contracts/openapi/web-bff.yaml`](../../contracts/openapi/web-bff.yaml).

| Operation | Core | Browser BFF | Policy |
| --- | --- | --- | --- |
| Progress aggregate | `progress.get` | `bff.progress.get` | Project Progress read |
| Milestones | `progress.milestones.*` | `bff.progress.milestones.*` | Human session mutations |
| Tasks | `progress.tasks.*` | `bff.progress.tasks.*` | Human mutations; Agent scheduling changes are Proposals |
| Dependencies | `progress.dependencies.*` | `bff.progress.dependencies.*` | Human session mutations |
| Reminders | `progress.reminders.*` | `bff.progress.reminders.*` | Core automatically publishes due events to Notification |
| Proposals | `progress.proposals.*` | `bff.progress.proposals.*` | Non-human creation; individual or atomic batch human review |
| Settings | `progress.settings.*` | `bff.progress.settings.*` | Human Project management |

All browser routes use the selected Project context from the BFF session and
forward the browser identity to Core. Core performs the final Project RBAC and
identity-kind checks.

All Progress references are Project-scoped. A Task milestone, both Dependency
Task IDs, and a Reminder Task or Milestone must belong to the route Project. A
Task assignee must be a current member of that Project. Task
`related_object_ids` must contain UUIDs for non-hidden Data Hub objects in the
same Project. Duplicate related object IDs are accepted by the current
contract.

The same rules apply to Progress Proposal `target_id` and to Task reference
fields inside Proposal `changes`. Proposal creation validates references before
the pending record is stored, and acceptance validates them again in the review
transaction. Invalid, missing, hidden, nonmember, and cross-Project references
all return `400 PROGRESS_REFERENCE_INVALID` with the same generic message, so
the API does not reveal the existence of records outside the caller's Project.
Existing Task related-object IDs are stable historical references: a later
Data Hub hide does not erase them or block an unrelated Task update, but an
explicit update that submits a hidden ID is rejected.

Progress events are documented in [`docs/events/catalog.md`](../events/catalog.md).
`milestone`, `task`, and `progress_proposal` are available through Data Hub
`data.list`/`data.read`; the Progress page itself uses the aggregate endpoint.

Reminders move through `pending -> processing -> triggered`. A processing lease
that expires or a transient event-write failure returns the Reminder to
`pending` after a safe delay; exhausting the bounded attempt count moves it to
`failed`. `cancelled` remains terminal. The Core processor and the manual
trigger operation use the same PostgreSQL claim and completion path, so only
one of them can publish `progress.reminder.due`. The event ID is the globally
unique Reminder UUID, and the `triggered` state plus Outbox row commit in one
transaction.

Task completion and automatic work state are deliberately separate. `status`
retains the human completion authority (`done` versus open), while the
read-only `work_state` records the latest automatic `todo`, `in_progress`, or
`blocked` assessment. Reopening a completed Task therefore restores its last
work state instead of erasing the Agent assessment.

An evaluation never directly creates or reschedules a Task or Milestone and
never directly completes one. Those outputs become pending Progress Proposals;
`task.complete` and `milestone.complete` are the only completion suggestion
types. The batch-review endpoint applies all selected proposals in one
transaction or rolls the whole batch back. Work-state updates are the sole
automatic task mutation and do not require confirmation.
