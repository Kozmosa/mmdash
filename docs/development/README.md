# Local development

## Prerequisites

- Node.js 24 or newer and pnpm 11
- Go 1.26 or newer (the Go 1.26 toolchain is required by the backend module)
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
MCP Gateway `3002`, Core `8080`, PostgreSQL `5432`, MinIO API `9000`, and MinIO
Console `9001`.

The current Stage 8 implementation still exposes a legacy opt-in Box profile.
It requires an operator-provided Core registration credential and a Repo-owned
detached workspace with a matching `.mmdash-commit` marker:

```bash
docker compose -f deploy/compose/compose.yaml --profile box up -d --build
MMDASH_SMOKE_REPO_MODE=docker MMDASH_SMOKE_STAGE8=1 pnpm smoke
```

Treat this smoke only as migration-source regression coverage while completing
the account-owned, outbound, offline-resumable redesign in the
[Box guide](box.md) and [Experiment guide](experiment.md); it is not acceptance
for the target Stage 8 product. The normal smoke does not start Box or claim an
Experiment. The optional Stage 8 smoke first creates the managed Local Git
fixture and then creates and reads a frozen Experiment from its fixed code commit; set
`MMDASH_SMOKE_STAGE8_RUN=1` only when a registered online Box is available.
`MMDASH_SMOKE_STAGE8_COMMIT` may override the fixture commit for a separately
prepared Repo project. Do not use `down -v`; the Box, Artifact, PostgreSQL,
MinIO, and Repo data volumes are intentionally preserved.

The legacy Box advertises E2B only when `E2B_API_KEY` is injected. Do not carry
that single-variable availability rule into the redesign: the target Adapter
must pass dependency, configuration, connection, creation, and deletion probes
before advertising either hosted or self-hosted E2B.

## Isolated native environment

Run the complete baseline on Windows or Linux without Docker with the
[native environment guide](native-environment.md). It uses Pixi as the sole
global prerequisite and keeps service data and toolchains under `.testenv`.

The production-shaped public entry is defined only in the repository-root
`Caddyfile`, using `mmdash.moe`. Browser traffic uses `/api`, the native CLI
uses the authenticated `/v1` user API surface and `/mcp`, and Core is never
exposed publicly. Validate it without starting services:

```bash
caddy validate --config Caddyfile --adapter caddyfile
```

`pnpm caddy:check` uses that local binary when available and otherwise runs
the same validation in the pinned Caddy 2.10 Docker image; it never starts the
Caddy service.

Run migrations independently with:

```bash
DATABASE_URL=postgres://mmdash:mmdash@localhost:5432/mmdash?sslmode=disable pnpm migrate
```

## Quality commands

| Command                                       | Purpose                                                        |
| --------------------------------------------- | -------------------------------------------------------------- |
| `pnpm lint`                                   | TypeScript, Go formatting, and Python lint                     |
| `pnpm test`                                   | TypeScript, Go, and Python tests                               |
| `pnpm test:core`                              | Go Core/backend tests only                                     |
| `pnpm test:python`                            | Python Worker tests (30-second test, 120-second suite timeout) |
| `pnpm build`                                  | All three language builds                                      |
| `pnpm format`                                 | Format supported source files                                  |
| `pnpm api:check`                              | Check OpenAPI operations against the API catalog               |
| `pnpm contracts:generate`                     | Regenerate TypeScript and Go contract outputs                  |
| `pnpm contracts:check`                        | Validate schemas, mocks, generation, compatibility             |
| `pnpm caddy:check`                            | Validate ingress invariants and Caddyfile syntax               |
| `pnpm commit:check -- "feat(scope): summary"` | Validate a commit subject                                      |

`pnpm test:core` runs only `go test ./backend/...`; it does not start the
TypeScript, Box/CLI Go, or Python test runners. `pnpm test:python` runs the
maintained Worker suite from `workers/mmdash-worker`. The normal `pnpm test`
continues to run TypeScript, all maintained Go packages, and the full Python
Worker suite.

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
- [Stage 4 Progress](progress.md)

- [Stage 7 Model](model.md)
- [Stage 8 Experiment](experiment.md)
- [Stage 8 Box Gateway and Sandbox](box.md)
- [Stage 9 Article](article.md)

- [Stage 5 Agent sessions](agent.md)
