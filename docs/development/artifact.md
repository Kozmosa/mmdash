# Artifact development, deployment, and Core acceptance

Stage 2 Artifact stores stable Project-scoped file identities and immutable
Versions. Go Core is the only owner of Artifact, upload-session, Blob, trash,
authorization, and Audit state. Browser and Worker bytes cross only the
`BlobStore` boundary.

The contract is documented in the [Artifact API guide](../api/artifact.md) and
the storage decision in [ADR 0002](../adr/0002-artifact-multipart-storage.md).
Migration `000013_artifact` owns the domain tables. Migration
`000014_artifact_preview_transfer` adds the internal, Job-bound preview-output
transfer state without changing the frozen public Artifact schema. Both must
be applied through the normal migrator.

Migration `000035_agent_artifact` adds the reserved `agent` kind/source and an
optional `agent_instance_id` on upload sessions. Existing public upload
contracts and rows are unchanged. The Agent-specific initialization route is
private Core and is reached only from MCP Gateway with the inbound Agent Token.

## Storage backends

`ARTIFACT_STORAGE_BACKEND` selects one of three adapters:

- `local` stores objects below `ARTIFACT_LOCAL_STORAGE_ROOT`. Core issues
  short-lived signed `/v1/artifact-transfers/{token}` grants and streams PUT
  and GET bodies without buffering a complete part or file.
- `minio` uses the configured object-storage endpoint and path-style
  presigned multipart PUT/GET requests.
- `s3` uses the same multipart contract with virtual-host-style presigning
  against an S3-compatible endpoint.

The bucket, endpoint, region, credentials, upload limits, part size, and
lifetimes are system environment configuration. They are never Project
settings. Provider upload IDs, object keys, credentials, and signed transfer
tokens are internal; access logs and HTTP audit observations replace Local
transfer tokens with `/v1/artifact-transfers/{redacted}`.

| Variable                             | Compose default                | Purpose                                       |
| ------------------------------------ | ------------------------------ | --------------------------------------------- |
| `ARTIFACT_STORAGE_BACKEND`           | `minio`                        | `local`, `minio`, or `s3`                     |
| `ARTIFACT_LOCAL_STORAGE_ROOT`        | `/var/lib/mmdash/artifacts`    | Durable Local adapter root                    |
| `ARTIFACT_UPLOAD_MAX_BYTES`          | `10737418240`                  | Per-file system limit                         |
| `ARTIFACT_PREVIEW_OUTPUT_MAX_BYTES`  | `4194304`                      | Generated preview-output limit                |
| `ARTIFACT_MULTIPART_PART_BYTES`      | `16777216`                     | Preferred part size; Core increases as needed |
| `ARTIFACT_MULTIPART_URL_TTL`         | `15m`                          | Direct or Core-stream transfer lifetime       |
| `ARTIFACT_MULTIPART_SESSION_TTL`     | `24h`                          | Refresh-safe upload-session lifetime          |
| `ARTIFACT_STAGING_TTL`               | `24h`                          | Earliest automatic staging cleanup            |
| `ARTIFACT_STAGING_SWEEP_INTERVAL`    | `5m`                           | Expired-session reaper interval               |
| `OBJECT_STORAGE_ENDPOINT`            | `http://minio:9000` in Compose | MinIO/S3-compatible endpoint                  |
| `OBJECT_STORAGE_BUCKET`              | `mmdash`                       | Artifact object bucket                        |
| `OBJECT_STORAGE_REGION`              | `us-east-1`                    | Signing region                                |
| `OBJECT_STORAGE_ACCESS_KEY` / secret | local development values only  | Provider credentials                          |
| `CORE_INTERNAL_URL`                  | `http://core:8080` in Compose  | Worker-reachable signed Core transfer base    |

Compose mounts the Local root and MinIO data on named volumes. Preserve those
volumes during ordinary shutdown. Automatic cleanup aborts only expired,
unconfirmed staging uploads; it never deletes available originals or immutable
Versions.

## Core lifecycle

Initialization validates Project RBAC, normalizes metadata and an optional
Project-scoped folder assignment, computes an
S3-compatible dynamic multipart plan, and reuses a matching Project-local
SHA-256 plus size Blob only after a real object `Stat`. Part grants are signed
on demand. Upload-session reads reconcile completed parts with the provider so
pause, reconnect, and browser refresh do not create a second Version.

The target folder is written in the same transaction as the new stable
Artifact, so a confirmed upload cannot be left at the Project root merely
because a later folder request encountered a network failure. Confirmation
holds a database lease, compares the exact ordered part list with
provider state, completes multipart storage, verifies full object size and
streaming SHA-256, promotes to the Project content-addressed key, and makes the
Version available transactionally. A competing confirmation receives a
bounded conflict or the idempotent completed result. Crash recovery recognizes
provider-completed and already-promoted states.

Ordinary deletion moves an Artifact to trash. Restore preserves all Versions.
Permanent purge is owner/maintainer-only and deletes an object only after the
transaction proves no Version or retained preview references the Blob.

Git-backed internal registration accepts only a Project repository ID, full
commit SHA, and Repo-relative path. It reads through the Repo service's
immutable content API and records `source=git`; no public Artifact upload route
can forge that source.

## Worker preview lifecycle

Making an immutable Version available creates one `artifact.preview` Job and a
registry entry in the same Core transaction. The Worker claims the Job using
its existing Project-scoped API token, then requests short-lived input and
output capabilities from
`/v1/internal/artifact-preview-jobs/{jobId}/transfers`. The Job payload and
result contain only Project, Artifact, Version, preview target IDs, bounded
summary data, and provider ETags—never object keys, provider handles,
credentials, or signed URLs.

Worker input and generated thumbnail output always stream through signed Core
routes in 64 KiB chunks, including when the selected backend is MinIO or S3.
The output is a one-part provider multipart upload. Core checks the Job target,
expected size, provider part and ETag, streams full SHA-256 verification,
promotes the content-addressed object, and updates Job plus preview state in one
transaction. Local public thumbnail downloads use the same signed route;
MinIO/S3 public thumbnails use short-lived presigned GET.

The Worker limits image pixels, PDF pages and text pages, CSV rows and columns,
JSON/text bytes, summary bytes, thumbnail dimensions/bytes, and elapsed
processing time through `MMDASH_PREVIEW_*` system environment variables.
Invalid supported formats fail the Job safely; unsupported/binary or
over-limit inputs create a bounded `unsupported` projection without making the
original unavailable. The reaper aborts expired preview staging and reconciles
preview state with retry, cancellation, lease-timeout, and terminal Job state.

`SemanticDescriptionGenerator` is a protocol only. No implementation or call
site exists in Stage 2.

## Metrics and readiness

Core readiness checks PostgreSQL plus the selected Artifact backend. Artifact
metrics use bounded operation, outcome, and backend labels:

```text
mmdash_artifact_operations_total{operation,outcome,backend}
mmdash_artifact_operation_duration_seconds{operation,backend}
mmdash_artifact_uploads_active
```

Do not add Artifact IDs, Project IDs, filenames, MIME types, hashes, object
keys, or URLs as metric labels.

## Events, Data Hub, MCP, and Project references

Artifact persistence writes the three frozen lifecycle events in the same
transaction as authoritative state and Audit:

- `artifact.created` for the stable Artifact and first pending Version;
- `artifact.available` after upload verification, Project-local deduplication,
  historical Version restoration, or trash restoration;
- `artifact.deleted` when the Artifact enters recoverable trash.

The `datahub.projections` consumer reloads authoritative Artifact and registry
state, then idempotently upserts `artifact` and
`attachment_registry_entry`. Trashed objects remain as hidden tombstones and
reappear after restoration. Projection metadata contains no object key,
provider upload ID, credential, or signed URL.

Generic MCP `data.list/read` remains the Stage 2 read surface. The later Agent
integration adds exact-scope `artifact.read` for chat attachments and
`artifact.upload` for Agent-produced images and files.
`data.list` reads projections; `data.read` passes through current Project RBAC
to the Artifact reader and signs a short-lived download only for that call.
Project creation remains step one of the source-file flow. After upload, PATCH
`source_artifact_ids[]`; Project calls Artifact through a narrow validator that
requires every ID to be unique, same-Project, available, and not trashed. Data
Hub's Project-home `problem` section resolves those validated source files and
their current preview state.

`artifact.upload` carries metadata and multipart state only. It calls the
dedicated Agent initialization operation, requires `kind/source=agent`, and
returns direct object-storage PUT grants. Hermes computes SHA-256 and performs
the PUTs itself; Gateway/Core never buffer a complete part or file. Upload
continuation is bound to the creating Agent instance. A remote Hermes
deployment must be able to reach the configured object-storage endpoint;
Local/Core-proxy transfer mode is rejected by the Tool.

Agent uploads may provide the current Session and Run only as a pair. Core
validates their Agent/Project ownership and records an `output` relation to the
Run. Browser composer uploads remain `kind=attachment`, `source=user_upload`;
starting a Run records an `attachment` relation to its current immutable
Version. Transcript cards and previews are derived from those two relation
types. `artifact.read` delegates to the standard Artifact download grant and
never returns permanent object locations.

## Core verification

Run focused native checks with a migrated PostgreSQL database:

```bash
go test ./internal/artifact ./internal/audit ./internal/project \
  ./internal/platform/coreapp ./internal/platform/metrics
go test ./...
go vet ./...
go build ./...
pnpm smoke:artifact-preview
```

The real Artifact integration suite is opt-in:

- `MMDASH_TEST_DATABASE_URL` enables PostgreSQL plus Local multipart coverage.
- `MMDASH_TEST_MINIO_ENDPOINT`, access key, and secret key add real MinIO
  presigned multipart coverage.
- `MMDASH_TEST_CORE_URL` and `MMDASH_TEST_ADMIN_PASSWORD` run the public Core
  HTTP lifecycle against a healthy Compose Core.
- `pnpm smoke:artifact-preview` performs a real MinIO presigned PUT, refresh
  recovery, idempotent confirmation, container Worker preview, Core-streamed
  thumbnail output, and signed thumbnail download; it cleans up its Artifact
  and credential afterward.

Acceptance must cover out-of-order and missing parts, refresh recovery,
provider ETag mismatch, repeated and concurrent confirmation, cancellation,
expiry, full-size and SHA-256 mismatch, cross-role denial, version retention,
trash restore, shared-Blob purge, and Local signed streaming. Inspect Core logs
afterward for errors and secret-bearing values.

## Web, BFF, and browser acceptance

Web BFF proxies Artifact JSON and streaming routes without buffering complete
parts or files. It rewrites only signed Core-local transfer URLs; MinIO/S3
presigned URLs remain direct. The Project-scoped Web library provides:

- bounded multipart upload with incremental SHA-256, retry, pause, resume,
  refresh recovery, and cancel;
- kind, source, status, and exact-tag filters;
- editable display metadata, immutable Version history, historical restore,
  secure preview, thumbnail, and controlled download;
- source-file selection for `source_artifact_ids[]`, Project creation
  hand-off, and Project-home rendering;
- a distinct trash view with restore and confirmation-gated permanent purge.

Direct object-storage download grants sign `response-content-disposition` and
`response-content-type`, so the browser downloads the immutable filename
instead of rendering images inline. Preview grants intentionally retain inline
provider behavior.

Stage 2 acceptance on 2026-07-31 used real Chrome, PostgreSQL, MinIO, Core,
Web BFF, Web, MCP Gateway, and one-shot Docker Workers. It verified real image
and PDF multipart uploads, a 512 x 356 image thumbnail, a 362 x 512 PDF
first-page thumbnail, image structure summary, a two-page PDF page count and
bounded two-page text extraction, a 63,706-byte browser download, v2 upload,
v1-to-v3 immutable restore, source binding, filters, trash retention of
v1/v2/v3, and trash restore with no console errors. Preview state is
Version-scoped and asynchronous. Every available Version row has an independent
preview action; real Chrome switched from current v3 to historical v1 and
rendered v1's own preview. The PDF detail rendered its first page, page count,
and extracted text together.
Reviewed screenshots:

- [`artifact-uploader.png`](../screenshots/artifact-uploader.png)
- [`artifact-library.png`](../screenshots/artifact-library.png)
- [`artifact-detail-preview.png`](../screenshots/artifact-detail-preview.png)
- [`artifact-versions.png`](../screenshots/artifact-versions.png)
- [`artifact-pdf-preview.png`](../screenshots/artifact-pdf-preview.png)
- [`artifact-trash.png`](../screenshots/artifact-trash.png)
- [`artifact-project-home.png`](../screenshots/artifact-project-home.png)

The final real-service checks were:

```powershell
$env:MMDASH_TEST_DATABASE_URL = "postgres://..."
$env:MMDASH_TEST_MINIO_ENDPOINT = "http://localhost:9000"
$env:MMDASH_TEST_CORE_URL = "http://localhost:8080"
go test ./internal/artifact -count=1 -v

$env:REPO_LOCAL_ALLOWED_ROOTS = "/tmp"
$env:MMDASH_SMOKE_WORKER_MODE = "docker"
$env:MMDASH_SMOKE_REPO_MODE = "docker"
pnpm smoke
pnpm smoke:artifact-preview
pnpm check
```

The full Artifact suite ran every opt-in PostgreSQL, Local, MinIO, and Core
HTTP test without skips. `pnpm check` passed with Web 56/56 and Python Worker
25/25 with no skips. A fresh database applied all 14 migrations through
`000014_artifact_preview_transfer`; the preserved development database also
contains `000013` and `000014`.

The long-running Compose `worker` service requires a Project-scoped token and
therefore exits when that intentionally empty development variable is used.
Acceptance issues a real token and runs the Worker as a one-shot container.
Artifact CLI commands remain deferred until the Stage 3 Go CLI foundation is
available and an Artifact iteration adds them through its extension mechanism.
