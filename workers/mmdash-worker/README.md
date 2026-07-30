# mmdash Worker

The Python Worker executes asynchronous jobs exclusively through the Core Job
API. It never connects to PostgreSQL or updates business tables directly.

The baseline runtime plus Stage 2 Artifact provides:

- API-token authentication, Worker heartbeat, and capability advertisement;
- atomic job claim, lease renewal, cancellation observation, logs, and result
  submission;
- a typed handler registry and a `system.test` baseline handler;
- an `artifact.preview` handler for bounded image, PDF, CSV, JSON, and text
  previews, thumbnails, and structural summaries;
- Job-bound, short-lived Core transfers for immutable input and generated
  output; no Worker database or object-storage credentials;
- `--once` for deterministic development and smoke checks.

Run one poll:

```powershell
$env:MMDASH_WORKER_API_TOKEN = "<issued-api-token>"
$env:MMDASH_CORE_URL = "http://localhost:8080"
uv run --package mmdash-worker mmdash-worker --once
```

Preview limits are system environment configuration (`MMDASH_PREVIEW_*` in
`.env.example`). Stage 2 intentionally provides only the
`SemanticDescriptionGenerator` interface: LLM/multimodal descriptions and
automatic recommended usage remain deferred to Article.
