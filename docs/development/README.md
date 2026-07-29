# Local development

## Prerequisites

- Node.js 24 or newer and pnpm 11
- Go 1.18 or newer (the backend module remains runnable with Go 1.17)
- Python 3.11 or newer and uv
- Docker Compose

Caddy 2.10 or newer is optional for validating the production-shaped public
entry point.

## One-command development environment

The one-command environment runs PostgreSQL and MinIO in Docker, then runs the
hot-reloading application services natively:

| Component     | Runtime              | Local address           |
| ------------- | -------------------- | ----------------------- |
| Web           | Next.js              | `http://localhost:3000` |
| Web BFF       | TypeScript / Node.js | `http://localhost:3001` |
| Core          | Go                   | `http://localhost:8080` |
| MCP Gateway   | TypeScript / Node.js | `http://localhost:3002` |
| Worker        | Python / uv          | Core Job API            |
| PostgreSQL    | Docker Compose       | `localhost:5432`        |
| MinIO API     | Docker Compose       | `http://localhost:9000` |
| MinIO Console | Docker Compose       | `http://localhost:9001` |

On Windows PowerShell:

```powershell
.\scripts\dev.ps1
```

If the local execution policy blocks scripts:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1
```

On Linux, macOS, or WSL:

```bash
bash ./scripts/dev.sh
```

The script reads `.env` when present, installs the pnpm and uv workspaces,
starts and waits for PostgreSQL and MinIO, applies Go migrations, starts every
application process, and waits for their health endpoints. If
`MMDASH_WORKER_API_TOKEN` is not configured, it logs in as the bootstrap admin
and creates a temporary unscoped development API token for the Python Worker;
that token is revoked when the script exits.

Press `Ctrl+C` to stop the native application processes. PostgreSQL and MinIO
remain available for fast restarts. Stop them and remove the Compose network
with either wrapper:

```powershell
.\scripts\dev.ps1 --down
```

```bash
bash ./scripts/dev.sh --down
```

Useful optional flags:

- `--check` starts the full environment, verifies every health endpoint, and
  then stops the native processes.
- `--skip-install` skips `pnpm install` and `uv sync` when dependencies are
  already current.
- `--skip-worker` starts the stack without the Python Worker.
- `--help` prints all supported options.

Copy `.env.example` to `.env` only when you need to customize local
credentials or settings. The native services always use the host-published
PostgreSQL and MinIO ports; set `MMDASH_DEV_DATABASE_URL` or
`MMDASH_DEV_OBJECT_STORAGE_ENDPOINT` to override those native endpoints.

After the environment reports ready, verify the complete example path from a
second terminal:

```bash
pnpm smoke
```

## Install and verify

To install and verify all workspaces without starting services:

```bash
pnpm install
uv sync --all-packages
pnpm check
```

## Run the containerized baseline

The existing package command builds and runs the application services as
containers instead of native hot-reloading processes:

```bash
pnpm dev
pnpm smoke
pnpm dev:down
```

The smoke verifies login/project creation, Web → BFF → Core → PostgreSQL,
Worker Job handling, Outbox delivery, Data Hub routing, Audit/request IDs, MCP
health, CLI startup, and metrics.

## Isolated native environment

Run the complete baseline on Windows or Linux without Docker with the
[native environment guide](native-environment.md). It uses Pixi as the sole
global prerequisite and keeps service data and toolchains under `.testenv`.

The production-shaped public entry is defined only in the repository-root
`Caddyfile`, using `mmdash.com`. Validate it without starting services:

```bash
caddy validate --config Caddyfile --adapter caddyfile
```

Run migrations independently with:

```bash
DATABASE_URL=postgres://mmdash:mmdash@localhost:5432/mmdash?sslmode=disable pnpm migrate
```

## Quality commands

| Command                                       | Purpose                                            |
| --------------------------------------------- | -------------------------------------------------- |
| `pnpm lint`                                   | TypeScript, Go formatting, and Python lint         |
| `pnpm test`                                   | TypeScript, Go, and Python tests                   |
| `pnpm build`                                  | All three language builds                          |
| `pnpm format`                                 | Format supported source files                      |
| `pnpm api:check`                              | Check OpenAPI operations against the API catalog   |
| `pnpm contracts:generate`                     | Regenerate TypeScript and Go contract outputs      |
| `pnpm contracts:check`                        | Validate schemas, mocks, generation, compatibility |
| `pnpm caddy:check`                            | Validate ingress invariants and Caddyfile syntax   |
| `pnpm commit:check -- "feat(scope): summary"` | Validate a commit subject                          |

## Create a module

```bash
pnpm scaffold:module -- sample
```

The generator creates explicit Core, BFF, Web, and contract starting points.
Read the generated `README.md`, add the endpoint to the OpenAPI contract and API
catalog, then add tests at every process boundary. Domain modules may omit a
layer only when their module documentation explains why.

## More guides

- [Web foundation](web.md)
- [Web BFF foundation](web-bff.md)
- [MCP Gateway foundation](mcp-gateway.md)
- [CLI development and packaging](cli.md)
- [Go Core Server foundation](core.md)
- [Worker development](worker.md)
- [Native development environment](native-environment.md)
- [Stage 3.15 foundation acceptance](foundation-acceptance.md)
- [Contract development](contracts.md)
