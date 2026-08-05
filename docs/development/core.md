# Go Core Server foundation

`backend` is the modular monolith and the only owner of authoritative business
state. Stage 3.7 establishes platform boundaries. Stage 3.8 adds the first
product domains: Auth and collaborative Projects.

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

| Variable                        | Default                       | Purpose                                                        |
| ------------------------------- | ----------------------------- | -------------------------------------------------------------- |
| `CORE_ADDR`                     | `:8080`                       | HTTP listen address                                            |
| `CORE_OPENAPI_PATH`             | `contracts/openapi/core.yaml` | Contract served at `/openapi.yaml`                             |
| `CORE_STARTUP_TIMEOUT`          | `15s`                         | Dependency initialization deadline                             |
| `CORE_SHUTDOWN_TIMEOUT`         | `10s`                         | Graceful HTTP drain deadline                                   |
| `MMDASH_VERSION`                | `0.1.0`                       | Version in liveness and build-info metrics                     |
| `DATABASE_URL`                  | required                      | PostgreSQL DSN                                                 |
| `DATABASE_MAX_OPEN_CONNS`       | `20`                          | Pool upper bound                                               |
| `DATABASE_MAX_IDLE_CONNS`       | `5`                           | Idle pool bound                                                |
| `DATABASE_CONN_MAX_IDLE_TIME`   | `5m`                          | Idle connection lifetime                                       |
| `DATABASE_CONN_MAX_LIFETIME`    | `30m`                         | Absolute connection lifetime                                   |
| `OBJECT_STORAGE_ENDPOINT`       | required                      | MinIO/S3-compatible HTTP(S) origin                             |
| `OBJECT_STORAGE_ACCESS_KEY`     | required                      | Object storage access identity                                 |
| `OBJECT_STORAGE_SECRET_KEY`     | required                      | Object storage secret                                          |
| `OBJECT_STORAGE_BUCKET`         | `mmdash`                      | Authoritative Artifact bucket                                  |
| `AUTH_BOOTSTRAP_EMAIL`          | `admin@mmdash.local`          | First local account email                                      |
| `AUTH_BOOTSTRAP_DISPLAY_NAME`   | `Local Admin`                 | First local account display name                               |
| `AUTH_BOOTSTRAP_PASSWORD`       | `mmdash-local-admin`          | First local account password; change outside local development |
| `AUTH_JWT_SECRET`               | local-only fallback           | HMAC key for browser session JWTs                              |
| `AUTH_ACCESS_TOKEN_TTL`         | `24h`                         | JWT access-token lifetime                                      |
| `AUTH_SESSION_TTL`              | `720h`                        | Maximum refreshable browser/CLI session lifetime               |
| `AUTH_DEVICE_AUTHORIZATION_TTL` | `10m`                         | CLI device-code lifetime                                       |
| `AUTH_DEVICE_POLL_INTERVAL`     | `5s`                          | Minimum advertised CLI polling interval                        |
| `SETTINGS_ENCRYPTION_KEY`       | local-only fallback           | Stable key material for AES-256-GCM setting secrets            |
| `OUTBOX_POLL_INTERVAL`          | `500ms`                       | Idle event Processor delay                                     |
| `OUTBOX_EVENT_LEASE`            | `30s`                         | Outbox publication lease                                       |
| `OUTBOX_DELIVERY_LEASE`         | `30s`                         | Per-consumer processing lease                                  |
| `OUTBOX_RETRY_DELAY`            | `2s`                          | Baseline publication and consumer retry delay                  |
| `PROGRESS_REMINDER_POLL_INTERVAL` | `1s`                        | Idle due Reminder scan interval                                |
| `PROGRESS_REMINDER_BATCH_SIZE`    | `20`                        | Maximum Reminders claimed per scan                             |
| `PROGRESS_REMINDER_LEASE`         | `30s`                       | Recoverable due Reminder processing lease                      |
| `PROGRESS_REMINDER_RETRY_DELAY`   | `2s`                        | Retry delay after a due event transaction failure              |

Configuration validates all values before opening listeners. JSON logging
recursively redacts credential, authorization, secret, token, password,
cookie, access/API/private-key, passphrase, DSN, database-URL, and
connection-string fields.

## HTTP and request context

- `/health/live` reports process liveness.
- `/health/ready` checks PostgreSQL and object storage with a deadline.
- `/openapi.yaml` serves the same contract used to generate Core clients.
- `/metrics` serves bounded-label Prometheus metrics and build version.
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

`outbox.Writer` accepts that same `transaction.Tx`, validates the stable
envelope, and fills the event ID, UTC occurrence time, and schema version.
Migration `000002_system_outbox.up.sql` creates the Outbox; migration
`000006_event_delivery.up.sql` adds publication leases, per-consumer
deliveries, append-only failures, consumption idempotency, and replay records.

The background `outbox.Processor` publishes events through the in-process
`eventbus.Bus`. Consumer names are stable idempotency boundaries. Use
[`docs/api/events.md`](../api/events.md) before registering a consumer or
adding operator replay support.

`cmd/migrate` takes a PostgreSQL advisory lock, applies sorted `*.up.sql` files
transactionally, and records each version in `system_schema_migrations`.
Domain migrations must only change their own table prefix and semantics.

## Auth and collaborative projects

Auth owns users, revocable browser sessions, and hashed API, Agent, and Box
tokens. Project owns project records, team membership, roles, and project
authorization. Project-scoped Agent and Box tokens pass through both token
scope and role permission checks.

Project creation inserts the project, first owner, and `project.created`
outbox event atomically. Project updates and membership changes follow the same
transactional outbox rule. See [Auth, Project, and RBAC](../api/auth-projects.md)
for the route and permission lookup.

Settings types are registered in code by their owning module. Settings stores
public values separately from AES-GCM envelopes, exposes only redacted
projections, and decrypts only for trusted in-process adapters. See
[Settings and secret management](../api/settings.md) before adding a module
configuration or connection test.

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

| Package                           | Boundary                                                   |
| --------------------------------- | ---------------------------------------------------------- |
| `config`                          | Environment loading and validation                         |
| `database`                        | PostgreSQL pool and readiness                              |
| `transaction`                     | Commit/rollback orchestration                              |
| `migration`                       | Advisory-locked schema changes                             |
| `identity`, `clock`, `pagination` | Stable shared primitives                                   |
| `apperror`, `httpx`, `requestctx` | HTTP contract, generated validation, and request context   |
| `contract/generated`              | Generated handler DTOs, validation, and Event Envelope     |
| `logging`                         | Redacted JSON events                                       |
| `objectstorage`                   | MinIO configuration, bucket identity, readiness            |
| `module`                          | Deterministic explicit registration                        |
| `eventbus`                        | Named in-process consumer registration and dispatch        |
| `outbox`                          | Transactional writes, durable delivery, retries, replay    |
| `settings`                        | Typed configuration, encrypted secrets, permissions, tests |
| `health`, `coreapp`, `server`     | HTTP composition and process lifecycle                     |
