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
| Reminders | `progress.reminders.*` | `bff.progress.reminders.*` | Due event goes to NotificationAdapter |
| Proposals | `progress.proposals.*` | `bff.progress.proposals.*` | Non-human creation; human review |
| Settings | `progress.settings.*` | `bff.progress.settings.*` | Human Project management |

All browser routes use the selected Project context from the BFF session and
forward the browser identity to Core. Core performs the final Project RBAC and
identity-kind checks.

Progress events are documented in [`docs/events/catalog.md`](../events/catalog.md).
`milestone`, `task`, and `progress_proposal` are available through Data Hub
`data.list`/`data.read`; the Progress page itself uses the aggregate endpoint.
