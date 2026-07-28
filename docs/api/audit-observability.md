# Audit and observability

Search terms: Audit, observability, `request_id`, `user_id`, `project_id`,
JSON logs, Prometheus, metrics, service version, redaction, MCP audit, Box
audit, Job logs, error logs.

The canonical Audit API schemas live in
[`contracts/openapi/core.yaml`](../../contracts/openapi/core.yaml). Migration
`000008_audit_observability` creates the append-only ledger.

## API

| `operationId`               | Method | Path               | Purpose                       |
| --------------------------- | ------ | ------------------ | ----------------------------- |
| `audit.events.list`         | GET    | `/v1/audit/events` | Search immutable audit events |
| `audit.events.record`       | POST   | `/v1/audit/events` | Trusted service ingestion     |
| `observability.metrics.get` | GET    | `/metrics`         | Prometheus text exposition    |

Audit search accepts `project_id`, `actor_id`, `category`, `action`, `outcome`,
`source`, `request_id`, `cursor`, and `limit`. Results are ordered by
`occurred_at` and stable Audit ID using an opaque cursor.

System administrators may search globally. Project owners and maintainers may
search their project with `project.audit.read`. A project-scoped credential
cannot query another project.

## Immutable audit contract

Every record contains:

- stable `audit_id`, `occurred_at`, and database `recorded_at`;
- `request_id`, authenticated actor ID/kind, and optional project ID;
- lowercase category, dotted action, source, and outcome;
- optional resource, duration, safe error code, and structured metadata.

The database rejects `UPDATE` and `DELETE` through an append-only trigger.
Caller identity is never accepted from an ingestion body. Trusted external
ingestion requires a Core-issued API or Box token; Core derives the service
actor and enforces `project.audit.write`.

HTTP requests under `/v1/` automatically create
`http.request.completed` events. Authentication and Project authorization
replace forwarded identity fields with verified `user_id` and `project_id`
before the access log and audit record are written. Audit persistence failure
does not replace the already-produced HTTP response; it emits
`audit.record.failed` and increments a dedicated failure metric.

## Service integration

MCP Gateway always writes secret-free JSON audit logs. When
`MCP_CORE_AUDIT_TOKEN` contains a Core-issued API token, it additionally sends
`mcp.tool.called` records to Core using the same request and project IDs. The
MCP principal and logical session are preserved as delegated metadata.

Worker Job logs remain durable through `jobs.logs.list`/`jobs.logs.append`.
Future Box Gateway operations use the same `audit.events.record` contract with
source `box-gateway`; Box credentials are accepted but cannot alter the actor
Core derives from their token.

## Logging and redaction

Core logs one JSON object per line. HTTP completion records contain method,
safe path, status, duration, `request_id`, verified `user_id`, and authorized
`project_id`. Panics, Outbox processor failures, and audit persistence failures
use error-level structured events.

Redaction is recursive through nested objects and arrays. Keys containing
authorization, credential, password, passphrase, secret, token, cookie,
access/API/private-key, DSN, database-URL, or connection-string markers are
replaced by `[REDACTED]`. Request bodies, authorization headers, and query
values are not included in HTTP access logs.

## Metrics and version

`GET /metrics` exposes bounded-label Prometheus metrics:

- `mmdash_build_info{service,version}`;
- `mmdash_http_requests_total{method,status}`;
- HTTP request duration sum/count with the same bounded labels;
- `mmdash_audit_write_failures_total`.

Paths, users, projects, and request IDs are deliberately absent from metric
labels to avoid unbounded cardinality. `MMDASH_VERSION` configures Core,
Web BFF, and MCP Gateway liveness versions; Core also publishes it through
`mmdash_build_info`.
