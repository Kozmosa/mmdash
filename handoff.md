# mmdash v0.1 Stage 2 Artifact handoff

- Updated: 2026-07-31
- Branch: `codex/stage-2-artifact`
- Base: `origin/main@0f54510`

## Status

Stage 2 Artifact is implemented and fully validated on the dedicated
`stage-2-artifact` worktree. Go Core remains the only owner of authoritative
Artifact, Version, upload, Blob, trash, RBAC, Audit, Outbox, and Project source
reference state. Web BFF, Web, Worker, MCP Gateway, and Data Hub do not write
Artifact business tables directly.

Checkpoint commits:

| Checkpoint | Commit      | Delivery                                                                                                  |
| ---------- | ----------- | --------------------------------------------------------------------------------------------------------- |
| 1          | `0da28fe`   | Frozen design, OpenAPI, events, errors, ADR, generated clients, and migration reservation                 |
| 2          | `a5d1bdf`   | Multipart `BlobStore`, `000013_artifact`, Local/MinIO/S3-compatible adapters, and system configuration    |
| 3          | `11f9b21`   | Core Artifact, immutable Version, upload, download, trash, purge, RBAC, Audit, and metrics                |
| 4          | `948e8f6`   | Bounded Worker image/PDF/CSV/JSON/text previews, thumbnails, registry, and semantic interface placeholder |
| 5          | `2da9678`   | Outbox, Data Hub, MCP `data.list/read`, and validated Project source references                           |
| 6          | `b302734`   | Web BFF and Project file library, uploader, filters, detail, versions, preview, selector, and trash       |
| 7          | `fb63feb`   | Real-service acceptance, browser screenshots, download hardening, documentation, and handoff              |
| Follow-up  | this commit | Per-Version preview selection plus explicit PDF first-page, page-count, and bounded-text rendering        |

## Delivered behavior

- Stable Project-scoped `Artifact` identities and immutable retained Versions.
- Fixed system `kind` and `source` enums with normalized user `tags[]`.
- Project-local SHA-256 plus size deduplication with real provider `Stat`.
- Local, MinIO, and S3-compatible multipart adapters behind one Core contract.
- Dynamic MiB-aligned part sizing within S3 limits and on-demand short-lived
  part grants.
- Bounded browser concurrency, single-part retry, pause/resume, refresh-safe
  recovery, cancellation, provider list/complete/abort, expiry cleanup, and
  idempotent confirmation.
- Full completed-object size and streaming SHA-256 verification before a
  Version becomes available.
- Direct MinIO/S3 presigned part PUT; Local signed BFF/Core streaming transfer
  without buffering a complete part or file.
- Indefinite retention of successful originals and every historical Version.
  Normal deletion moves an Artifact to recoverable trash. Permanent purge
  removes object bytes only when no retained Version or preview references the
  Project-local Blob.
- Worker previews for image, PDF, CSV, JSON, and text, including thumbnails and
  bounded structural summaries. `SemanticDescriptionGenerator` is an unused
  interface placeholder only.
- `artifact.created`, `artifact.available`, and `artifact.deleted` in the same
  transaction as authoritative state and Audit.
- Data Hub `artifact` and `attachment_registry_entry` projections, generic MCP
  `data.list/read`, and current Project RBAC on controlled reads.
- Project `source_artifact_ids[]` validation, two-step Project creation flow,
  source selector, and Project-home aggregate.
- Project file library with multipart uploader, kind/source/status/tag filters,
  metadata detail, independently selectable preview for every immutable
  Version, explicit image/PDF rendering, controlled download, selector, and
  trash.
- Direct object-storage downloads sign attachment disposition and MIME response
  headers. Preview URLs remain inline.

## Migrations and configuration

Stage 2 adds:

- `000013_artifact`
- `000014_artifact_preview_transfer`

Both the preserved development database and a disposable fresh database were
validated. The fresh database applied all 14 migrations in order through
`000014_artifact_preview_transfer`, exposed both Artifact and Registry tables,
and was deleted after verification.

Object-storage endpoint, bucket, credentials, region, upload limit, part size,
signed URL lifetime, session lifetime, staging cleanup, preview limits, and
Local root are system environment variables documented in
[`docs/development/artifact.md`](docs/development/artifact.md). None are Project
settings.

## Final verification

The following passed on 2026-07-31:

- Real PostgreSQL + Local + MinIO + Core HTTP Artifact suite:
  `go test ./internal/artifact -count=1 -v`; every opt-in integration test ran,
  with no skips.
- Real multipart cases: interruption/recovery, out-of-order upload, missing
  manifest, repeated confirmation, cancellation and repeat abort, expiry,
  SHA-256 mismatch, unauthorized signing, Local streaming, MinIO presigning,
  and provider completion.
- `pnpm smoke` with Docker Worker and Docker Local Repo:
  `status: passed`.
- `pnpm smoke:artifact-preview`: real MinIO upload and one-shot Docker Worker
  produced `image` and `thumbnail`.
- A second real MinIO multipart upload and one-shot Docker Worker produced a
  two-page PDF preview, a valid first-page PNG, and bounded extracted text from
  both pages.
- `pnpm check`: exit 0.
  - script tests: 9/9;
  - Web: 56/56;
  - Core client: 6/6;
  - CLI shell: 7/7;
  - validation: 1/1;
  - MCP Gateway: 11/11;
  - Web BFF: 28/28;
  - all Go packages passed, including the real temporary-Git Repo suite;
  - Python Worker: 25/25, no skips;
  - all TypeScript, Go, and Python builds passed;
  - 2 OpenAPI documents and shared schemas passed freshness and compatibility;
  - API catalog covers 180 operations;
  - Caddy configuration is valid.
- Compose required services are healthy: PostgreSQL, MinIO, Core, Web BFF,
  Web, and MCP Gateway. Migration and MinIO initialization containers exited 0. Core readiness reports PostgreSQL, object storage, Git, and Repo storage
  as `ready`.
- Artifact metrics are exposed with bounded labels. Recent required-service
  logs contain no panic, service fatal/error, credentials, tokens, or
  Authorization values. Two PostgreSQL error lines were caused by acceptance
  operator typos, diagnosed immediately, and followed by successful checks.

The persistent `worker` Compose profile intentionally has no Project-scoped API
token and exits 2. Acceptance issued real Core tokens and ran one-shot Worker
containers successfully; this is not a product failure.

## Real browser acceptance

Real Chrome exercised Project creation, source-file setup, multipart upload,
filters, tags, metadata, preview, thumbnail, download, new Version upload,
historical Version preview selection and restore, a real two-page PDF upload
and preview, source binding, Project home, trash, permanent purge controls, and
trash restore with no console errors.

Observed results:

- decoded thumbnail: 512 x 356;
- image structural summary: PNG, RGB, 1440 x 1000;
- controlled browser download: `repo-browser.png`, 63,706 bytes, complete and
  safe;
- immutable history after restore: v1 original, v2 new upload, v3 restored
  copy;
- every v1/v2/v3 row exposes an independent preview action. Real Chrome
  switched from current v3 to historical v1 and rendered the preview belonging
  to v1;
- real PDF preview: 2 pages, 362 x 512 decoded first-page thumbnail, 5,805-byte
  signed PNG, and bounded text from both pages;
- trash retained v1/v2/v3 and exposed restore plus confirmation-gated
  permanent purge.

Reviewed screenshots:

- [`artifact-uploader.png`](docs/screenshots/artifact-uploader.png)
- [`artifact-library.png`](docs/screenshots/artifact-library.png)
- [`artifact-detail-preview.png`](docs/screenshots/artifact-detail-preview.png)
- [`artifact-versions.png`](docs/screenshots/artifact-versions.png)
- [`artifact-pdf-preview.png`](docs/screenshots/artifact-pdf-preview.png)
- [`artifact-trash.png`](docs/screenshots/artifact-trash.png)
- [`artifact-project-home.png`](docs/screenshots/artifact-project-home.png)

## Boundaries and known follow-ups

Stage 2 intentionally does not implement:

- Artifact CLI commands, CLI login, or CLI Project binding; those begin in
  Stage 3 using the reusable Core contracts.
- Cross-Project or account-wide file libraries.
- A storage administration panel or Project-level storage settings.
- Browser file editing.
- LLM or multimodal semantic description and automatic
  `recommended_usage`; Article owns that future behavior.
- Article, Model, Experiment, Agent, Progress, or Box product modules.

Issue #22 remains the separate Worker test-reliability tracker. Issue #19
remains the separate Go toolchain alignment decision. No Stage 2 workaround
changes either issue.

The single recommended next product stage is Stage 3. Start from the
authoritative versioned design, reuse Artifact Core contracts, and keep CLI
authentication and Project binding outside Stage 2.

## Revalidation

Preserve Docker volumes:

```powershell
$env:REPO_LOCAL_ALLOWED_ROOTS = "/tmp"
docker-compose -f deploy/compose/compose.yaml up -d --build

$env:MMDASH_SMOKE_WORKER_MODE = "docker"
$env:MMDASH_SMOKE_REPO_MODE = "docker"
pnpm smoke
pnpm smoke:artifact-preview
pnpm check
```

Ordinary shutdown may use `docker-compose ... down`. Never use `down -v`
unless deletion of PostgreSQL and MinIO test data is explicitly approved.
