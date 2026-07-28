# mmdash Worker

The Python Worker executes asynchronous jobs exclusively through the Core Job
API. It never connects to PostgreSQL or updates business tables directly.

Stage 3.11 provides:

- API-token authentication, Worker heartbeat, and capability advertisement;
- atomic job claim, lease renewal, cancellation observation, logs, and result
  submission;
- a typed handler registry and a `system.test` baseline handler;
- `--once` for deterministic development and smoke checks.

Run one poll:

```powershell
$env:MMDASH_WORKER_API_TOKEN = "<issued-api-token>"
$env:MMDASH_CORE_URL = "http://localhost:8080"
uv run --package mmdash-worker mmdash-worker --once
```

Notion, LaTeX, preview, analysis, and other product handlers are intentionally
registered by later modules.
