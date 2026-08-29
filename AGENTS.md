# AGENT.md

This file is the repository-local operating guide for AI coding agents working
on mmdash.

## 1. Sources of truth

Use the following sources in order when making implementation decisions:

1. The user's current request and explicit local operating instructions in this
   file.
2. The current v0.1 design baseline indexed by
   `docs/design/v0.1/README.md`: implementation order v0.4, technical
   architecture v0.4, and product design v0.1.
   `docs\design\v0.1\基座功能与功能模块实现顺序v04.md`
   `docs\design\v0.1\技术架构设计文档v04.md`
   `docs\design\v0.1\设计文档v01.md`

3. Current OpenAPI, event, and JSON Schema contracts under `contracts/`, plus
   ADRs and the API catalog under `docs/`.
4. Current code, migrations, and tests.
5. `handoff.md` and recent Git history for recent project context. Treat the
   handoff as a dated snapshot and verify its branch and status against the
   current checkout before acting.

The `archive/v0.0/` tree and the `v0.0` tag/branches are historical references,
not implementation authority for v0.1. Do not copy the old architecture into
the active tree. If old behavior must be studied, use a separate worktree and
migrate only the necessary behavior into the v0.1 architecture.

When sources disagree, do not silently invent a third design. Resolve the
conflict from the authoritative documents and current contracts, record the
reason for the chosen interpretation, and update stale documentation when that
is within the task scope.

## 2. Project overview and implementation order

mmdash v0.1 is a collaborative workbench for mathematical modeling and similar
research projects. It connects mature external tools such as coding agents,
Hermes, Notion, Zotero, Git, and sandbox runtimes to one versioned Project Data
Hub instead of reimplementing them.

The product is designed around this traceable workflow:

```text
human milestones
  -> Agent-assisted TODO planning
  -> versioned model snapshots
  -> MCP/CLI experiment requests
  -> archived results
  -> Markdown article authoring
  -> LaTeX build
  -> human-reviewed release
```

The v0.1 implementation order is:

```text
Stage 0 foundation
  -> Stage 1 Repo
  -> Stage 2 Artifact
  -> Stage 3 extensible Go CLI foundation and local MCP access
  -> Stage 4 Home and Progress
  -> Stage 5 Agent sessions
  -> Stage 6 automatic progress tracking
  -> Stage 7 Model
  -> Stage 8 Experiment, Box, and Sandbox
  -> Stage 9 Article
  -> Stage 10 hardening and release
```

The order defines dependencies and module ownership; it is not permission to
reduce the product scope defined by the design documents. Confirm the current
implementation state from code, tests, Git history, and the latest handoff
instead of recording transient progress in this file.

PostgreSQL is the Job Queue backend and uses `FOR UPDATE SKIP LOCKED`. Do not
introduce Redis or another infrastructure dependency without an approved
architecture change.

## 3. Repository map

```text
apps/web/                 Next.js product UI
apps/web-bff/             TypeScript session, security, and aggregation layer
apps/mcp-gateway/         TypeScript remote MCP transport and policy layer
clients/cli/              Stage 3 native Go CLI and local stdio MCP bridge
packages/                 Shared TypeScript packages and generated clients
contracts/                OpenAPI, event schemas, JSON Schemas, examples, mocks
backend/                  Go Core modular monolith and migrations
workers/mmdash-worker/    Python asynchronous worker
box/                      Gateway, capabilities, runtimes, and contracts
deploy/                   Compose and deployment support
docs/                     Architecture, ADR, API, event, and development docs
archive/v0.0/             Frozen historical material only
```

Stage 3 replaces the temporary TypeScript CLI shell with a separately buildable
Go module and native binary. Keep the CLI extensible through compile-time
feature registration: Stage 3 owns the common command, configuration,
credential, transport, output, diagnostics, and stdio MCP foundations; later
domain stages add their own human commands. Do not couple the Go CLI to shared
TypeScript runtime packages.

The CLI serves a human user and local Coding Agents. Hermes and other bound
mmdash Agent instances are independent remote MCP clients: at the later Agent
stage they authenticate directly to the MCP Gateway with a Project-scoped,
revocable Agent Token. They never connect through the CLI, and the Hermes API
Key used by mmdash to call Hermes is a separate, opposite-direction
credential. The current environment-configured Gateway Agent token is only a
foundation development/test mechanism, not that product lifecycle.

Use the component guide under `docs/development/` before changing an unfamiliar
boundary. Use `docs/api/README.md`, `docs/events/`, and
`docs/architecture/README.md` as searchable indexes rather than guessing an
endpoint or event.

## 4. Working method

Before editing:

1. Inspect `git status`, the current branch, and relevant recent commits.
2. Read the owning design section, component guide, contract, and tests.
3. Identify the module owner for every state mutation.
4. Preserve all pre-existing user changes, including ignored local files.
5. Define the smallest complete vertical slice and its acceptance evidence.

Implement product modules vertically. A full slice normally considers:

```text
Web UI and interaction
  -> Web BFF route
  -> Core OpenAPI
  -> Core domain module
  -> database migration
  -> settings and adapters
  -> Worker or Box runtime when needed
  -> domain event and Outbox
  -> Data Hub projection and data.read
  -> MCP/CLI support when required
  -> authorization and Audit
  -> unit, integration, contract, and end-to-end tests
  -> module documentation
```

A layer may be omitted only when it is genuinely unnecessary and the module
documentation explains why. Do not deliver only a backend when the design
requires a usable page and workflow.

Do not create temporary parallel architectures. In particular:

- do not create module-specific file tables before Artifact is stable;
- do not bypass Repo for Experiment or Article Git operations;
- do not let Agents invent private task storage before Progress is stable;
- do not create another coding-agent interface before the minimum CLI/MCP loop
  is complete;
- do not let Box consume private ad hoc request formats before Experiment
  contracts are frozen;
- do not bypass Core, contracts, permissions, Audit, or Data Hub with a
  short-lived direct database path.

Keep changes focused. Do not rewrite unrelated files, discard user work, update
dependency versions without need, or perform destructive cleanup merely to
make a check pass.

## 5. Local development environment

There are two local paths that start the full development stack. When
`pixi` is available, always prefer the Docker-free Pixi path; use the
Docker-based wrapper only as a fallback.

### Preferred: Pixi isolated environment (Docker-free)

`pixi` is the only global prerequisite. It provides Node.js 24, Go 1.26,
Python 3.13, `uv`, PostgreSQL 16, MinIO, and Caddy entirely under the
repository-local `.testenv/` directory (`.testenv/pixi.toml` and
`.testenv/pixi.lock` are tracked in git). No Docker daemon, global Node, or
global Go install is required. Read `docs/development/native-environment.md`
before changing anything on this path.

```powershell
.\scripts\testenv.ps1 install   # one-time: toolchain + workspace dependencies
.\scripts\testenv.ps1 doctor    # verify tool versions and port availability
.\scripts\testenv.ps1 dev       # start everything in the foreground
```

Bash equivalent: `./scripts/testenv.sh <task>`. Available tasks: `install`,
`doctor`, `check`, `test`, `dev`, `smoke`, `stop`, `reset`. Stop the dev
environment with Ctrl+C in its terminal or `testenv.ps1 stop`; `reset`
recreates the isolated state and deletes `.testenv` service data.

### Fallback: Docker-based local wrapper

Requires a running Docker daemon plus Node.js 24/pnpm 11, Go 1.26, and
Python 3.11+/`uv` on PATH. The Stage 3 CLI uses its own Go module but must
remain aligned with the repository's Go 1.26 workspace and CI toolchain. A
CLI dependency must not silently introduce a second Go version or bypass the
workspace checks.

```powershell
node .localscripts\dev.mjs [--check | --skip-install | --skip-worker | --down]
```

The wrapper installs frozen pnpm and uv workspaces, starts PostgreSQL and
MinIO in Docker, applies migrations, builds the generated Core client, starts
the application services and waits for health endpoints, and issues a
temporary Worker API token when necessary. Go Core is launched once with
`go run` and is not file-watched, so restart the wrapper after changing Go
code to rebuild and run the updated Core process.

On this workstation the gitignored `.env` pins `POSTGRES_HOST_PORT=15432`
(Windows WinNAT reserves TCP 5355-5454, which blocks the default 5432) and
sets `GOPROXY=https://goproxy.cn,direct` because `proxy.golang.org` is
unreachable. Check `.env` before debugging port or module-download failures
on this path.

After a successful start, report the reachable services to the user:

| Service       | Pixi path (preferred)    | Docker wrapper fallback |
| ------------- | ------------------------ | ----------------------- |
| Web           | `http://127.0.0.1:13000` | `http://localhost:3000` |
| Web BFF       | `http://127.0.0.1:13001` | `http://localhost:3001` |
| Core          | `http://127.0.0.1:18080` | `http://localhost:8080` |
| MCP Gateway   | `http://127.0.0.1:13002` | `http://localhost:3002` |
| PostgreSQL    | `127.0.0.1:15432`        | `localhost:15432` (*)   |
| MinIO API     | `http://127.0.0.1:19000` | `http://localhost:9000` |
| MinIO Console | `http://127.0.0.1:19001` | `http://localhost:9001` |

(*) Docker wrapper: 5432 by default, 15432 with the local `.env` override
described above.

Also report the local bootstrap login:

```text
Email: admin@mmdash.local
Password: mmdash-local-admin
```

These are development defaults only. If `.env` overrides the bootstrap
credentials, state that the defaults do not apply and name the relevant
variables without copying secret values into chat or logs.

Stopping the Docker wrapper leaves its PostgreSQL and MinIO containers
available for fast restarts; use `--down` to stop that infrastructure. Never
delete volumes unless the user explicitly approves deleting the persisted
PostgreSQL and MinIO development data. Never run both paths at the same time:
they share no ports except PostgreSQL 15432, where a collision breaks the
newer process.

## 6. Network and dependency downloads

The available local proxy is `127.0.0.1:22334`, but it may be slow. When a
dependency download is slow:

1. prefer an appropriate reputable mirror in mainland China when the package
   manager can still enforce the lockfile and integrity hashes;
2. reuse the repository-local pnpm, uv, and Go caches where safe;
3. poll infrequently and allow the current download enough time to progress, and reduce token usage;
4. do not repeatedly reinstall or regenerate lockfiles;
5. after three unsuccessful attempts, stop retrying and ask the user to
   intervene, including the command and concise failure evidence.

Do not change machine-wide proxy or registry configuration without explicit
authorization. Never weaken TLS or checksum verification to make a download
succeed.

## 7. Contracts, migrations, and generated code

For every cross-process contract change:

1. edit the source OpenAPI or JSON Schema under `contracts/`;
2. update examples and contract-mock fixtures;
3. run `pnpm contracts:generate`;
4. use generated request and response types in implementations;
5. update `docs/api/endpoints.md` for every added or changed operation;
6. run `pnpm contracts:check` and the owning module tests.

Never hand-edit generated files. Never rewrite the compatibility baseline just
to hide an unexplained breaking change. If a breaking change is intended,
document and review it explicitly.

Migrations belong to the owning Core module and must remain safe for both a
fresh database and supported existing development data. Do not rename, reorder,
or destructively rewrite an already used migration without a deliberate
migration and compatibility plan.

Events must use the standard envelope, stable schemas, and catalog
documentation. New asynchronous handlers need retry, idempotency, failure, and
observability tests.

## 8. Verification policy

Use the narrowest useful feedback loop while implementing:

```powershell
pnpm lint
pnpm test
pnpm build
pnpm contracts:check
pnpm api:check
```

Prefer the owning package or language-specific test first, then expand. For
contract changes, generate before checking. Before handing off a code change,
run the complete repository gate:

```powershell
pnpm check
```

`pnpm check` covers TypeScript, Go, and Python linting, tests, builds,
contracts, API catalog coverage, and Caddy validation. Do not report success if
tests were silently skipped or if generated files are stale.

After completing a module or implementation stage, run the Docker acceptance
path in addition to `pnpm check`:

```powershell
docker compose -f deploy/compose/compose.yaml up -d --build
pnpm smoke
docker compose -f deploy/compose/compose.yaml down
```

Use module-specific environment variables and acceptance steps from
`docs/development/` and the current module documentation.

Inspect Compose health, readiness, and recent logs after full-stack tests.
Confirm there are no panic/fatal/error messages or credential leaks. Shut down
with `down`, never `down -v`, unless volume deletion was explicitly requested.

Documentation-only changes do not require an unnecessary full Docker rebuild,
but their paths, commands, links, and factual claims must still be checked
against the current repository.

You do not need to test caddy service, just test caddyfile.

## 10. Definition of done and handoff

A completed stage or product module includes:

- a usable end-to-end user workflow;
- OpenAPI and event contracts;
- migrations and authoritative persistence;
- Data Hub projections;
- permissions and Audit;
- tests at every changed process boundary;
- current operational and API documentation;
- integration with previously completed modules.

Validate commit subjects when preparing a commit:

```powershell
pnpm commit:check -- "feat(scope): concise summary"
```

Remember to update handoff.md if you have just completed a module or completed a stage
