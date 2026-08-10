# Worker development

The Python Worker lives in `workers/mmdash-worker`. It polls Core over HTTP and
has no PostgreSQL dependency.

## Development checks

```powershell
$env:UV_CACHE_DIR = "I:\Project\mmdash\.uv-cache"
uv run --package mmdash-worker pytest workers/mmdash-worker/tests
uv run --package mmdash-worker ruff check workers/mmdash-worker
```

## Run one job

1. Log in to Core and issue an API token with `POST /v1/auth/tokens`.
2. Set `MMDASH_WORKER_API_TOKEN` to the one-time token secret.
3. Start one deterministic poll:

```powershell
$env:MMDASH_CORE_URL = "http://localhost:8080"
$env:MMDASH_WORKER_ID = "local-worker"
uv run --package mmdash-worker mmdash-worker --once
```

Optional runtime variables:

| Variable                      | Default                 | Meaning                   |
| ----------------------------- | ----------------------- | ------------------------- |
| `MMDASH_CORE_URL`             | `http://localhost:8080` | Core API base URL         |
| `MMDASH_WORKER_ID`            | host name plus PID      | Stable process identity   |
| `MMDASH_WORKER_LEASE_SECONDS` | `60`                    | Lease duration, 10–900    |
| `MMDASH_WORKER_POLL_SECONDS`  | `2`                     | Delay after an empty poll |
| `MMDASH_PROGRESS_EVALUATOR_MODE` | `core_agent`          | `core_agent` or deterministic `mock` Progress evaluation |

Register handlers through `HandlerRegistry`. Job type names are stable dotted
identifiers. A handler receives a `HandlerContext` and JSON-object payload and
must return a JSON-object result. Raise `HandlerError` to control the safe error
code and retry policy.

Do not import a PostgreSQL driver into the Worker or mutate domain tables from
a handler. Domain results return through Core Job API operations.

## Progress evaluation handler

`progress.evaluate` is registered through the normal `HandlerRegistry`. In
`core_agent` mode the handler calls the leased Core execute route; Core owns
the existing Hermes Session/Run interaction and returns local Agent provenance
plus the raw JSON output. The Worker parses and validates the exact bounded
Stage 6 schema before Job completion. It does not call Hermes directly and does
not invent a second provider contract.

In `mock` mode the handler reads the immutable evaluation input from Core and
deterministically derives a stage, summary, task lists, and blocked-task risks.
Use this mode for local/Docker acceptance when no real Hermes is available.
Both paths return only a JSON-object Job result; Progress applies Tasks,
Proposals, risks, tracker state, Audit, and Outbox after Core revalidates it.
