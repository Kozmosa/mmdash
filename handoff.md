# mmdash v0.1 Stage 8 Box / Experiment environment-preparation handoff

- Updated: 2026-08-20
- Branch: `main`
- Base: `2cb81e6` (`feat(repo): add repository previews`)
- Delivery state: Stage 8 now includes deterministic Local Docker environment
  preparation, immutable environment evidence, cache reuse and managed-image
  expiry. A full Coding Agent MCP experiment passed on the account-bound local
  Box `iznnyaku` with non-empty NumPy and Matplotlib dependencies.
- Follow-up fixes (2026-08-16): the Experiment list is now card-only and links
  to an independent detail route for Terminal/result-tree views; Box Management
  now lists platform-specific installers from the hidden mmdash system Artifact
  Project and includes browser/device-auth installation guidance. Repo source
  archives skip Git symlinks instead of failing CI's frozen-tree test.

## Proposed follow-up: `local-process` bare-metal Runtime

This section records an approved design direction only; it is not implemented.
Some Box hosts cannot use Docker because nested virtualization is unavailable.
Add a `local-process` Sandbox Runtime Adapter that executes a task as a direct
host process. Display it as “裸机进程”, classify it as `trusted-host`, disable
it by default, and require both Box-owner opt-in and explicit Experiment
selection. `auto` must continue to select only E2B and Local Docker and must
never silently fall back to `local-process`.

- Repo/Core freezes the Commit and provides a short-lived source transfer.
  Gateway verifies and expands it into a disposable Box-managed workspace
  before environment preparation. The Runtime must not clone inside the task,
  receive Git credentials, or execute against the user's canonical checkout.
- Add a small per-task `mmdash-task-runner` supervisor. It directly starts the
  fixed entrypoint without a shell, owns the process group (Linux) or Job Object
  (Windows), enforces timeout/cancellation across the complete process tree,
  durably writes PID/status/exit state and stdout/stderr, and lets Gateway
  reconnect after a Gateway restart. A host reboot is terminal
  `HOST_RESTARTED`; it must not replay the same execution automatically.
- Gateway spools ordered stdout/stderr locally and uploads by sequence through
  the existing Core/BFF SSE path. A network disconnect does not stop the task;
  the existing log budget and `logs_truncated` behavior remain authoritative.
- Use a dedicated low-privilege OS account, a minimal environment, isolated
  `HOME`/temporary/cache directories, a disposable workspace and separate
  output directory. Never inherit Gateway, Git, SSH, cloud-provider or Box
  credentials. This is trusted-code execution, not container-equivalent
  isolation.
- Runtime probing must advertise actual enforcement. Timeout and process-tree
  termination are mandatory. CPU/memory/PID limits may use Linux cgroup v2 or
  Windows Job Objects. Output size is enforced at collection. If the host
  cannot enforce a requested network or resource policy, scheduling must reject
  the task instead of claiming that an advisory limit is enforced.

### Python environment discovery and caching

The first implementation should support common Python environment families,
not only root `requirements.lock`:

- `requirements.lock` and `requirements.txt` through a controlled `pip`
  builder; use `--require-hashes` when the file is fully hash-pinned;
- `pyproject.toml` plus `uv.lock` through `uv sync --frozen
  --no-install-project` so the cached environment contains dependencies but
  never copies Project source;
- `pyproject.toml` plus `poetry.lock` through an equally frozen, no-project
  installation path;
- `Pipfile` plus `Pipfile.lock` through locked synchronization;
- Conda-style `environment.yml`/`environment.yaml` only in a later adapter
  after an exact solver/toolchain contract is defined; until then return the
  stable `ENVIRONMENT_MANIFEST_UNSUPPORTED` error.

Recognize manifests only from a documented allowlist and validate every path.
If more than one environment family is present, do not guess precedence:
require an explicit environment selection in the frozen RunSpec or fail with
`ENVIRONMENT_MANIFEST_AMBIGUOUS`. A plain `requirements.txt` is accepted for
compatibility but is only best-effort reproducible unless every dependency is
pinned and hashed; persist the resolved package set in result evidence.

Build a content-addressed virtual environment under
`<box-root>/environments/local-process/python/<environment-key>`. The key must
include OS/architecture, interpreter path and version, Runtime/builder version,
installer version and all selected manifest paths/content hashes. Build in a
temporary directory and atomically publish only a successful environment.
Persist the selected manifests, their hashes, the resolved dependency set,
environment identity and cache-hit state in resource usage and the immutable
result Manifest.

Update `last_used_at` under a cache-entry lock. An environment built by Box is
eligible for ordinary, non-forced removal only after 96 hours without use and
when its active reference count is zero. Also apply a configured total cache
budget with LRU eviction; failed or partial builds are never reusable.

Implementation requires a vertical contract change: add `local-process` to
OpenAPI/JSON Schema and append-only database constraints, add Box configuration
and capability/enforcement metadata, extend explicit scheduler matching,
generalize environment evidence away from Docker-only terminology, add the Web
warning/selection flow, and cover source staging, every supported manifest
family, ambiguity, cache/GC, process-tree cancellation, Gateway restart,
offline log replay and unsupported-limit rejection.

## Delivered Stage 8 vertical slice

- Local Docker inspects the frozen source transfer for supported environment
  manifests before execution. The first complete slice accepts a hash-pinned
  root `requirements.lock`, derives a content-addressed environment key from
  the lock, base-image identity, Runtime/platform and builder inputs, and
  builds an environment-only image without copying Project source into it.
- Cache hits reuse the prior image; failed builds are not cached. Images
  carrying the exact mmdash management labels become eligible for ordinary,
  non-forced deletion after 96 hours without use. Environment key, image IDs,
  manifest paths/hashes, builder version and cache-hit state are persisted in
  resource usage and the immutable result Manifest.
- The Box continues to clone no Git repository inside a container: Repo/Core
  freezes and transfers source first, the host-side Gateway prepares the
  environment, and only then is source mounted read-only for execution.

- Account-owned Box device authorization and dedicated Box Tokens; many-to-many
  Project assignment, personal Box management, Project Box/Experiment settings,
  member-departure force-unassignment, draining and force revoke.
- Outbound-only Gateway long polling, execution epochs, durable restart-safe
  callback/log/result spool, offline resume, ordered log acknowledgement,
  `logs_truncated`, 72-hour offline timeout, immutable Execution Bundle upload,
  Local Docker and hosted/self-hosted E2B adapters with lifecycle probes.
- Frozen `box / box-re / self` Experiment types, separate execution/connectivity
  state, structured failures, immutable retry chains, old-ID guidance, Project
  timezone result paths, MCP/CLI create/run/status/bind/result flows, and exact
  Coding Agent self-run instructions.
- Artifact-owned permanent raw Bundles and large files, Worker-safe extraction,
  Repo-owned serialized result commits and remote verification, Artifact pointer
  manifests, and compensating revert for pushed-but-unbound results invalidated
  by force revoke/unassignment.
- Experiment cards/progress, read-only persisted Terminal history and SSE stream,
  result tree, virtual Artifact files, comparison, Box management and split
  settings UI; Data Hub projections, event schemas, Audit records, generated
  clients and API catalogs are aligned.
- Durable Project purge aborts multipart uploads, deletes mmdash-owned Artifact
  bytes and internal Repo storage, cascades Project-scoped database state, keeps
  account Boxes/Tokens, and never changes external Git repositories or branches.
- The first Box operator workflow is now the cross-platform `mbox` command:
  `setup`, account login/status/logout, `config show/set`, service
  init/start/stop/status, and uninstall. Windows uses SCM, Linux uses systemd,
  and the selected Box root stores config, identity, logs, spools and outputs.
  The public homepage/download center is reserved for Box and CLI binaries;
  `dev.mjs` builds both binaries on every startup.

## Verification evidence

- On 2026-08-20, Coding Agent acceptance created Experiment
  `09ecacd9-9b92-4e6b-a4c8-a28f3622847e` in Project
  `9d4c937d-8332-475e-9c88-62dc8d5c8a60`; Box `iznnyaku` executed NumPy
  2.5.2 and Matplotlib 3.11.1, produced the expected plot/stdout/summary,
  reused environment key `435a16fca498...` with `cache_hit=true`, and bound
  result Commit `ede87738a2fd4afbfc1773b8cab5129d97f90bfb`.
- Focused Local Docker/Box, Artifact, Experiment and Worker suites passed.
  Contract generation/check and API catalog checks passed earlier in the same
  change. The repository-wide `pnpm check` was intentionally not rerun after
  the final focused fixes because the user requested a fast experiment and no
  additional broad testing.

- Focused Core, Box, Repo, Artifact, Experiment, Data Hub, Project and CLI Go
  suites passed; Web 134 tests/build, Web BFF 63 tests/build, MCP Gateway 38
  tests/build, contract generation/check and API catalog check passed.
- The repository-wide `pnpm check` was rerun on 2026-08-16 after formatting
  `box/contracts/types.go`. TypeScript/Go/Python lint, all tests, production
  builds, contract checks, and API catalog checks passed. The final Caddyfile
  step could not spawn the locally installed `caddy.exe` in this restricted
  Windows process (`EPERM`); no Caddyfile syntax failure was reported. No
  Docker/Compose command was run per the user's quota instruction.

## Historical pre-implementation preparation snapshot

# mmdash v0.1 Stage 8 Box / Experiment redesign preparation handoff

- Updated: 2026-08-15
- Branch: `main`
- Base: `origin/main@12c1b35`
- Scope: design and implementation handoff only; the Stage 8 runtime refactor is
  intentionally not implemented in this change.
- Authority: the Stage 8 sections in all three documents under
  `docs/design/v0.1/`, plus `docs/development/box.md` and
  `docs/development/experiment.md`, now supersede the 2026-08-11 Stage 8
  implementation snapshot later in this file.

## Why Stage 8 must be refactored

The merged Stage 8 code proves the basic Core -> Box Gateway -> Local
Docker/E2B execution chain, but its product identity and persistence model do
not match the newly confirmed design:

- `box_nodes.project_id` and Project-scoped Box Token make installation depend
  on one Project, although Box must be a user-owned device reusable across
  Projects;
- the binding model permits one Box per Project rather than many-to-many;
- Gateway cancels or recovers work from short lease loss, although an offline
  Box must continue Runtime execution for up to 72 hours and later replay logs;
- callback/log state is not a durable restart-safe spool with execution epoch
  and sequence acknowledgement;
- Web exposes only a minimal Experiment form and read-only Box status, not
  personal Box management, Project assignment, settings, result tree, compare,
  retry, or complete failure UX;
- Experiment treats `artifact.zip` as the public result instead of separating
  the permanent raw Execution Bundle, result branch Commit, and large-file
  Artifact pointers;
- there is no Coding Agent `self` execution/result binding contract.

Do not patch new UI onto the legacy Project-owned Box model. Change contracts,
migrations, Core authority, Gateway protocol, and product workflows as one
vertical refactor.

## Frozen product and architecture decisions

- Box is installed on a user-controlled device and uses the existing CLI-like
  browser device flow. Exchange issues a short-lived Box registration grant,
  then a dedicated account-level Box Token; Box never stores a user Session.
- One Box may be assigned to multiple Projects and one Project may bind
  multiple Boxes. Project owner/maintainer/editor can use assigned Boxes;
  editor cannot change assignment. The Box owner leaving a Project force
  removes only that Project assignment and active tasks, not the global Box or
  other Projects.
- Gateway is outbound-only: heartbeat, maximum 60-second long poll, callbacks,
  and resume. No public IP or inbound port is required.
- Box Control is the Core control plane. Box Gateway is the user-device
  controller. Sandbox is a Capability. Hosted/self-hosted E2B and Local Docker
  are Runtime Adapters under Sandbox; business modules never call E2B directly.
- Adapters compile into Box but advertise only after dependency/configuration
  and lifecycle probes. `auto` prefers E2B and falls back to Local Docker only
  before scheduling freezes. Explicit Runtime never falls back.
- Offline is connectivity, not execution failure. Runtime continues and
  Gateway persists state/logs/Bundle locally. Reconnect resumes by execution
  epoch and sequence. At the local log budget, execution continues and new log
  bytes are dropped with `logs_truncated=true`. Only 72 continuous offline
  hours produce `BOX_OFFLINE_TIMEOUT`.
- Normal revoke drains existing tasks; force revoke expires Token immediately,
  fails active related Experiments with `BOX_FORCE_REVOKED`, and compensates a
  pushed-but-unbound result with a revert Commit, never force-push.
- Experiment types are `box`, `box-re`, and `self`. Runtime failures do not
  auto-rerun after execution begins. Human rerun creates a new ID and immutable
  retry chain; old-ID reads return old data plus latest-ID guidance.
- Managed success is
  `created -> queued -> preparing -> running -> uploading ->
  processing_result -> succeeded`. Self success is
  `created -> awaiting_result -> verifying_result -> succeeded`. Success means
  Repo fetched/verified the remote result Commit and Experiment bound it.
- Result paths use Experiment creation time in the Project IANA timezone:
  `experiments/{exp_id}_{yyyymmdd_hhmm}/`. Paths never rename after timezone
  changes.
- Raw `execution-bundle.zip` is a permanent Artifact until Project purge.
  Worker safely extracts/preprocesses but never writes Git. Repo commits small
  files; large files become Artifact Versions referenced by
  `.mmdash/artifacts.json`.
- Project purge after the existing recycle period removes all mmdash-owned
  Project data, Artifact bytes, Bundle, Project-scoped grants/credentials,
  assignments, internal Repo worktrees/cache, Audit/Data Hub/Outbox/log state,
  but preserves the account-level Box and Box Token and never modifies the
  user's external repository or remote branches.

## Frozen data and migration route

Current main has continuous migrations through `000042_article_module`.
`000041_stage8_box_experiment` is already used and must remain immutable.

Planned append-only sequence:

1. `000043_box_account_nodes`: account owner, many-to-many Project binding,
   registering/online/offline/draining/revoked state, execution epoch, resume,
   ordered log acknowledgement/truncation, safe legacy backfill and
   reauthorization marker.
2. `000044_experiment_execution_results`: `box/box-re/self`, execution and
   connectivity states, failure record, retry chain, frozen result directory,
   actual Box/Runtime, result Commit, Bundle pointer, Manifest hash, log
   sequence and compensating result metadata.
3. `000045_project_stage8_purge`: durable idempotent Project purge progress for
   Stage 8 data, Artifact objects and internal Repo state.

If main advances, shift the numbers while preserving order and module
ownership. Tests must cover fresh, existing `000041`, revoked/active legacy
Boxes, partial processing, concurrent result commits, Project purge, and
down/up. Active legacy Box secrets cannot be reconstructed; require the new
device authorization rather than inventing credentials.

## Frozen contract route

Update source contracts first, then examples, mocks, generated clients, API
catalog and every process boundary.

- Auth device flow adds `client_kind=box` and returns a registration grant, not
  a CLI Session.
- Personal inventory: `/v1/users/me/boxes*`, including rename and
  `revoke(mode=drain|force)`.
- Project assignment becomes plural:
  `GET /v1/projects/{projectId}/boxes` and
  `PUT/DELETE /v1/projects/{projectId}/boxes/{boxId}`.
- Box control adds long-poll claim, resume handshake, batch log sequence
  acknowledgements, execution epoch, truncation and late diagnostic fields.
- Experiment schemas add new types/states/failure/retry/result fields, human
  rerun, self result binding, result tree/compare aggregation and BFF SSE logs.
- MCP target set is `experiment.create`, `experiment.run`,
  `experiment.status`, `experiment.result.bind`, `result.get`,
  `artifact.upload`, and `artifact.read`.
- `self` creation must return the exact result directory, Manifest Schema,
  Git/Artifact threshold, `.mmdash/artifacts.json` example, push requirement,
  and binding Tool instructions. Bind accepts only a pushed result Commit that
  Repo can fetch and validate.

## Recommended implementation order

1. Source OpenAPI/event/MCP schemas, examples, mocks, API docs, generated
   clients, and compatibility errors for legacy singular Box routes.
2. Append-only migrations plus fresh/existing/down-up integration tests.
3. Auth Box device registration grant and account-level Box credential.
4. Core Box ownership, assignment, permissions, scheduler, drain/force,
   offline/resume/log protocol and Project purge participation.
5. Gateway durable state/spool, long poll, restart/reconnect, removal of
   lease-loss Runtime cancellation, hosted/self-hosted E2B probes, and explicit
   Local Docker availability.
6. Experiment state/failure/retry/self/result-finalization domain changes.
7. Artifact raw Bundle and large-result support; Repo fixed-Commit transfer,
   serialized result writes/fetch verification and compensating revert; Worker
   typed extraction/preprocessing.
8. Web/BFF personal Box management, Project Box and Experiment Settings,
   Experiment cards/detail/read-only terminal/tree/preview/compare/retry.
9. Data Hub, events, Audit, Notification, Agent analysis and MCP/CLI completion.
10. Focused tests, `pnpm check`, Docker acceptance, offline/restart/reconnect,
    multi-Project fairness, drain/force, purge, managed Box, self-run, hosted
    E2B, self-hosted routing mock, and Local Docker acceptance.

## Definition of handoff-ready implementation

The refactor is not complete merely when Box can run a command. It is complete
when a newly installed outbound-only Box can device-login, serve several
Projects, survive offline execution/restart, replay or explicitly truncate
logs, publish a verified mixed Git/Artifact result, and drain/revoke safely;
when self-run Coding Agents receive and satisfy the exact result contract; and
when Web users can manage Boxes, inspect full Experiment history/results,
compare runs and follow retry chains. All contracts, migrations, generated
code, docs, security/Audit, Data Hub and full acceptance evidence must agree.

# mmdash v0.1 Stage 9 Article handoff

- Updated: 2026-08-13
- Branch: `codex/stage-9-article`
- Base: `origin/main@ac23ccf471ad`
- Canonical migrations: continuous `000001` through
  `000042_article_module`; Stage 8 is now
  `000041_stage8_box_experiment` with an explicit compatibility alias for the
  pre-merge `000033_stage8_box_experiment` filename.
- Delivery state: the complete Article vertical slice and real container
  acceptance are complete. The branch is ready for a non-draft PR and human
  review; do not merge it without that review.

## 2026-08-13 Stage 9 Article

Article is a Core-owned Markdown-first publishing module. It adds PostgreSQL
authority, Web/BFF collaboration, immutable Git checkpoints, Artifact-backed
build inputs/outputs, a fixed Worker toolchain, Data Hub projections,
Agent-assisted semantic insertions, Zotero reference freezing, and human-only
Release publication without creating a parallel repository, file table, job
queue, or provider path.

### Delivered workflow

- The Article page has Write, History, Templates, and Zotero workspaces. Write
  uses a two-column Tiptap/Yjs editor with a collapsible/resizable source panel,
  Reference/Artifact/Zotero/PDF tabs, Problem/Model/Experiment source filters,
  autosave/offline state, fixed-reference insertion, reviewed Agent patches,
  Ctrl+S flush, commit-only, and Commit -> Build -> Release actions.
- Web BFF hosts project-scoped Hocuspocus-compatible WebSocket rooms while Core
  remains authoritative. It enforces browser authentication, Project roles,
  Viewer read-only behavior, 32 connections per Project, 4 MiB messages,
  bounded pre-auth buffering, compare-and-swap flush retries, and a flush
  barrier before commit, preview, or publication.
- Core persists Draft revisions, Yjs state, canonical Tiptap JSON, blocks,
  fixed references, patch proposals, commits, builds, publications, templates,
  Zotero bindings, and immutable releases. Commits freeze exact collaborative
  state and write through Repo to the Article branch; restoring a commit
  restores that exact state. A failed build retains its commit and publication
  record for explicit retry.
- Formal builds pin one commit, template Artifact Version, every referenced
  Artifact Version, checksums, engine, bibliography tool, exact toolchain, and
  resource limits. Preview is latest-only; one commit can own multiple builds;
  a Release points to one successful build and is rejected by a database
  trigger if updated.
- Worker accepts only job-scoped transfer grants, validates ZIP paths/counts/
  expansion, rejects scripts and executable entries, rewrites only canonical
  `mmdash://artifact/.../versions/...` resources, invokes tools with argument
  arrays and no shell escape, applies CPU/memory/file/process/disk/output/time
  limits, sanitizes logs, and emits PDF, TeX, reproducible source ZIP, report,
  log, and SyncTeX Artifact Versions.
- Release history exposes immutable source/PDF/report/SyncTeX downloads and a
  three-column source tree, read-only source, and fixed PDF view. Parsed
  SyncTeX supports source-to-PDF and PDF-position-to-source navigation.
- Templates use `article-template.schema.json`, immutable Artifact Versions,
  registration validation, real test builds, stable error codes, and a secure
  Overleaf ZIP import/conversion wizard. The Worker image pins
  `python:3.12.11-slim-bookworm`, Pandoc 2.17.1.1, latexmk 4.79, biber 2.18,
  and TeX Live 2022/Debian.
- Zotero is a read-only Project binding with encrypted API credentials. Added
  references freeze item version and metadata. Artifact semantic description
  is a durable Core Job and deterministic Article Agent session; the Worker
  never calls a model provider directly.
- Article events flow through Outbox into Notification, Home/Progress, and Data
  Hub projections. OpenAPI, generated clients, event schemas, endpoint/event
  catalogs, ADRs, security notes, template specification, and the Article
  component guide are current.

### Verification evidence

- `pnpm check` passed on 2026-08-13: TypeScript/Go/Python lint, 134 Web tests,
  62 Web BFF tests including two real concurrent WebSocket clients and Viewer
  read-only behavior, 37 MCP Gateway tests, all Go backend/box/CLI tests, 44
  Worker tests, all production builds, contract compatibility, API catalog
  coverage for 477 operations across 16 contracts, and Caddyfile validation.
- Windows without developer-mode symlink privilege explicitly skips two
  symlink-construction assertions. The complete Go suite was also run in the
  official Linux Go 1.26 container, where both assertions executed and every
  backend/box/CLI package passed.
- Real PostgreSQL tests passed for the complete fresh 42-migration catalog,
  legacy filename reconciliation, partial/coexisting states, recent down/up,
  concurrent same-tag Release serialization, and database-enforced Release
  immutability.
- `docker compose -f deploy/compose/compose.yaml up -d --build` succeeded and
  `pnpm smoke` passed with Docker Worker mode, native Go CLI, MCP, Audit, Job,
  and Data Hub coverage. The initial start exposed the duplicate Stage 8
  migration number already present on `origin/main`; the canonical renumbering
  and legacy alias above fixed both fresh and existing development databases.
- `pnpm smoke:article-worker` passed in a `--network none` Worker container
  using a real template, Markdown manuscript, BibTeX database, and fixed PNG
  Artifact. It produced a 41,106-byte PDF and 94,827-byte reproducible source
  ZIP, ran BibTeX, and verified PDF/source/report/log/SyncTeX/TeX outputs plus
  frozen image content and checksums. Reported tool versions matched all four
  pinned contracts.
- All long-running Compose services were healthy. Recent logs contained no
  panic/fatal/error or credential-leak markers. Acceptance ended with ordinary
  `docker compose down`, never `down -v`; PostgreSQL, MinIO, Artifact, and Repo
  volumes remain preserved.

### Operational notes

- Article builds require a registered template in `ready` state and a Worker
  image matching the exact toolchain strings carried in the Job input. Drift
  fails with `ARTICLE_TOOLCHAIN_MISMATCH` before template execution.
- The standard repository smoke verifies transport with `system.test`;
  `pnpm smoke:article-worker` is the real network-isolated Article compilation
  acceptance and should remain part of Stage 9 release verification.
- Core Go changes require restarting the local development environment. Local
  bootstrap defaults remain `admin@mmdash.local` / `mmdash-local-admin` unless
  `.env` overrides the documented variables.

# Historical mmdash v0.1 Stage 8 Experiment / Box / Sandbox handoff

> Historical implementation evidence from 2026-08-11 only. Its Project-scoped
> Box identity, short-lease recovery, Artifact-only result and “Stage 8
> complete” conclusion are superseded by the 2026-08-15 redesign preparation
> at the top of this file. Preserve the evidence, but do not use it as current
> implementation authority.

- Updated: 2026-08-11
- Branch: `codex/stage-8-experiment-box-sandbox`
- Worktree: `/data/yile.chen/code/mmdash-stage8`
- Base: local `2fe2c05` (`fix(repo): stabilize maintenance git identity`)
- Draft PR: <https://github.com/Kozmosa/mmdash/pull/35>; keep Draft and do not
  merge it.
- Remote note: a fresh fetch on 2026-08-11 confirmed
  `imouup/main == origin/main == 9282b4e`; the `imouup` remote has no newer
  Stage 8 fix to integrate. Before the final push, the PR head contained the
  six committed Stage 8 changes through `4b076fb`; the verified follow-up
  commit described in this handoff advances that same branch.
- Migration: `000041_stage8_box_experiment` (renumbered after merge from the
  pre-merge `000033_stage8_box_experiment` name, which remains a migration
  compatibility alias for existing development databases)
- Historical delivery state: the legacy Stage 8 implementation, Local Docker
  terminal acceptance, and the legacy
  `Core -> Project-scoped Box Gateway -> E2B` hosted acceptance completed. The
  current redesign remains unimplemented.

## Stage 8 implementation snapshot

Implemented in this worktree:

- Core Experiment lifecycle, frozen `run_spec`, PostgreSQL task queue,
  lease/timeout/cancel/recovery paths, Audit, Outbox, and Data Hub projections.
- Core Box registration, scoped token use, heartbeat/offline state, binding,
  task lease/status/log/result/artifact boundaries, permission checks, and a
  formal revoke lifecycle that unbinds the Box, records Audit/Outbox, and asks
  Auth to revoke the project-scoped Box credential.
- Artifact-side `artifact.zip` stream staging with manifest, path, symlink,
  zip-slip, file count, uncompressed size, file hash, and declared-size checks.
- Independent Go 1.26 Box Gateway, commit-marker workspace pinning, restart
  state, Local Docker runtime, official-protocol E2B adapter, and Sandbox
  artifact packaging.
- Web/BFF, MCP Gateway, CLI, Worker handlers, contracts, event schemas,
  endpoint catalog, Stage 8 development guides, optional Compose Box profile,
  and opt-in Stage 8 smoke flow.

Verification completed:

- `pnpm contracts:generate`, `pnpm contracts:check`, `pnpm api:check`, and the
  complete `pnpm check` passed.
- `go test -race ./box/...` passed with the permanent E2B Platform
  REST/Envd Connect mock, including secure routing, transfer, logs, metrics,
  timeout, cancellation, malformed-create reconciliation, redaction, and
  cleanup. Focused Auth, Box Control, Experiment, Data Hub, Audit, and Core
  tests also passed against the real isolated PostgreSQL database.
- The current isolated Compose project is `mmdash-stage8-terminal`: Web
  `13100`, BFF `13101`, MCP `13102`, Core `18180`, PostgreSQL `15442`, and
  MinIO `19100/19101`. Current Core was rebuilt after the final SQL, Audit,
  maintenance, retry, Local Docker, artifact, and Box revoke changes; all six
  long-running services were healthy.
- Repo-backed Local Docker terminal smoke passed with Experiment
  `80022a50-338c-42e0-8d73-308620372abb` and Artifact
  `c3e40bb3-4743-4cba-badf-13d0de7d6a66`.
- Repo-backed hosted E2B terminal smoke passed with Experiment
  `0f37dbf1-0835-450a-ad98-c08e8ac95299` and Artifact
  `46abae4e-0543-476b-aaf2-4c5b9a1da3b4`. A direct official
  `/v2/sandboxes` query after cleanup returned zero active sandboxes.
- Both successful terminal smokes automatically revoked their Box node and
  credential. The acceptance database finished with zero active Stage 8 Box,
  Box Token, bootstrap Token, or Worker smoke Token. Ten older Box credentials
  and eleven older Worker smoke credentials from failed development runs were
  retired through product APIs before the final reruns.
- Recent Compose logs had no panic/fatal/error, `invalid audit input`, bad
  connection, or E2B credential-pattern match. Native CLI MCP login was
  skipped in the final terminal smokes only because this headless workstation
  has no unlocked Linux Secret Service keyring; CLI build and tests passed in
  `pnpm check`.
- The repository-requested `.localscripts/dev.ps1` entry point is absent in
  this checkout and was not claimed as used. Compose acceptance uses
  `up -d --build` and ordinary `down`, never `down -v`.
- After the final health, log, provider, and credential-leak checks, the
  `mmdash-stage8-terminal` project was stopped with ordinary `down`. Its
  PostgreSQL, MinIO, Artifact, and Repo volumes remain preserved.

## Known limits and acceptance evidence

- The E2B adapter follows the official JavaScript SDK `2.38.3` source at
  `cfd4bedd90558f12ddbf80763d90bcd3332423fe`: Platform REST create/detail/
  metrics/delete, Envd Connect JSON and `/files`, secure access headers,
  direct argv, dynamic custom-domain routing, and unconditional cleanup.
- Paid hosted acceptance passed success/log/file/artifact collection, a
  two-second timeout, active cancellation, and final sandbox count returning
  to zero. The full production-shaped Core/Box/E2B success path was rerun after
  the final Gateway, Experiment, Audit, Local Docker, artifact, and lifecycle
  fixes. The credential was not written to source, docs, handoff, logs, or the
  PR. The dashboard API Key ID/Project ID is not sent by the API client.
- The hosted `base` Template exposed one CPU, 512 MiB memory, and 10 GiB disk.
  Live acceptance therefore requests 512 MiB. Production checks each created
  sandbox's actual CPU/memory/disk capacity against the frozen request; Box
  deployment limits should be configured conservatively for all enabled
  runtimes.
- The default Box workspace mode consumes a Repo-owned detached checkout and
  requires a matching `.mmdash-commit` marker. It intentionally does not run
  Git or accept long-lived Git credentials.
- The optional Compose Box profile requires a Core-issued registration token,
  a populated read-only workspace volume, and explicit Docker socket privilege
  for Local Docker.
- Local Docker acceptance used the already cached `python:3.12-slim` image.
  An initial rerun with uncached `python:3.12-alpine` failed at Docker image
  pull because Docker Hub timed out; the Experiment correctly recorded
  `NON_ZERO_EXIT`, and the failed run still revoked its Box and credential.

# mmdash v0.1 Stage 7 integration handoff

- Updated: 2026-08-11
- Branch: `main`
- Current delivery commit: `c88a4a0 feat(progress): harden automatic evaluation workflow`
- Canonical migrations: continuous `000001` through
  `000040_progress_reasoning_effort`
- Delivery state: Agent single-token authentication, private-Core boundary,
  recoverable instance removal, full-screen workbench, and Agent Artifact
  upload are integrated with the Progress human scheduling workbench and review
  policy; the merged repository gate and Docker Compose smoke acceptance pass;
  Project and per-Run instructions explicitly advertise Markdown/KaTeX support

## 2026-08-11 Progress automatic evaluation hardening

- Periodic Progress evaluation is now scheduled by Core/PostgreSQL rather than
  Hermes Jobs. PostgreSQL owns due-time calculation, leases, retries, request
  deduplication, and manual/automatic concurrency; Hermes only executes the
  resulting Progress Session Run.
- Manual evaluation is blocked while a real evaluation is queued or running.
  A stale Progress evaluation whose backing Job has already reached
  `succeeded`, `failed`, `cancelled`, or `timed_out` is reconciled to `failed`
  before deduplication, so an orphan `running` row cannot block future manual
  evaluation. This fixes the observed `nanako` record whose evaluation remained
  `running` after its Job became `timed_out`.
- A newly accepted manual request immediately renders the queue stage, polls
  Progress each second until its Evaluation row appears, and shows
  `Session 准备中` instead of reopening the previous completed Session. A
  genuinely merged request explicitly says that no new evaluation was created.
- The latest automatic evaluation exposes a lightweight read-only Session
  dialog. Sent evaluation input is collapsed by default, SSE updates are
  batched, Tool Calls and safe reasoning availability render inline, and the UI
  explains that absence of `reasoning.available` does not prove the model did
  not reason; hidden reasoning text remains unavailable.
- Project Settings now persist `reasoning_effort` and pass it to Hermes through
  `model_options.reasoning_effort`. The evaluator Prompt may consult current
  project state through granted mmdash MCP read tools and must not create or
  modify schedules without an explicit time; missing time becomes a pending
  question instead of an invented arrangement.
- Verification completed with Web TypeScript checks, focused Progress/Session
  tests, Core Agent/Progress Go tests, generated-contract checks, and manual
  browser confirmation of the queue and Session state transitions. A remaining
  live issue exists: after clicking `立即评估`, the newly queued Progress
  evaluation can still transition to `failed`. The queue-state/orphan-Job fixes
  must not be treated as resolving this downstream evaluator failure; the next
  investigation should capture the failed Evaluation error code, backing Job
  status/result, Agent Run status, and Core/Worker logs for the same IDs before
  changing retry behavior. Core Go changes require restarting the local
  development launcher; PostgreSQL and MinIO volumes remain preserved.

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
  instance. Cron due-time calculation, leases, retries, and request creation
  are owned by mmdash Core/PostgreSQL; Hermes only executes each evaluation Run.
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
