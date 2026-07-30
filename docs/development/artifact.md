# Artifact development, deployment, and Core acceptance

Stage 2 Artifact stores stable Project-scoped file identities and immutable
Versions. Go Core is the only owner of Artifact, upload-session, Blob, trash,
authorization, and Audit state. Browser and Worker bytes cross only the
`BlobStore` boundary.

The contract is documented in the [Artifact API guide](../api/artifact.md) and
the storage decision in [ADR 0002](../adr/0002-artifact-multipart-storage.md).
Migration `000013_artifact` must be applied through the normal migrator.

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

| Variable                              | Compose default                  | Purpose                                      |
| ------------------------------------- | -------------------------------- | -------------------------------------------- |
| `ARTIFACT_STORAGE_BACKEND`            | `minio`                          | `local`, `minio`, or `s3`                    |
| `ARTIFACT_LOCAL_STORAGE_ROOT`         | `/var/lib/mmdash/artifacts`      | Durable Local adapter root                   |
| `ARTIFACT_UPLOAD_MAX_BYTES`           | `10737418240`                    | Per-file system limit                        |
| `ARTIFACT_MULTIPART_PART_BYTES`       | `16777216`                       | Preferred part size; Core increases as needed |
| `ARTIFACT_MULTIPART_URL_TTL`          | `15m`                            | Direct or Core-stream transfer lifetime      |
| `ARTIFACT_MULTIPART_SESSION_TTL`      | `24h`                            | Refresh-safe upload-session lifetime         |
| `ARTIFACT_STAGING_TTL`                | `24h`                            | Earliest automatic staging cleanup           |
| `ARTIFACT_STAGING_SWEEP_INTERVAL`     | `5m`                             | Expired-session reaper interval              |
| `OBJECT_STORAGE_ENDPOINT`             | `http://minio:9000` in Compose   | MinIO/S3-compatible endpoint                 |
| `OBJECT_STORAGE_BUCKET`               | `mmdash`                         | Artifact object bucket                       |
| `OBJECT_STORAGE_REGION`               | `us-east-1`                      | Signing region                               |
| `OBJECT_STORAGE_ACCESS_KEY` / secret  | local development values only    | Provider credentials                         |

Compose mounts the Local root and MinIO data on named volumes. Preserve those
volumes during ordinary shutdown. Automatic cleanup aborts only expired,
unconfirmed staging uploads; it never deletes available originals or immutable
Versions.

## Core lifecycle

Initialization validates Project RBAC, normalizes metadata, computes an
S3-compatible dynamic multipart plan, and reuses a matching Project-local
SHA-256 plus size Blob only after a real object `Stat`. Part grants are signed
on demand. Upload-session reads reconcile completed parts with the provider so
pause, reconnect, and browser refresh do not create a second Version.

Confirmation holds a database lease, compares the exact ordered part list with
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

## Core verification

Run focused native checks with a migrated PostgreSQL database:

```bash
go test ./internal/artifact ./internal/audit ./internal/project \
  ./internal/platform/coreapp ./internal/platform/metrics
go test ./...
go vet ./...
go build ./...
```

The real Artifact integration suite is opt-in:

- `MMDASH_TEST_DATABASE_URL` enables PostgreSQL plus Local multipart coverage.
- `MMDASH_TEST_MINIO_ENDPOINT`, access key, and secret key add real MinIO
  presigned multipart coverage.
- `MMDASH_TEST_CORE_URL` and `MMDASH_TEST_ADMIN_PASSWORD` run the public Core
  HTTP lifecycle against a healthy Compose Core.

Acceptance must cover out-of-order and missing parts, refresh recovery,
provider ETag mismatch, repeated and concurrent confirmation, cancellation,
expiry, full-size and SHA-256 mismatch, cross-role denial, version retention,
trash restore, shared-Blob purge, and Local signed streaming. Inspect Core logs
afterward for errors and secret-bearing values.

Worker previews, Outbox/Data Hub/MCP/Project source integration, and Web/BFF
are added by the following Stage 2 checkpoints. Artifact CLI commands remain
deferred to Stage 3.
