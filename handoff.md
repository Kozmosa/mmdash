# mmdash v0.1 Stage 4 Home and Progress handoff

- Updated: 2026-08-05
- Branch: `codex/stage-4-home-progress`
- Base: `origin/main@23057c4ebbea43d62ef388a63144fc4dad55be68`
- Delivery state: Stage 4 implementation complete; commit and PR handoff
  pending

## Status

Stage 4 is complete against the v0.1 implementation-order v0.4,
technical-architecture v0.4, and product-design v0.1 baselines. Project Home
now reads a real aggregate, and Progress is the authoritative Core boundary
for Milestone, Task, Dependency, Reminder, and Proposal state. No Model,
Experiment, Article, or Agent business behavior was introduced ahead of its
stage.

Human-controlled milestones and critical task changes remain human-session
operations. Agent-originated Task changes and Proposal creation go through
Progress, require a non-empty `source_run_id`, and write source and Audit
metadata. There is no direct database path from Web, BFF, MCP, or the Worker
into Progress-owned state.

## Delivered behavior

- Project Home aggregation for progress, todos, upcoming reminders,
  proposals, and real Empty States for Model, Experiment, Article, and Agent
  regions.
- Progress Core models and APIs for Milestone, Task, Dependency, Reminder,
  Proposal, and the Home aggregate, including human-only milestone/critical
  change rules and idempotent operations.
- Agent Task auto-change policy through the Progress service with mandatory
  run provenance and Audit records; completed and cancelled Tasks are excluded
  from Home todos.
- Proposal create, review, and apply workflow with explicit human review for
  critical changes.
- Web BFF aggregation and Progress routes plus Web Project Home, board/list,
  Gantt, today/reminder, and Proposal review views.
- Data Hub authoritative readers/projections for Milestone, Task, and
  Progress Proposal, with MCP `data.list`/`data.read` access through Core.
- Project-scoped RBAC, request/operation Audit coverage, stable Progress event
  envelopes, JSON Schemas, generated clients, API catalog entries, and module
  documentation.
- Minimal Stage 4 Notification前置能力: `progress.reminder.due` is consumed
  only by the NotificationAdapter, which persists an idempotent notification
  intent keyed by `source_event_id`. Progress never sends Feishu or Webhook
  requests directly.

## Contracts and persistence

Migration `000016_progress` owns the Progress tables, constraints, indexes,
and projections. Migration `000017_notification_stage4` owns only the minimal
NotificationAdapter intent boundary required for Reminder acceptance. No
Redis or other queue infrastructure was introduced; PostgreSQL remains the
Job Queue backend.

Progress create/update/delete and proposal lifecycle events are defined under
`contracts/events/`, included in the event catalog, and wired to the standard
Outbox/Data Hub path. OpenAPI source files, examples/catalog coverage,
generated Go/TypeScript clients, and the Progress API/development guides are
aligned. The coverage matrix and API/event indexes were updated.

## Verification

Passed:

- `pnpm contracts:generate`
- `pnpm contracts:check`
- `pnpm api:check`
- TypeScript lint, tests, and builds; Go formatting, lint, tests, and builds
  outside the existing Repo Git integration timeout suite; Python Worker
  lint/tests/build; and Web/BFF/MCP/CLI builds.
- Progress, NotificationAdapter, Data Hub, BFF aggregation, and Web tests.
- Docker Compose image build and full Stage 4 smoke acceptance. Because the
  workstation already had ports 3000, 3001, 5432, 8080, 9000, and 9001 in
  use, the same Compose stack ran on isolated host ports 13000, 13001, 15432,
  18080, 19000, and 19001. All services became healthy and `pnpm smoke`
  passed, including Web/BFF/Core, login/project creation, Worker Job,
  Outbox/Audit, Data Hub, MCP, and native CLI flows.
- Compose logs had zero panic/fatal/error matches and zero matches for the
  configured development credential values. The stack was stopped with
  `docker compose down`, never `down -v`, so named volumes were preserved.

`pnpm check` reached the test stage but did not exit successfully because five
pre-existing Repo Git integration cases repeatedly timed out in
`repo.worktree.add/reset` and `repo.commit.finalize`. A fresh uncached run
reproduced only those failures; all changed Stage 4 suites and the other Go,
CLI, TypeScript, and Python checks passed. The Docker smoke needed a temporary
`/tmp` Secret Service shim because this workstation has no writable Linux
keyring; the product CLI and its credential-store implementation were not
changed.

## Operational notes and boundaries

- Local bootstrap defaults remain `admin@mmdash.local` and
  `mmdash-local-admin` unless overridden by the documented environment
  variables.
- Reminder delivery stops at the stable event and NotificationAdapter intent
  boundary. Notification 3.17 still needs provider adapters (including
  Feishu/Webhook), user preferences, delivery/retry policy, and its complete
  operational surface in a later stage.
- Stage 5 Agent-session lifecycle, product Agent tokens, and Stage 6 automatic
  progress tracking are not implemented. The Stage 4 Task provenance rule is
  limited to the Progress mutation boundary and does not create a Stage 6
  tracker.
- Model, Experiment, Box, Sandbox, and Article remain honest Empty States on
  Home; no temporary or fake records are created for them.

The next implementation stage is Stage 5 Agent sessions.
