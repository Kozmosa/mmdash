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

Use repeatable `--job-type` filters for a dedicated Worker that must advertise
and claim only selected registered handlers. The foundation smoke uses this to
keep its one-shot Worker scoped to `system.test`, regardless of other Project
Jobs already queued by product initialization:

```powershell
uv run --package mmdash-worker mmdash-worker --job-type system.test --once
```

Article's pinned Pandoc/TeX toolchain is installed only in the Worker image;
it does not install packages into the host Python or operating-system
environment. When Docker Hub is unavailable, pull the same base image through
1ms and use optional signed Debian mirrors for the image build:

```powershell
docker pull docker.1ms.run/library/python:3.12.11-slim-bookworm
docker tag docker.1ms.run/library/python:3.12.11-slim-bookworm python:3.12.11-slim-bookworm
docker build -f workers/mmdash-worker/Dockerfile -t mmdash-worker --build-arg DEBIAN_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/debian --build-arg DEBIAN_SECURITY_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/debian-security .
pnpm smoke:article-worker
```

Preview limits are system environment configuration (`MMDASH_PREVIEW_*` in
`.env.example`). Stage 2 intentionally provides only the
`SemanticDescriptionGenerator` interface: LLM/multimodal descriptions and
automatic recommended usage remain deferred to Article.
