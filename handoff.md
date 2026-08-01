# mmdash v0.1 Stage 3 CLI and local MCP handoff

- Updated: 2026-08-01
- Branch: `main`
- Base: `origin/main@490b06e`
- Delivery state: implemented and validated for the Stage 3 delivery commit

## Status

Stage 3 is complete against the v0.1 implementation-order v0.4, technical-
architecture v0.4, and product-design v0.1 baselines. The temporary TypeScript
CLI has been replaced by a separately buildable Go 1.26 module and native
binary. Local Coding Agents can use the CLI's stdio MCP bridge, while hosted
Agent runtimes remain independent direct clients of the MCP Gateway.

Go Core remains authoritative for authentication, Project membership, RBAC,
Data Hub reads, Audit, and persisted state. The CLI stores only non-secret
configuration and current Project selection; access and refresh credentials
remain in the operating-system credential store.

## Delivered behavior

- Extensible compile-time Go Feature registration for commands and `doctor`
  checks; no runtime plugins and no TypeScript runtime dependency.
- Native Windows, macOS, and Linux credential adapters with no plaintext token
  fallback.
- Browser-approved CLI device authorization, bounded polling, delegated access
  and refresh-token rotation, `whoami`, logout, and server-scoped credential
  profiles.
- Versioned, atomically written non-secret `config.json` with platform paths,
  environment overrides, HTTPS enforcement, and loopback HTTP support.
- `mmdash config set-domain [domain]` updates the unified public, Core, and MCP
  endpoints together. The hosted default is the confirmed production domain;
  a loopback host such as `localhost:3000` automatically selects HTTP for local
  testing.
- Explicit Project discovery and selection through `project list`, `project
  use`, and `project current`.
- Transparent stdio-to-Streamable-HTTP `mmdash mcp` bridge with JSON-RPC stdout
  isolation, safe stderr diagnostics, session handling, delegated CLI auth,
  project propagation, and retry after token refresh.
- `doctor` checks for configuration, delegated identity, selected Project, and
  MCP Gateway reachability.
- MCP Gateway CLI-token authentication through Core `auth.me`, separate static
  development Agent-token support, constant-time static-token comparison,
  tool and Project authorization, per-call session/request context, Audit, and
  redacted structured logs.
- MCP reads for `project.list`, `project.get`, `data.list`, and `data.read`,
  plus the existing bounded foundation tools, all routed through Core rather
  than direct database access.
- Go CLI release workflow for Windows, macOS, and Linux amd64/arm64 archives
  and SHA-256 checksums.
- The confirmed hosted domain was changed in-place across the existing
  configuration, ingress,
  contract identifiers/examples, Gateway defaults/tests, and documentation.

## Contracts and persistence

Stage 3 adds Core operations for CLI device authorization, exchange, refresh,
logout, delegated identity, and Project reads. Generated Core client output,
contract mocks, the API catalog, and MCP tool schemas/documentation are aligned.

Migration `000015_cli_device_auth` owns device grants, CLI sessions, hashed
refresh credentials, expiry, revocation, and safe lifecycle constraints. No
additional queue or infrastructure backend was introduced.

## Verification

The following completed successfully on 2026-08-01 before the final domain-
configuration increment:

- `pnpm check`: exit 0, including lint, TypeScript/Go/Python tests and builds,
  generated-contract freshness and compatibility, 187 documented API
  operations, and Caddyfile validation.
- TypeScript suites: scripts 9/9, Web 60/60, Core client 6/6, validation 1/1,
  MCP Gateway 15/15, and Web BFF 30/30.
- Python Worker: 25/25.
- All Go packages, including the temporary-Git Repo integration suite.
- Exact Compose build and smoke path with the local
  `golang:1.26-alpine` image: all required services became healthy and the
  native CLI device-login, Project selection, and stdio MCP path passed.
- A second full smoke run against fresh tmpfs PostgreSQL and MinIO state passed;
  all required readiness endpoints returned HTTP 200, fault-log matches were
  zero, and credential/token/Authorization leak scans were zero.
- Both Compose environments were stopped with `down`, never `down -v`; named
  PostgreSQL and MinIO volumes were preserved.

After adding `config set-domain` and replacing the hosted domain, these narrow
checks passed:

- `go test ./clients/cli/...`
- `pnpm contracts:generate` followed by `pnpm contracts:check`
- `pnpm caddy:check` (`Valid configuration`; no Caddy service was started)
- `gofmt` on all changed CLI Go files
- `git diff --check`
- `\.localscripts\dev.ps1 --check --skip-install --skip-worker`: built the
  native CLI, generated its isolated local launcher, applied migrations, and
  reached healthy Core, Web BFF, Web, and MCP Gateway before stopping the
  application processes.

The local wrapper check reused Docker only for PostgreSQL and MinIO. It did not
repeat the full Compose image build or Stage 3 smoke acceptance.

## Operational notes

- Local bootstrap defaults remain `admin@mmdash.local` and
  `mmdash-local-admin` unless overridden by the documented environment
  variables.
- The environment-configured static Gateway Agent token remains only a
  foundation development/test mechanism. Project-scoped, revocable Agent-token
  lifecycle belongs to the later Agent stage.
- A historical persisted Repo record produced one `repo.sync.failed` line in
  the first Compose log review. A fresh-state rerun produced zero fault-log
  matches, proving it was pre-existing data rather than this stage's smoke path.
- The ignored local `\.localscripts\dev.mjs` now enforces Go 1.26, supports
  per-service application-port overrides, forces safe local service URLs,
  builds `\.tmp\dev-tools\mmdash.exe`, and generates an isolated
  `mmdash-local` launcher. It remains workstation-local by repository policy.
- The pytest basetemp directory created during validation was explicitly
  inspected and safely removed, including its five internal-only symlinks.

## Boundaries and next stage

Stage 3 intentionally does not implement Artifact-specific human CLI commands,
Agent-instance lifecycle, product Agent tokens, Progress, Model, Experiment,
Box, Sandbox, or Article behavior. Later domain stages should register their
commands and diagnostics through the Go Feature boundary and reuse the same
authenticated Core/MCP transports.

The next implementation stage is Stage 4 Home and Progress.
