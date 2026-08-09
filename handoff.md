# mmdash v0.1 Stage 4 Home and Progress handoff

## 2026-08-09 Notification routing correction

Branch `codex/fix-notification` corrects the Stage 3.17/Stage 4 Notification
implementation against the v0.1 baseline without changing the source module
contract. The Type Registry now exclusively owns Inbox policy and Project
Notification Rules exclusively own optional external delivery. The obsolete
`inbox_enabled` API/database field is removed by migration
`000023_notification_routing_model`; invitation remains required Inbox-only,
while Progress reminders remain default-on in Inbox and optionally external.

The Web now has one global Inbox icon/unread badge on `/projects` and project
workspace chrome, a consistent global page shell, unread/all/processed and
archive views, project/type/time filters, pagination, scoped batch read, safe
rendered copy, and a detail route. Notification settings separate read-only
Inbox policy from owner/maintainer-only channel/rule/Delivery management;
explicit retry requires a reason. Focused Go, BFF, Web, contract, API, lint,
test, and build checks passed. The first full test command hit a workstation
permission error in pytest's system Temp directory; all 25 Python tests passed
when rerun with a worktree-local `--basetemp`. Docker, smoke, and Caddy checks
are intentionally omitted at the request of the user.

- Updated: 2026-08-05
- Branch: `codex/stage-4-home-progress`
- Base: `origin/main@23057c4ebbea43d62ef388a63144fc4dad55be68`
- Delivery state: Stage 4 implementation complete in commit `2d8a5a5`; PR #30
  remains open for review

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
- Complete Stage 4 Notification 3.17: the Core Type Registry consumes stable
  invitation, registration, invitation-lifecycle, and reminder events into
  canonical Notification, Recipient, Inbox, Rule, Delivery, and Delivery
  Attempt records. Invitation notifications persist only a typed,
  browser-safe accept Action; invitation outcomes preserve read/archive state
  and cancel pending/retrying deliveries. Project channel settings stay behind
  encrypted Settings; the Core Delivery Processor owns leases, bounded retry,
  safe provider errors, target/Rule/Settings snapshots, metrics, and
  Feishu/Generic Webhook adapters. Progress still publishes events only and
  never sends Feishu or Webhook requests directly.

## Contracts and persistence

Migration `000016_progress` owns the Progress tables, constraints, indexes,
and projections. Migration `000017_notification_stage4` remains a compatibility
bridge for old development data; migration `000018_notification_core` owns the
canonical Notification, Recipient, Inbox, Rule, Delivery, and Delivery Attempt
tables and backfills prior reminder intents. Migration
`000019_notification_rule_channels_jsonb` converts the already-applied Rule
`channel_keys` compatibility column from `text[]` to the design-baseline JSONB
representation. Migrations `000020_notification_authoritative_fields` and
`000021_notification_delivery_unique_target` add browser-safe Actions, target
keys, recipient/rule/settings snapshots, bounded attempts, provider diagnostics,
and the target-aware delivery idempotency key. Rule updates use optimistic
version checks. No Redis or other queue infrastructure was introduced;
PostgreSQL remains the Job Queue backend.

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
- Progress, Notification registry/adapter/processor, PostgreSQL Delivery and
  Core HTTP Rule/Action integration tests, invitation outcome cancellation,
  Notification metrics, Data Hub, BFF aggregation, and Web tests.
- Docker Compose image build and full Stage 4 smoke acceptance. Because the
  workstation already had ports 3000, 3001, 5432, 8080, 9000, and 9001 in
  use, the same Compose stack ran on isolated host ports 13000, 13001, 15432,
  18080, 19000, and 19001. All services became healthy and `pnpm smoke`
  passed, including Web/BFF/Core, login/project creation, Worker Job,
  Outbox/Audit, Data Hub, MCP, and native CLI flows.
- Compose logs had zero panic/fatal/error matches and zero matches for the
  configured development credential values. The stack was stopped with
  `docker compose down`, never `down -v`, so named volumes were preserved.

`pnpm check` reached the test stage but did not exit successfully because the
pre-existing Repo Git integration cases timed out in `repo.worktree.add/reset`
and related workspace tests (11 cases in the current run; the Repo package
failed after about 200 seconds). All changed Stage 4 suites and the other Go,
CLI, TypeScript, and Python checks passed; `pnpm build`, contract,
compatibility and API-catalog checks passed separately. The latest
`pnpm caddy:check` was blocked while Docker attempted to pull
`caddy:2.10-alpine` and hit a network timeout; Caddy configuration was not
changed by this work. The Docker smoke needed a temporary `/tmp` Secret
Service shim because this workstation has no writable Linux
keyring; the product CLI and its credential-store implementation were not
changed.

## Operational notes and boundaries

- Local bootstrap defaults remain `admin@mmdash.local` and
  `mmdash-local-admin` unless overridden by the documented environment
  variables.
- Notification 3.17 deliberately does not include user-level notification
  preferences, Email/Slack/Teams adapters, or a general arbitrary publish API.
  Reminder delivery stops at the stable Progress event boundary and enters
  Notification; only Core's trusted adapters may perform provider I/O.
- Project Notification settings now provide owner/maintainer channel save,
  test, delete, and Rule channel selection. A Rule cannot reference a channel
  unless that registered channel is configured and enabled.
- Stage 5 Agent-session lifecycle, product Agent tokens, and Stage 6 automatic
  progress tracking are not implemented. The Stage 4 Task provenance rule is
  limited to the Progress mutation boundary and does not create a Stage 6
  tracker.
- Model, Experiment, Box, Sandbox, and Article remain honest Empty States on
  Home; no temporary or fake records are created for them.

The next implementation stage is Stage 5 Agent sessions.
