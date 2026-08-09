# mmdash v0.1 Stage 7 Model handoff

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
`000024_notification_routing_model`; invitation remains required Inbox-only,
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

Migration `000023_model_notion_oauth` stores only hashed, expiring, one-use
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
