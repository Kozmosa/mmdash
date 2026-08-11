# mmdash v0.1 Progress workbench refactor handoff

- Updated: 2026-08-11
- Branch: `codex/progress-refactor`
- Base: `main@9282b4e`
- Canonical migrations: continuous `000001` through
  `000033_progress_human_workbench` on this branch
- Delivery state: Progress human scheduling workbench, review policy, and real
  `progress` Session evaluation accepted in an isolated worktree; branch is
  committed separately and must not be merged until the user requests it

## 2026-08-11 Progress human workbench refactor

Progress now exposes only Calendar and TODO views. Calendar provides a day
button plus a cycling two/three/four-day button, fixed two-axis scrolling,
centered current-time positioning, a Milestone strip, optional timed Milestone
duplication in the time grid, 15-minute creation/move/resize snapping,
overlap lanes, translucent drag ghosts, live resize geometry, and one detail
drawer. TODO is one waterfall whose day-versus-period setting changes heading
depth only and never rewrites exact stored time.

Human completion is independent from the automatic `todo`, `in_progress`, and
`blocked` work assessment. Human completion renders a filled/faded card;
automatic completion remains an amber `task.complete` or
`milestone.complete` Proposal with explicit accept/reject controls. Agent
creation and scheduling changes likewise remain reviewable, including atomic
approve-all/reject-all. Work assessments apply directly without review. The
cancelled Task and Milestone states have been removed. Completion, review,
drag, and resize interactions update optimistically and roll back on failure.

Automatic evaluation creates a dedicated Agent Session with
`session_type=progress`. Against the bound nanako Hermes instance, evaluation
`7904c7dc-0ce2-47b0-94dd-2fc67b985d9e` succeeded through progress Session
`8c10d986-5520-4bdc-b5b5-bf2aca71bac2` and Run
`542c284b-e24c-49fe-abca-2b4bf82a4ca9`; its three creation/scheduling
suggestions remained pending until the browser batch-rejected them. A
controlled completion Proposal was also accepted through the card and became
human completion.

Acceptance evidence:

- Real browser checks used the provided account and the isolated nanako copy.
  A timed Milestone showed completion controls in both locations; completion
  and reopen rendered in 46 ms and 43 ms respectively. Drag produced a
  pointer-following ghost and faded source. Moving the resize edge by 18 px
  changed the card from 126 px to 144 px and `12:15–14:00` to
  `12:15–14:15` before release.
- Web tests pass with 108 tests, including live resize, drag ghost, timed
  Milestone completion, optimistic completion, view switching, AI review, and
  Progress Agent selection. Web production build and TypeScript checking pass.
- BFF passes 52 tests; Worker passes 36 tests and Ruff; focused and full Go
  suites pass. A PostgreSQL migration integration test exercises
  `000033` up/down/up and verifies completion Proposals retain equivalent
  `*.update + status` meaning during rollback.
- Every code/test/build/contract/API stage of `pnpm check` passed. The command's
  final Caddy child process was blocked by the workspace sandbox with `EPERM`;
  the same repository `pnpm caddy:check` passed outside the sandbox with
  `Valid configuration`.

Integration notes:

- The main worktree's pre-existing Agent/Auth/Artifact work remains untouched.
  Its uncommitted migrations overlap numbers `000033`–`000035`, so this
  branch's `000033` must be renumbered deliberately when the user later asks
  to rebase/merge; do not silently overwrite either migration line.
- The isolated database is an online copy used only for acceptance. During
  initial setup, the additive workbench migration was also applied to the
  shared development database; it added compatible columns/constraints and
  migrated cancelled rows without deleting records. It remains applied
  because rolling it back would be destructive and was not authorized.

## 2026-08-10 official Hermes API-alignment hardening

Five independently implemented and reviewed issues close the remaining gaps
against the official Hermes `v2026.8.3` Runtime contract:

- Job schedules now preserve Hermes' `{kind, expr, display}` representation
  instead of assuming a scalar cron expression.
- Run SSE no longer forwards `Last-Event-ID` or advertises replay semantics
  that Hermes does not implement; live queue consumption remains supported.
- Profile validation now matches Hermes' canonical 64-character identifier,
  reserved-name, and built-in `default` rules.
- Capability probing validates the exact method and path for all 14 required
  Session and Run endpoints while continuing to probe the Jobs endpoint live.
- Explicit runtime checks now create/read/list a temporary Session, start and
  stop a Run, require the matching live `run.cancelled` SSE event and final
  cancelled status, and delete the temporary Session using a fresh bounded
  cleanup context. Normal message history continues to accept Hermes'
  resumed/descendant Session identifier semantics.

The fixes are split into one logical commit per issue:

- `4f8b4a4 fix(agent): normalize Hermes job schedules`
- `46587a4 fix(agent): correct Hermes event replay semantics`
- `a7e3b0d fix(agent): validate Hermes profile identifiers`
- `7e84933 fix(agent): enforce Hermes capability endpoints`
- `1ae6a10 fix(agent): exercise Hermes runtime connections`

The OpenAPI Agent profile pattern and maximum length changed together with the
generated clients and examples. There are no migration changes; the canonical
catalog remains continuous through `000032_agent_progress_evaluation_source`.
Contract generation/check and API catalog coverage passed, with 368 operations.

### Official-instance acceptance

The Compose mock Hermes service was stopped before any Agent acceptance. The
official Hermes Agent repository tag `v2026.8.3`, commit
`3c27eb6234bf91b8ceee9e9071591b31e9b148cb`, ran from an isolated `HERMES_HOME`
with its locked Anthropic and messaging extras. The DeepSeek credential was
read from the local credential file directly into the process environment and
was never copied into source, command output, or logs.

- Authenticated `/health`, `/health/detailed`, and `/v1/capabilities` passed;
  detailed health reported the configured model, Gateway, API Server, state
  database, disk, and background queues ready. The Core container reached the
  host Hermes API over the isolated Compose network.
- Creating a manual mmdash Agent instance ran the new runtime exercise and
  returned `runtime_check.status=passed`. A second explicit runtime check also
  passed runtime, authentication, capabilities, Sessions, messages, SSE, Runs,
  and Jobs. Hermes reported zero remaining `mmdash_runtime_check` Sessions
  after both cleanup paths.
- A temporary Gateway-attestation API Token enabled the real manual reverse
  connection. MCP `initialize` and exact `tools/list` returned the six reviewed
  tools, the pending Agent Token activated, Project access passed, and the
  instance became active.
- Through the active mmdash Core boundary, a real Session and Run invoked the
  configured DeepSeek Anthropic-compatible endpoint. The Run completed after
  one provider API call, SSE contained `message.delta`, `tool.progress`, and
  `run.completed`, message history contained user and assistant rows, and the
  assistant returned `MMDASH_REAL_HERMES_OK`.
- Complete `pnpm check` passed after one transient pair of pre-existing Repo
  worktree timeout failures was rerun successfully. Docker smoke passed on the
  isolated ports with only the native CLI credential-storage subtest skipped
  because the workstation has no unlocked Secret Service. All six Compose
  services and real Hermes were healthy; recent Compose logs contained no
  panic/fatal/error and had zero exact matches for the Hermes, DeepSeek,
  Gateway-attestation, Agent, or Core session credentials.
- The temporary Agent instance and Project were disabled/trashed, the Agent
  and Gateway-attestation credentials were revoked, official Hermes and the
  task-owned mihomo process stopped cleanly, and the credential-bearing
  isolated Hermes state was deleted. Compose stopped with `down` and no `-v`;
  the PostgreSQL and MinIO volumes remain preserved.

### Remaining upstream/runtime warnings

- The Hermes Python 3.11 runtime links SQLite 3.50.4, so Hermes warns about the
  upstream WAL-reset corruption issue and recommends SQLite 3.51.3+ (or the
  listed backports). Its response store safely selected DELETE journaling, but
  the other isolated acceptance databases retained WAL. This is an upstream
  runtime/toolchain warning rather than an mmdash contract failure.
- Binding the Hermes API Server to the Compose gateway requires a non-loopback
  listener while its terminal backend remains local, so Hermes emits its
  expected unsandboxed-terminal exposure warning. The listener was used only
  for this local isolated acceptance and is not a production deployment model.
- Hermes reports unavailable optional BFL, browser, image-generation, preview,
  and web-search tools because those unrelated provider/runtime extras are not
  configured. The no-tools DeepSeek Run and every mmdash-owned capability
  completed successfully.
- No messaging-platform allowlists were configured because this acceptance
  enabled only the authenticated API Server. Hermes therefore emits its normal
  warning that unknown messaging senders would be denied.

## 2026-08-10 real Hermes integration

Hermes Agent 0.20.0 from `NousResearch/hermes-agent` tag `v2026.8.3` at commit
`3c27eb6234bf91b8ceee9e9071591b31e9b148cb` was started locally with an
external model-provider credential supplied only to the Hermes process. The
real Runtime and Dashboard were connected to the production mmdash Core,
Worker, MCP Gateway, PostgreSQL, and Data Hub paths; the credential and all
one-time mmdash Tokens were excluded from source, logs, and this handoff.

The live integration exposed and fixed four contract defects:

- Hermes capability `features` contains transport metadata strings beside
  boolean flags. The Adapter now decodes the object heterogeneously and reads
  only owned boolean capability keys.
- Hermes message row IDs are JSON integers in the pinned release. The Adapter
  now normalizes both integer and opaque-string IDs without exposing private
  reasoning or Tool results.
- Hermes Dashboard uses the standard `Mcp-Session-Id` Streamable HTTP header.
  MCP Gateway now accepts and emits both that header and the existing
  `X-Mmdash-Session-Id` compatibility alias, and rejects mismatched dual
  headers with `MCP_SESSION_HEADER_CONFLICT`.
- Stage 6 had overloaded the parent-Run `agent_runs.source_run_id` foreign key
  with a Progress evaluation ID. Migration
  `000032_agent_progress_evaluation_source` adds the dedicated
  `source_evaluation_id` foreign key; Agent persistence, OpenAPI, events,
  generated clients, and documentation now keep both provenance types
  distinct.

### Real-runtime evidence

- Adapter probing passed health, authentication, capability, Session, Run,
  SSE, stop, approval, Jobs, and Tool-progress checks.
- A manual Agent instance completed MCP initialization, exact `tools/list`,
  Token activation, and authorized `data.list`. A real Session and Run returned
  `MMDASH_HERMES_REAL_OK`; normalized SSE included `message.delta`,
  `tool.progress`, and `run.completed`, and message history returned numeric
  IDs successfully.
- An automatic Agent instance used the authenticated Dashboard management API
  to install the mmdash MCP entry, record reverse-call evidence, activate the
  Token, restart/reload the Gateway, and rotate the Token without returning
  plaintext from ordinary APIs. The old Token was revoked only after the new
  credential verified.
- A Stage 6 `core_agent` evaluation completed through the real Worker/Hermes
  path after migration `000032`, detected implementation stage 7, persisted
  its Agent Session/Run provenance, updated tracker and risk state, and created
  four reviewable Proposals. The original failed attempt remains as diagnostic
  history for the foreign-key regression.
- Focused Agent, Hermes Adapter, Progress, MCP Gateway, contract, and API checks
  passed. Real PostgreSQL migration coverage includes the fresh catalog,
  upgrade, idempotent rerun, and `000029-000032` down/up path. Complete
  `pnpm check` passed: TypeScript, Go, Python and CLI lint/tests/builds;
  contract compatibility; 368-operation API coverage; and Caddyfile-only
  validation.
- Docker Compose stack smoke passed on the isolated
  `13000/13001/18080/19002` ports with a one-shot local Worker against the
  containerized Core. The native CLI login subtest was explicitly skipped
  because this headless workstation has no unlocked Secret Service; CLI
  build/unit coverage passed in `pnpm check`. Core, Web, BFF, MCP Gateway,
  PostgreSQL, MinIO, Hermes Runtime, and Dashboard were healthy, and recent
  application logs had zero error/credential matches. The provider Token had
  zero exact matches in Hermes logs.
- All acceptance Agent, Worker, and Gateway-attestation Tokens were revoked;
  the pending manual rotation was cancelled; four temporary Dashboard MCP
  entries were deleted. Compose stopped with `down` and no `-v`; Hermes,
  Dashboard, forwarding, and mihomo processes stopped; PostgreSQL and MinIO
  volumes were preserved. The credential-bearing Hermes state under `/tmp`
  was removed while the pinned upstream source checkout was retained.

### Environment limits

- The primary DeepSeek-backed Runs and Progress evaluation passed. Optional
  Hermes auxiliary Nous/OpenRouter clients were not configured and emitted
  non-fatal availability warnings; they are not part of the mmdash Adapter
  contract exercised here.
- Automatic management was validated over a direct server-reachable Dashboard
  connection. Cloudflare Access remains covered by connector and management
  contract tests rather than this localhost run.

## 2026-08-10 migration numbering and integrated acceptance

The merged Stage 7 branch introduced numeric collisions at `000022-000024`.
Model now owns canonical migrations `000029_model_stage7` and
`000030_model_notion_oauth`; the Notification routing correction is
`000031_notification_routing_model`. The migration runner rejects malformed,
duplicate, gapped, or unpaired catalogs before applying SQL.

An immutable compatibility ledger preserves databases that already recorded
the former Model/Notification names, the pre-merge
`000023_notification_routing_model`, or the pre-integration
`000023_agent_sessions` development name. Under the existing PostgreSQL
advisory lock, the runner records the canonical name transactionally and does
not execute that migration again. Legacy rows remain as upgrade evidence.

Real PostgreSQL coverage passed for a fresh canonical database, a complete
legacy database, mixed/partial state, both historical Notification names,
canonical/legacy coexistence, repeated execution, and the `000029-000031`
down/up round trip. The preserved development database upgraded from
`000023_agent_sessions` without replaying Agent SQL; its existing user,
Project, and Agent counts were unchanged during migration. It now records all
31 canonical migrations plus that retained legacy row at the time of the
numbering integration. The current catalog adds migration `000032` as described
above.

Integrated acceptance also exposed old `progress.reminder.due` events whose
test Projects had already been removed from the preserved database. Notification
event persistence now takes a Project key-share lock and treats an event for an
already deleted Project as an idempotent no-op. A real replay of one formerly
failed event completed successfully after the fix. Historical failed-delivery
records were retained rather than deleted.

### Integration verification

- Stage 7 focused Go, Worker, Web, BFF, CLI, Data Hub, Artifact, Project,
  Notification, and migration tests passed with real PostgreSQL where
  applicable. Contract generation/check and the API catalog covering 368
  operations passed without contract changes.
- Complete `pnpm check` passed after the integration fixes: TypeScript, Go,
  Python, and CLI lint/tests/builds; contract compatibility; API coverage; and
  Caddyfile-only validation.
- Docker Compose images built from this worktree. Because unrelated native
  services already occupied `3000/3001/8080`, acceptance used loopback-only
  `13000/13001/13002/18080` host ports while keeping normal container ports and
  the preserved PostgreSQL/MinIO volumes.
- Repository smoke passed with its native CLI login subtest skipped because
  this headless workstation has no unlocked Secret Service. CLI build/unit
  coverage passed separately. A one-off Docker Worker completed a real
  `system.test` Job and its temporary Token was revoked.
- The Model page returned HTTP 200, the BFF reported the expected OAuth
  unavailable/disconnected state without local Notion credentials, and the
  live MCP Gateway completed `data.list(type=model_source)` through Core/Data
  Hub. The real Notion OAuth, recursive discovery, Snapshot, unchanged/changed
  Hash, media, Diff, refresh rotation, and disconnect evidence from 2026-08-09
  remains authoritative because this integration changed no Model runtime or
  provider contract.
- Core, Web BFF, Web, MCP Gateway, PostgreSQL, and MinIO were healthy. Current
  application logs contained no panic/fatal/error or credential match; the
  only expected persistent Worker-service message was the missing boot Token,
  while the separately tokenized one-off Docker Worker succeeded. Compose was
  stopped with `down`, never `down -v`.

## Previous Stage 6 automatic Progress tracking handoff

- Updated: 2026-08-10
- Branch: `codex/stage-6-auto-progress`
- Base: `origin/main@52e398f`
- Migration: `000028_progress_auto_tracking`
- Delivery state: merged through Ready PR #33

## 2026-08-10 Stage 6 automatic Progress tracking

Stage 6 is implemented as a complete vertical slice on top of the already
merged Stage 4 Home/Progress, Stage 5 Agent, Stage 7 Model, and Notification
work. Core remains the sole Progress writer and PostgreSQL remains the Job
Queue; no Redis or parallel persistence path was introduced.

Delivered behavior includes event, Cron, manual, and retry scheduling;
debounce and source-event replay deduplication; recoverable assembly/Cron
leases using `FOR UPDATE SKIP LOCKED`; canonical evaluation inputs and history;
Worker evaluation in production `core_agent` and deterministic `mock` modes;
automatic Task convergence; human-protected Task fields; milestone Proposals;
human accept/reject and stage override controls; risks and failure recovery;
RBAC, transactional Audit/Outbox, bounded metrics, and Project-scoped
settings. Agent automation uses the existing Session, Run, and Jobs contracts,
and Progress-generated Agent Runs and domain events do not recursively trigger
new evaluations.

The Web and BFF expose effective/detected stage, summary, recalculation,
evaluation history/detail/provenance, risks, retry, settings, Agent/Cron state,
Proposal review, and stage override controls. Home consumes the effective
Progress tracker state. Data Hub adds authoritative `progress_evaluation` and
`progress_risk` projections. MCP Gateway adds exact-scope `progress.get` and
`progress.recalculate` tools, expanding the reviewed Agent tool set to six.

Migration `000028_progress_auto_tracking` is additive and follows the current
migration set without rewriting an existing migration. It adds evaluation
requests/triggers/history, risks, tracker state and overrides, automatic
Task/Proposal provenance and convergence keys, tracking/Cron settings, and the
supporting lease/deduplication indexes.

### Stage 6 verification

- Fresh migrations through `000028` and an explicit `000028` down/up round
  trip passed. Real PostgreSQL integration coverage includes debounce, replay,
  lease recovery, input deduplication, automatic mutation convergence, manual
  overrides, Proposal/risk/history/failure paths, Audit, Outbox, and stage
  override restoration.
- Core, Data Hub, Agent, Project, config, metrics, Worker, Web, BFF, and MCP
  Gateway focused tests and builds passed. Contract generation/check and the
  API catalog covering 368 operations passed.
- Docker acceptance used the explicit deterministic mock evaluator because no
  real Hermes instance was available. It exercised manual and event
  evaluations, an automatic Task, pending/rejected/accepted Proposals, a
  Proposal-created Milestone, blocked-task risk detection, stage override and
  clearing, Home aggregation, Data Hub readers, and both Progress MCP tools.
- The standard smoke path passed with only its native CLI subtest skipped: this
  headless workstation has no unlocked Linux Secret Service, so the CLI cannot
  persist its device-login session. The same live MCP Gateway was exercised
  directly with the current Streamable HTTP protocol. All containers were
  healthy, log scans found no error/fatal/panic or credential pattern, the
  Worker token was revoked, and Compose was stopped with `down` without
  deleting volumes.
- The Worker image now normalizes copied source permissions before switching to
  its non-root runtime user, avoiding host umask/directory-mode dependent
  import failures.

### Stage 6 operational notes

- `MMDASH_PROGRESS_EVALUATOR_MODE=core_agent` is the production default;
  `mock` is explicit deterministic development/acceptance behavior only.
- Automatic tracking in `core_agent` mode requires an active Project Agent
  instance. Cron synchronization uses the existing Hermes Jobs API and stores
  only the remote Job ID/status in Progress settings.
- Hermes-facing behavior is contract/mock tested; a real Hermes environment
  remains the release-environment integration check.
- PostgreSQL and MinIO acceptance volumes were preserved.

## Previous Stage 7 and Notification handoff

- Updated: 2026-08-09
- Branch: `main`
- Base: `origin/main@f10733e`
- Integration-token baseline: `b7150e3 feat(model): implement stage 7 model workflow`
- Notification correction: `af5e596 fix(notification): correct inbox routing model`
- Delivery state: Stage 7 complete and Notification routing correction merged

## 2026-08-09 Notification routing correction

Commit `af5e596` corrects the Stage 3.17/Stage 4 Notification
implementation against the v0.1 baseline without changing the source module
contract. The Type Registry now exclusively owns Inbox policy and Project
Notification Rules exclusively own optional external delivery. The obsolete
`inbox_enabled` API/database field is removed by migration
`000031_notification_routing_model`; invitation remains required Inbox-only,
while Progress reminders remain default-on in Inbox and optionally external.

The Web now has one global Inbox icon/unread badge on `/projects` and project
workspace chrome, a consistent global page shell, unread/all/processed and
archive views, project/type/time filters, pagination, scoped batch read, safe
rendered copy, and a detail route. Notification settings separate read-only
Inbox policy from owner/maintainer-only channel/rule/Delivery management;
explicit retry requires a reason. Focused Go, BFF, Web, contract, API, lint,
test, and build checks passed on the source branch. The merge verification is
recorded below together with Stage 7.

## Status

Stage 7 is complete against the v0.1 implementation-order v0.4,
technical-architecture v0.4, and product-design v0.1 baselines. Model is a
vertical Core-owned module with Web, BFF, PostgreSQL, Worker, Artifact, Data
Hub, MCP, and native Go CLI integration. Each Project has one Notion Source;
each active question binds one recursively discovered descendant page and owns
an independent immutable Snapshot chain.

The Integration Token implementation was frozen in `b7150e3`. The current
delivery replaces new browser token entry with a public Notion OAuth flow.
Legacy `integration_token` settings remain read-only upgrade compatibility and
are removed atomically after a successful OAuth callback.

## Delivered behavior

- Single Project-scoped Notion Source with recursive child-page discovery,
  explicit Q1/Q2-style bindings, one full-width question card per row, and
  independent question histories.
- Question detail layout with timeline, Notion-aligned document, document
  information, and a viewport-bounded three-level outline card with vertical
  scrolling.
- Character-level Diff with contiguous operations, faded pink strikethrough
  deletions, blue additions, normal unchanged text, and no line numbers.
- Worker normalization for rich text, equations, tables, bookmarks, images,
  files, and nested blocks. Changed Notion media is imported through Artifact
  as `model_file` / **模型文件** before Snapshot commit; unchanged hashes skip
  media transfer and Snapshot creation.
- Editable multiple tags and version notes, with `初稿`, `修订中`, and `最终版`
  as optional built-ins and no automatic tag lifecycle.
- Model index and question refresh actions, synchronization progress, default
  five-minute automatic schedule, configurable interval, and Settings
  countdown.
- Human-team `project.model.sync` permission. Model-index sync and automatic
  sync both discover first and then fan out over the freshly persisted
  question set; question sync affects only that question. Every manual click
  resets the shared schedule, and an active task is reused without returning a
  conflict.
- Data Hub `model_source`, `model_question`, and `model_snapshot` projections,
  MCP `data.list` / `data.read` access, and human CLI `model list`, `model show`,
  and `model sync [question_id]` commands.

## OAuth and credentials

Migration `000030_model_notion_oauth` stores only hashed, expiring, one-use
authorization state. Core validates state, caller, Project permission, selected
root access, and callback ownership before Settings encrypts provider tokens.
Notion API, token exchange, refresh, and revocation endpoints are fixed in the
adapter and are not caller-controlled. The Worker receives only a Job-bound
export and never an OAuth credential.

Local configuration uses `NOTION_OAUTH_CLIENT_ID`,
`NOTION_OAUTH_CLIENT_SECRET`, and an exact localhost
`NOTION_OAUTH_REDIRECT_URI`. Disconnect revokes credentials, disables future
scheduling, and retains immutable Model and Artifact history.

## Synchronization invariants

The scheduler uses PostgreSQL `FOR UPDATE SKIP LOCKED`. Full synchronization
queues one discovery Job; successful discovery replaces the descendant set and
atomically creates question Jobs. A missing/disconnected Notion binding cannot
schedule work. Manual task creation, active-task reuse, and countdown reset are
one transaction. A PostgreSQL integration regression covers the timestamp
parameter, active-task reuse, one-task invariant, and second-click countdown.

## Verification

Passed on 2026-08-09:

- Real localhost Notion OAuth authorization and access to the selected root.
- Recursive discovery of 6 child pages and three bound questions.
- Model-index full sync: HTTP 202, `queued` → `running` → `succeeded`, followed
  by three fresh question Jobs.
- Question sync: UI `queued` state followed by `unchanged` when the semantic
  hash did not change. Manual clicks reset `next_sync_at` to click time plus
  five minutes.
- Changed content created a new Q1 Snapshot; unchanged Q1/Q2/Q3 runs created no
  duplicate Snapshot. Existing image/file Artifacts rendered through the
  Model page.
- Real PostgreSQL regression
  `TestPostgresManualSyncReusesActiveTaskAndResetsCountdown`.
- Complete `pnpm check`: TypeScript, Go, Python and CLI lint/tests/builds;
  contracts and compatibility; API catalog covering 288 operations; and
  Caddyfile-only validation reporting `Valid configuration`. No Caddy service
  was started.
- Docker Compose `--profile worker up -d --build`, repository smoke, and smoke
  with `MMDASH_SMOKE_WORKER_MODE=docker`. Web, BFF, Core, MCP Gateway,
  PostgreSQL, and MinIO were healthy; the Docker Worker completed a real Job.
- Docker Model live run over the OAuth-bound project: source discovery
  `succeeded`, and Q1/Q2/Q3 fan-out completed `unchanged` without Model error
  codes. The temporary Worker token was revoked and Compose was stopped with
  `down`, never `down -v`.

The merged Notification correction also removes the ambiguous Inbox
unread-count join that produced background HTTP 500 responses during the
Stage 7 browser acceptance.

## Operational notes

- The authoritative local launcher remains `.\.localscripts\dev.ps1`.
- Local bootstrap defaults remain `admin@mmdash.local` and
  `mmdash-local-admin` unless overridden by environment variables.
- OAuth provider secrets and Worker tokens must remain in environment/Secret
  injection. Do not put them in source control, CLI arguments, test fixtures,
  or logs.
- PostgreSQL and MinIO volumes were preserved.
- The next product stage is Stage 8 Experiment, Box, and Sandbox.
