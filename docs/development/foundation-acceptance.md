# Stage 3.15 technical-foundation acceptance

Search terms: stage 3.15, smoke, Docker Compose, login,
project creation, Worker, Job, Outbox, Data Hub, Audit, request ID, CI.

Stage 3.15 proves that the technical foundation is integrated. It does not
claim that later product modules are implemented.

## Development checks

Run without the full Docker stack while implementing a 3.x node:

```powershell
node scripts/check-contracts.mjs
node scripts/check-contract-compat.mjs
node scripts/check-api-docs.mjs
Set-Location backend
go test ./...
go vet ./...
```

## Start dependencies for development

PostgreSQL and MinIO may be started before the full stack:

```powershell
docker compose -f deploy/compose/compose.yaml up -d postgres minio
docker compose -f deploy/compose/compose.yaml run --rm migrate
```

Redis is not part of stage 0: Jobs use PostgreSQL and
`FOR UPDATE SKIP LOCKED`. If a later module introduces Redis, start only its
declared Compose service rather than adding an implicit local dependency.

Run Core, BFF, Web, and MCP Gateway in separate terminals using their guides
under `docs/development/`. Verify the Worker shell:

```powershell
$env:UV_CACHE_DIR = ".\.uv-cache"
uv run --offline --package mmdash-worker mmdash-worker --status
```

## Full Docker acceptance

Only run this after the 3.15 implementation commit:

```powershell
docker compose -f deploy/compose/compose.yaml up -d --build
docker compose -f deploy/compose/compose.yaml build worker
$env:MMDASH_SMOKE_WORKER_MODE = "docker"
pnpm smoke
```

The Worker is a Compose profile service because it needs a Core-issued API
token. Smoke creates that token and invokes a one-shot Worker container. For a
long-running Worker:

```powershell
$env:MMDASH_WORKER_API_TOKEN = "<Core-issued API token>"
docker compose -f deploy/compose/compose.yaml --profile worker up -d worker
```

Inspect status and logs:

```powershell
docker compose -f deploy/compose/compose.yaml ps
docker compose -f deploy/compose/compose.yaml logs --tail 100 core web-bff web mcp-gateway
```

Shut down without deleting persisted volumes:

```powershell
docker compose -f deploy/compose/compose.yaml down
```

## Evidence matrix

| Requirement                   | Runtime evidence in `pnpm smoke`                                                                    |
| ----------------------------- | --------------------------------------------------------------------------------------------------- |
| Services start                | Web, Core, BFF, MCP health plus CLI/Worker process checks                                           |
| Login and empty project       | Browser login and BFF project creation                                                              |
| Web → BFF → Core → PostgreSQL | `/api/example`, project, and home aggregation                                                       |
| MCP Gateway and native CLI    | Device login, Project selection, discovery, and all four Stage 3 tools over stdio → Streamable HTTP |
| Worker claims test Job        | Core-issued API token plus `system.test` one-shot Worker                                            |
| Outbox delivery               | `system.test.emitted` reaches successful delivery                                                   |
| Data Hub object               | `project.created` projection plus `data.read` routing                                               |
| Request ID and Audit          | BFF-preserved ID queried from append-only Core Audit                                                |
| CI                            | Node 24 workflow; parallel `ts`/`go`/`python`/`contracts` jobs running the same package scripts     |

Smoke creates isolated records with a timestamped name and idempotency key. It
polls asynchronous state once per second and stops after 30 attempts.
