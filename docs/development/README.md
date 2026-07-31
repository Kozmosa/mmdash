# Local development

## Prerequisites

- Node.js 24 or newer and pnpm 11
- Go 1.18 or newer (the backend module remains runnable with Go 1.17)
- Python 3.11 or newer and uv
- Docker Compose

Caddy 2.10 or newer is optional for validating the production-shaped public
entry point.

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

The baseline smoke verifies login/project creation, Web → BFF → Core →
PostgreSQL, Worker Job handling, Outbox delivery, Data Hub routing,
Audit/request IDs, MCP health, CLI startup, and metrics. Set
`MMDASH_SMOKE_REPO_MODE=docker` to add the managed Local Git Stage 1 E2E
described in the [Repo guide](repo.md). Local ports are Web `3000`, BFF `3001`,
Core `8080`, PostgreSQL `5432`, MinIO API `9000`, and MinIO Console `9001`.

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
- [Repo development, deployment, and acceptance](repo.md)
- [Artifact development, deployment, and Core acceptance](artifact.md)
- [Artifact Web, BFF, and resumable upload behavior](artifact-web.md)
