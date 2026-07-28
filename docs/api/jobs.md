# Jobs and Worker protocol

Search terms: `jobs.create`, `jobs.claim`, `FOR UPDATE SKIP LOCKED`, lease,
heartbeat, retry, cancellation, timeout, idempotency key, handler registry,
job logs, result submission, `system.test`, Worker API token.

The Core `jobs` module owns all durable queue state. Python Worker processes use
only the HTTP operations documented here; they do not connect to PostgreSQL,
MinIO, or domain tables. The canonical schemas and response codes live in
[`contracts/openapi/core.yaml`](../../contracts/openapi/core.yaml).

## Operation index

| Caller           | `operationId`            | Purpose                                      |
| ---------------- | ------------------------ | -------------------------------------------- |
| Project client   | `jobs.create`            | Create or resolve an idempotent job          |
| Project client   | `jobs.get`               | Read authoritative state                     |
| Project client   | `jobs.cancel`            | Cancel queued work or signal running work    |
| Project client   | `jobs.logs.list`         | Read ordered, append-only logs               |
| Worker API token | `jobs.workers.heartbeat` | Advertise process liveness and handlers      |
| Worker API token | `jobs.claim`             | Atomically claim one eligible job            |
| Worker API token | `jobs.lease.renew`       | Extend a lease and observe cancellation      |
| Worker API token | `jobs.logs.append`       | Append a log under the active lease          |
| Worker API token | `jobs.complete`          | Submit a successful object result            |
| Worker API token | `jobs.fail`              | Submit a safe error and desired retry policy |

Project roles have these effective capabilities:

| Role                      | Read | Create | Cancel |
| ------------------------- | ---- | ------ | ------ |
| owner, maintainer, editor | yes  | yes    | yes    |
| agent                     | yes  | yes    | yes    |
| viewer, box               | yes  | no     | no     |

An API token owned by a system administrator can process jobs across projects.
Other API tokens can claim only jobs in projects where the token owner is a
member. Project-scoped tokens remain constrained by their project identity.

## Create and idempotency

`POST /v1/jobs` accepts:

```json
{
  "project_id": "00000000-0000-4000-8000-000000000001",
  "job_type": "system.test",
  "payload": { "message": "hello" },
  "idempotency_key": "smoke-2026-07-28",
  "priority": 0,
  "max_attempts": 3,
  "timeout_seconds": 900
}
```

The idempotency scope is `(project_id, job_type, idempotency_key)`. The first
request returns HTTP 201. A later request using the same non-empty key returns
the existing authoritative job with HTTP 200 and does not enqueue duplicate
work. Omitting the key creates a new job on each call.

`available_at` optionally schedules future work. Claim order is priority
descending, then availability time, creation time, and job ID.

## Claim, lock, and lease

`jobs.claim` requires an Auth-issued opaque API token:

```json
{
  "worker_id": "worker-host-1",
  "job_types": ["system.test"],
  "lease_seconds": 60
}
```

Core performs the selection inside a PostgreSQL transaction using
`FOR UPDATE SKIP LOCKED`, so concurrent Workers do not block each other or
claim the same row. A successful claim:

- changes `queued` to `running`;
- increments `attempts`;
- records `locked_by` and `lease_expires_at`;
- initializes the absolute `timeout_at` on the first attempt.

No available work is a successful poll result:

```json
{ "job": null }
```

The Worker renews through `jobs.lease.renew` before lease expiry. The returned
Job includes `cancel_requested_at`; handlers should stop cooperatively when it
is present. A Worker may append logs or submit a result only while it owns a
non-expired lease.

## Retry, cancellation, and timeout

Job states are:

`queued → running → succeeded | failed | cancelled | timed_out`

A retryable `jobs.fail` response returns the job to `queued` when
`attempts < max_attempts`, using `retry_delay_seconds` as the next
`available_at`. Otherwise the job becomes `failed`.

Cancellation and timeout take precedence over a late handler result:

- a queued cancellation becomes `cancelled` immediately;
- a running cancellation records `cancel_requested_at`;
- completion/failure after that signal resolves to `cancelled`;
- reaching `timeout_at` resolves to `timed_out`;
- an expired lease is retried while attempts remain, otherwise it becomes
  `failed` with `LEASE_EXPIRED`.

Core applies these transitions; Worker handlers do not update queue state
directly.

## Worker heartbeat and Handler registry

`jobs.workers.heartbeat` upserts a stable `worker_id`, runtime version,
registered job types, safe metadata, and `last_seen_at`. The Python registry
advertises its job types and passes the same list to `jobs.claim`, preventing a
Worker from claiming work it cannot dispatch.

Stage 3.11 registers only `system.test`. Notion, LaTeX, preview, analysis, and
other product handlers intentionally remain unregistered until their owning
modules are implemented.

## Logs and results

Logs are append-only and capture attempt, level, message, structured fields,
Worker ID, and timestamp. Supported levels are `debug`, `info`, `warning`, and
`error`.

Successful results must be JSON objects. Failures provide a stable code, safe
message, `retryable`, and optional retry delay. Queue status and important Job
events are written to `system_outbox` in the same transaction.

Stable Job-specific errors include:

| Code                        | HTTP | Meaning                                     |
| --------------------------- | ---- | ------------------------------------------- |
| `INVALID_JOB_REQUEST`       | 400  | Contract or queue validation failed         |
| `UNAUTHENTICATED`           | 401  | Credential missing, invalid, or revoked     |
| `WORKER_API_TOKEN_REQUIRED` | 403  | Session, Agent, or Box token used by Worker |
| `FORBIDDEN`                 | 403  | Project permission denied                   |
| `JOB_NOT_FOUND`             | 404  | Job does not exist                          |
| `JOB_LEASE_LOST`            | 409  | Lease expired or belongs to another Worker  |
