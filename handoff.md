# mmdash v0.1 Stage 7 integration handoff

- Updated: 2026-08-11
- Branch: `main`
- Base: `origin/main@9282b4e`
- Canonical migrations: continuous `000001` through
  `000037_progress_human_workbench`
- Delivery state: Agent single-token authentication, private-Core boundary,
  recoverable instance removal, full-screen workbench, and Agent Artifact
  upload are integrated with the Progress human scheduling workbench and review
  policy; the merged repository gate and Docker Compose smoke acceptance pass;
  Project and per-Run instructions explicitly advertise Markdown/KaTeX support

## 2026-08-11 Progress human workbench integration

- The Progress page now exposes Calendar and TODO views. Calendar supports day
  and cycling two/three/four-day ranges, fixed two-axis scrolling, current-time
  positioning, a Milestone strip, optional timed Milestone duplication,
  15-minute creation/move/resize snapping, overlap lanes, drag ghosts, live
  resize geometry, and one detail drawer. TODO remains one waterfall; its
  day-versus-period setting changes grouping depth without rewriting stored
  time.
- Human completion is independent from the automatic `todo`, `in_progress`,
  and `blocked` assessment. Human completion renders a filled/faded card;
  automatic completion remains an amber `task.complete` or
  `milestone.complete` Proposal until explicitly accepted or rejected. Agent
  creation and scheduling changes are likewise reviewable, including atomic
  approve-all/reject-all, while work-state assessments apply directly.
- Automatic evaluation uses a dedicated `session_type=progress` Agent Session.
  The isolated-branch acceptance completed a real Hermes evaluation, left its
  creation/scheduling suggestions pending until browser batch rejection, and
  accepted a controlled completion Proposal through the card.
- The branch's original `000033_progress_human_workbench` migration was
  deliberately renumbered to `000037_progress_human_workbench` during merge;
  Agent/Auth/Artifact already authoritatively own migrations `000033` through
  `000036`. Its column, constraint, and index setup is re-entrant so a shared
  development database that previously received the old unrecorded migration
  body can still adopt the canonical `000037` record safely. The integration
  test exercises up/down/up behavior.
- Before integration, the Progress branch passed its Web, BFF, Worker, Go,
  build, contract, API, and Caddy checks. Browser acceptance covered live
  resize, drag preview, timed Milestone completion/reopen, optimistic rollback,
  AI review, and Progress Agent selection. The merged repository is revalidated
  separately below before delivery.
- After integration, the repository gate passed with 123 Web tests, 55 Web BFF
  tests, 37 MCP Gateway tests, all Go/Core/CLI tests, 36 Worker tests, all three
  language builds, contract compatibility, and a 373-operation API catalog.
  Caddy validation returned `Valid configuration` outside the process sandbox.
  Docker Compose then applied canonical migration `000037` to the existing
  shared database, brought every long-running service healthy, and passed the
  full repository smoke including Worker, native CLI device login, local/remote
  MCP, Audit, events, and Data Hub. Recent application logs contained no
  panic/fatal/error entries; acceptance containers/network were removed with
  `down` and volumes were preserved.

## 2026-08-11 Agent workbench and Agent Artifact integration

- Migration `000034_agent_instance_removal` adds `removed_at`. Deleting a
  disabled or active instance now revokes its Grant/Tokens and removes it from
  ordinary reads without destroying Session, Run, Audit, or Artifact history.
- Human Session creation is `main`-only. Persisted `progress` and `experiment`
  Sessions remain internal to automatic Progress evaluation and Experiment
  result analysis and are filtered from the human workbench.
- The Agent page is a viewport-filling ChatGPT-style workspace with collapsible
  Session and context rails, on-demand new-Session naming, Session context
  menus, an internally scrolling transcript, Enter-to-send/Shift+Enter newline,
  in-composer stop, SSE reattachment, inline safe reasoning/Tool status, and
  stale-history-safe terminal reconciliation. Below `1280px` the context drawer
  starts collapsed so it cannot cover the composer. Create and fork now clear
  the prior Session's transient Run/stream state through the same transition as
  an explicit Session selection; this closes the live stale-Run/404 defect found
  during browser acceptance.
- A generic transport-level SSE `error` no longer marks the Run failed. The Web
  client checks the authoritative Run, resumes from the last event ID with
  bounded backoff while it remains active, and clears the reconnect notice when
  output resumes. Agent-uploaded Artifact projections stay out of the transcript
  until the Run settles and are then ordered at the Run terminal timestamp
  instead of appearing inside its thinking/Tool chain. Persisted Hermes Tool
  Calls with omitted/stale creation-time state now render as completed history,
  while only the active Run may display `queued`/`running`.
- Session projections expose `last_run_id`; returning to a Session therefore
  queries and reattaches its still-running Run instead of losing the stop state
  and new output. The composer also offers a persisted per-Agent reasoning
  selector (`auto` plus Hermes' eight explicit levels), validated end to end and
  forwarded as request-scoped `model_options.reasoning_effort`.
- The selected Agent and Session now survive refresh through URL state. The
  workspace navigation and per-Agent Session rail persist their collapsed state,
  while two-second background projection polling keeps another tab on the same
  Session current without competing with the active Run SSE stream.
- Markdown rendering now safely supports fenced/inline Markdown plus `$...$`,
  `$$...$$`, `\(...\)`, and `\[...\]` KaTeX without invalid paragraph
  hydration markup.
- Migration `000035_agent_artifact` adds `kind/source=agent` and exact
  `agent_instance_id` upload ownership. New exact MCP Tool `artifact.upload`
  initializes through a private Core Agent endpoint, returns only direct
  object-storage multipart grants, and confirms size/SHA-256/ETags; no complete
  part, file, or base64 crosses MCP Gateway or Core application memory.
- Migration `000036_agent_chat_artifacts` binds uploaded immutable Artifact
  Versions to the originating Agent Session/Run. The composer uploads user files
  as `kind=attachment`; current attachments are explicit first-class Run inputs,
  and a bounded same-Session attachment ledger lets later questions retrieve
  earlier files through `artifact.read` without requiring the user to repeat an
  instruction to open them. Agent-delivered files render as cards and images as
  inline previews in persisted history.
- The real `iamswlx486@gmail.com` test account passed browser acceptance without
  recording its password or one-time Tokens: the disabled instance returned
  `204` from the UI removal flow and disappeared; its active instance rotated
  to the eight-Tool grant; Hermes Dashboard reported all eight exact Tools.
  Live Runs created text and PNG files, called
  `mcp__mmdash_project__artifact_upload`, completed direct PUT plus ETag/SHA-256
  verification, and both objects appeared `available` in the Artifact library
  with `kind=agent` and `source=agent`. The same browser run confirmed
  no-refresh replies, inline Tool/reasoning state, Markdown/KaTeX, Enter versus
  Shift+Enter, a successful `running -> stopping -> stopped` action, fixed page
  height, and an internally scrolling transcript (`overscroll-behavior: contain`).
  Follow-up acceptance proved proactive current-file inspection, retrieval of a
  file attached to an earlier Run in the same Session, cross-tab convergence,
  duplicate-final suppression, refresh-stable Session selection and rail states,
  and a generated 86.9 KiB PNG appearing immediately as an inline image card.
  Selecting that card opens the Artifact detail drawer in place without leaving
  the Agent page; it no longer initiates a download directly from the chat.
- The final post-acceptance repository gate passed: TypeScript lint and tests,
  122 Web tests, 54 Web BFF tests, 37 MCP Gateway tests, all Go/Core/CLI tests,
  36 Worker tests, TypeScript/Go/Python builds, contract compatibility, and the
  371-operation API catalog. The sandbox denied spawning the local `caddy`
  binary, so the identical Caddyfile check was rerun outside that process
  sandbox and returned `Valid configuration`.
- The final Docker Compose images built and all long-running services reached
  healthy state. The repository smoke passed end to end, including browser API,
  Core, Worker, native CLI device login, stdio/remote MCP, Audit, events, and
  Data Hub. Core was bound only to host loopback through a temporary acceptance
  override because the production Compose boundary intentionally leaves it
  unpublished. Recent Core/Web BFF/MCP Gateway/Web logs contained no
  panic/fatal/error entries; `docker compose down` removed the acceptance
  containers and network without deleting PostgreSQL or MinIO volumes.

## 2026-08-11 Agent single-token authentication and private Core

The former Gateway-attestation design recorded below is historical and is now
superseded. Product Agent Tokens and user Tokens use the same first-class Core
authentication model; their difference is the Agent Token's narrower binding
to one Agent instance, Project, and exact Tool grant.

- MCP Gateway forwards the original inbound Agent Token to Core. The
  `MCP_CORE_ACCESS_TOKEN`, `AUTH_AGENT_VERIFICATION_TOKEN_ID`, relay header,
  Core guard middleware, and secondary-credential client fields were removed.
- Each pending Agent Token receives an independent one-time challenge. Core
  stores only its SHA-256 Hash; the one-time MCP endpoint carries the plaintext
  challenge. After an initialized exact `tools/list`, Gateway calls Core with
  the same pending Agent Token and challenge. Core verifies the exact
  Token/Agent/Project identity, atomically consumes the challenge, and stores
  first-write evidence before human or automatic activation.
- Migration `000033_agent_token_challenge` revokes unrecoverable legacy
  pending credentials, marks their rotations for safe reissue, removes
  `verified_by_token_id`, and adds challenge/evidence constraints.
- Production Compose no longer publishes Core on a host port, and Caddy never
  proxies to Core. Public `/v1` traffic terminates at Web BFF, which forwards
  the original user Session/API Token after identity introspection and rejects
  Agent/service credentials. Explicit public auth, signed transfer, and signed
  webhook operations remain available. The acceptance override alone exposes
  Core on host loopback for test orchestration.
- Manual UI copy now treats both the Agent Token and challenged MCP endpoint as
  one-time secret material. Automatic Hermes management receives the same
  challenged endpoint, so the pinned Dashboard `/test` flow deterministically
  creates verification evidence through its negotiated `tools/list`.
- Focused Core/Auth/Agent, MCP Gateway, Web BFF, Web, Core Client, contract, API
  catalog, and smoke-script syntax checks pass. The final `pnpm check` also
  passes completely: TypeScript, Go, Python, CLI, builds, contract
  compatibility, the 368-operation API catalog, and Caddy validation.
- Acceptance images for Core/migrations, Web, Web BFF, and MCP Gateway built
  successfully. This workstation's Docker Compose v2.10.2 does not implement
  the acceptance file's `!override` port reset, so startup retained the base
  `5432` mapping and stopped at an existing host-port collision before
  migrations or smoke ran. The task-owned acceptance containers/network were
  removed with `down` and without `-v`; its volumes remain preserved. No
  existing development stack was stopped.

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
