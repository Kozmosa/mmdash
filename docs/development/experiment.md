# Stage 8 Experiment

Experiment is the Core-owned record for one frozen execution request. The
create boundary validates a full immutable Commit SHA, a supported fixed
entrypoint (`python`, `python3`, `node`, `go`, or `binary`), JSON parameters,
string environment values, runtime, and resource limits. Once created, the
request is not edited; `run` creates the PostgreSQL-backed Box task and freezes
the `run_spec` in the same transaction as the Experiment queue transition.

## Lifecycle

```text
created -> queued -> preparing -> running
                         |          |
                         +----------+--> succeeded / failed / canceled / timed_out
                                                -> archived
```

The state machine is monotonic and repeated `run`, status, and result callbacks
are idempotent. Core remains the only writer of Experiment, Box task, Audit,
Outbox, and Data Hub state. PostgreSQL is the queue and claims use
`FOR UPDATE SKIP LOCKED`.

## Result boundary

Successful Box tasks upload exactly one `artifact.zip`. Core stages the stream
to a bounded temporary file, validates `manifest.json`, rejects traversal,
symlinks, duplicate files, hash/size mismatches, and oversized zip expansion,
then hands the bytes to the existing Artifact multipart/version boundary.
The public result pointer is therefore an Artifact pointer; result bytes are
never copied into Experiment tables.

The Data Hub projects `experiment`, `experiment_run`, and `result_bundle` cards
from the same lifecycle events. `data.read` delegates back to the authoritative
Experiment, Box task, or Artifact adapter after permission checks.

## Product paths

- Web: `/projects/{projectId}/experiments` provides creation, run/cancel,
  status, bounded live log polling, and Box status.
- Web BFF: `/api/projects/{projectId}/experiments*` and `/box` proxy only the
  generated Core client and signed browser session.
- MCP: `experiment.create`, `experiment.run`, `experiment.status`, and
  `result.get` use the existing Project-scoped audit and authorization path.
- CLI: `experiment list`, `experiment create`, `experiment run`, and
  `experiment status` are compile-time registered Go commands.
- Worker: `experiment.result.summarize` and `experiment.result.compare` are
  bounded, deterministic Job handlers; they do not access business tables.

## Verification

Focused checks:

```bash
pnpm contracts:generate
pnpm contracts:check
pnpm api:check
go test ./backend/internal/experiment ./backend/internal/boxcontrol ./backend/internal/artifact ./backend/internal/datahub
pnpm --filter @mmdash/web-bff test
pnpm --filter @mmdash/mcp-gateway test
```
