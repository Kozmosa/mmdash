# Progress API

Progress is the authoritative Project workflow for milestones, tasks,
dependencies, reminders, and human-reviewed proposals. The machine-readable
contracts are in [`contracts/openapi/core.yaml`](../../contracts/openapi/core.yaml)
and [`contracts/openapi/web-bff.yaml`](../../contracts/openapi/web-bff.yaml).

| Operation | Core | Browser BFF | Policy |
| --- | --- | --- | --- |
| Progress aggregate | `progress.get` | `bff.progress.get` | Project Progress read |
| Milestones | `progress.milestones.*` | `bff.progress.milestones.*` | Human session mutations |
| Tasks | `progress.tasks.*` | `bff.progress.tasks.*` | Agent Task changes obey `auto_task_changes` |
| Dependencies | `progress.dependencies.*` | `bff.progress.dependencies.*` | Human session mutations |
| Reminders | `progress.reminders.*` | `bff.progress.reminders.*` | Core automatically publishes due events to Notification |
| Proposals | `progress.proposals.*` | `bff.progress.proposals.*` | Non-human creation; human review |
| Settings | `progress.settings.*` | `bff.progress.settings.*` | Human Project management |

All browser routes use the selected Project context from the BFF session and
forward the browser identity to Core. Core performs the final Project RBAC and
identity-kind checks.

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
