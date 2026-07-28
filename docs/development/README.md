# Local development

## Prerequisites

- Node.js 24 or newer and pnpm 11
- Go 1.18 or newer (the modules remain buildable with Go 1.17 during bootstrap)
- Python 3.11 or newer and uv
- Docker Compose

Copy `.env.example` to `.env`, then install and verify all workspaces:

```bash
pnpm install
uv sync --all-packages
pnpm check
```

## Run the baseline

```bash
pnpm dev
pnpm smoke
pnpm dev:down
```

The smoke verifies login/project creation, Web → BFF → Core → PostgreSQL,
Worker Job handling, Outbox delivery, Data Hub routing, Audit/request IDs, MCP
health, CLI startup, and metrics. Local ports are Web `3000`, BFF `3001`, Core
`8080`, PostgreSQL `5432`, MinIO API `9000`, and MinIO Console `9001`.

## Isolated native environment

Windows and Linux can run the same baseline without Docker. Pixi is the only
global prerequisite; Node.js, Go, Python, PostgreSQL, MinIO, caches, service
data, logs, and temporary files are kept under `.testenv`.

On Windows PowerShell:

```powershell
.\scripts\testenv.ps1 install
.\scripts\testenv.ps1 check
.\scripts\testenv.ps1 dev
```

On Arch Linux or another Bash environment:

```bash
./scripts/testenv.sh install
./scripts/testenv.sh check
./scripts/testenv.sh dev
```

The wrappers inherit explicit network proxy settings. For example, set
`HTTP_PROXY` and `HTTPS_PROXY` to `http://127.0.0.1:16888` before `install` when
the package registries are not directly reachable.

`dev` stays in the foreground and shuts down every child process on Ctrl+C. In
another terminal, verify the complete Web → BFF → Core → PostgreSQL chain:

```powershell
.\scripts\testenv.ps1 smoke
```

```bash
./scripts/testenv.sh smoke
```

If the terminal hosting `dev` becomes detached, request the same graceful
shutdown from another terminal with `.\scripts\testenv.ps1 stop` or
`./scripts/testenv.sh stop`.

The isolated defaults avoid the Docker Compose ports:

| Service       |    Port |
| ------------- | ------: |
| Web           | `13000` |
| Web BFF       | `13001` |
| MCP Gateway   | `13002` |
| Core          | `18080` |
| PostgreSQL    | `15432` |
| MinIO API     | `19000` |
| MinIO Console | `19001` |

Override a port with its `MMDASH_TESTENV_*_PORT` variable, for example
`MMDASH_TESTENV_WEB_PORT`. Values must be unique and available. Run `doctor` to
inspect the resolved tools, paths, ports, and supervisor state. Run `reset`
while the environment is stopped to remove only `.testenv/runtime`; installed
tools and dependency caches remain available.

Pixi isolates files and toolchains, not the operating-system kernel or network.
Existing repository-local outputs such as `node_modules`, `.next`, and `dist`
remain in their conventional ignored locations.

The production-shaped public entry is defined only in the repository-root
`Caddyfile`, using `mmdash.com`. If Caddy 2.10 or newer is installed, validate
it without starting services:

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
| `pnpm foundation:check`                       | Validate stage-3.15 static and process foundations  |
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
- [Stage 3.15 foundation acceptance](foundation-acceptance.md)
- [Contract development](contracts.md)
