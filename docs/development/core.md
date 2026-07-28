# Go Core Server foundation

`backend` is the modular monolith and the only owner of authoritative business
state. Stage 3.7 establishes platform boundaries; product domain modules are
introduced in later stages.

## Run and verify

From the repository root:

```bash
go test ./backend/...
go build ./backend/...
go run ./backend/cmd/migrate
go run ./backend/cmd/core-server
```

The migration and server commands require PostgreSQL. Core startup also checks
MinIO readiness and reads the canonical OpenAPI file before accepting traffic.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `CORE_ADDR` | `:8080` | HTTP listen address |
| `CORE_OPENAPI_PATH` | `contracts/openapi/core.yaml` | Contract served at `/openapi.yaml` |
| `CORE_STARTUP_TIMEOUT` | `15s` | Dependency initialization deadline |
| `CORE_SHUTDOWN_TIMEOUT` | `10s` | Graceful HTTP drain deadline |
| `DATABASE_URL` | required | PostgreSQL DSN |
| `DATABASE_MAX_OPEN_CONNS` | `20` | Pool upper bound |
| `DATABASE_MAX_IDLE_CONNS` | `5` | Idle pool bound |
| `DATABASE_CONN_MAX_IDLE_TIME` | `5m` | Idle connection lifetime |
| `DATABASE_CONN_MAX_LIFETIME` | `30m` | Absolute connection lifetime |
| `OBJECT_STORAGE_ENDPOINT` | required | MinIO/S3-compatible HTTP(S) origin |
| `OBJECT_STORAGE_ACCESS_KEY` | required | Object storage access identity |
| `OBJECT_STORAGE_SECRET_KEY` | required | Object storage secret |
| `OBJECT_STORAGE_BUCKET` | `mmdash` | Authoritative Artifact bucket |

Configuration validates all values before opening listeners. JSON logging
redacts fields whose names contain token, secret, password, credential, or
authorization.

## HTTP and request context

- `/health/live` reports process liveness.
- `/health/ready` checks PostgreSQL and object storage with a deadline.
- `/openapi.yaml` serves the same contract used to generate Core clients.
- Every response carries `X-Request-ID`; valid inbound IDs are preserved.
- `X-Mmdash-User-ID` and `X-Mmdash-Project-ID` are normalized into request
  context for trusted gateways.
- Errors use stable `code`, safe `message`, optional `details`, and
  `request_id`. Internal causes are never serialized.
- Access logs are JSON and preserve SSE flushing, WebSocket hijacking, and
  efficient file streaming.

The HTTP lifecycle drains on SIGINT/SIGTERM. Read and header timeouts are
bounded; write timeout stays disabled because Core will host long-lived
streams.

## Transactions, migrations, and outbox

Use `transaction.Manager.Within` for business mutations:

```go
err := transactions.Within(ctx, nil, func(tx transaction.Tx) error {
    // write module-owned state
    // write outbox event with the same tx
    return nil
})
```

`outbox.Writer` accepts that same `transaction.Tx` and fills the event ID,
UTC occurrence time, and schema version. Migration
`000002_system_outbox.up.sql` creates the delivery table and pending-event
index.

`cmd/migrate` takes a PostgreSQL advisory lock, applies sorted `*.up.sql` files
transactionally, and records each version in `system_schema_migrations`.
Domain migrations must only change their own table prefix and semantics.

## Add a domain module

Generate the reviewed cross-layer starter:

```bash
pnpm scaffold:module -- sample
```

The Go module must implement:

```go
type Module interface {
    Name() string
    RegisterRoutes(*http.ServeMux)
}
```

Register it explicitly in `cmd/core-server`. Dependencies should be
application services or narrow read interfaces; never pass another module's
repository or tables. Add the Core operation to `contracts/openapi/core.yaml`,
regenerate `packages/core-client`, update `docs/api/endpoints.md`, and test the
module plus its process boundary.

## Platform packages

| Package | Boundary |
| --- | --- |
| `config` | Environment loading and validation |
| `database` | PostgreSQL pool and readiness |
| `transaction` | Commit/rollback orchestration |
| `migration` | Advisory-locked schema changes |
| `identity`, `clock`, `pagination` | Stable shared primitives |
| `apperror`, `httpx`, `requestctx` | HTTP contract and request context |
| `logging` | Redacted JSON events |
| `objectstorage` | MinIO configuration, bucket identity, readiness |
| `module` | Deterministic explicit registration |
| `outbox` | Same-transaction event envelope writes |
| `health`, `coreapp`, `server` | HTTP composition and process lifecycle |
