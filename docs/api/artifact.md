# Artifact API, multipart, and retention contract

Artifact is the Stage 2 Project-scoped file module. Go Core owns authoritative
metadata, authorization, multipart state, object promotion, immutable versions,
trash, Audit, and Outbox events. Web BFF, Web, Worker, MCP Gateway, and future
modules call Core and never write Artifact tables or object storage directly.
The reserved migration is `000013_artifact`; it does not modify Repo tables.

Canonical request and response schemas live in
[`core.yaml`](../../contracts/openapi/core.yaml). Browser-safe projections are
mirrored by [`web-bff.yaml`](../../contracts/openapi/web-bff.yaml). The durable
storage decision is [ADR 0002](../adr/0002-artifact-multipart-storage.md).

## Stable identity and classification

- `Artifact` is a stable logical ID; `ArtifactVersion` is immutable content.
- Updating content creates a new Version. Restoring an old Version creates a
  new Version rather than changing history.
- `kind` is a fixed system enum. Public Stage 2 uploads allow `problem`,
  `attachment`, and `other`.
- `source` is a fixed system enum and cannot be forged by ordinary upload
  callers. Public uploads use `user_upload`.
- `tags[]` are user-defined and normalized by Core.
- Object content is deduplicated only within one Project by SHA-256 and size.
  API responses never reveal whether another Project contains the same bytes.
- Provider upload IDs, object keys, credentials, and signed URLs are omitted
  from persistent projections, events, Audit, metrics, and logs.

## Multipart upload

1. The browser incrementally computes SHA-256 and initializes an upload with a
   filename, size, MIME hint, kind, tags, idempotency key, and optional existing
   Artifact ID through the version-upload route.
2. Core checks Project RBAC and system limits, creates the pending immutable
   Version, chooses a MiB-aligned part size within the S3 10,000-part limit, and
   starts provider multipart state unless a Project-local blob is reusable.
3. The browser requests a bounded set of part numbers. MinIO/S3-compatible
   storage returns short-lived presigned PUT grants; Local returns short-lived
   signed BFF/Core streaming transfer grants.
4. Completed parts are discoverable from the application upload session.
   Browser refresh, pause, reconnect, and expired part URLs do not require a new
   Artifact or Version.
5. Confirm submits the ordered part number and ETag list. Core rejects missing,
   duplicate, out-of-range, or provider-mismatched parts, completes provider
   multipart state, checks total size, streams the complete object through
   SHA-256, and promotes it to the Project content-addressed key.
6. Successful verification makes the Version available and queues preview.
   Repeated confirm is idempotent. Cancel and expiry abort provider state;
   neither may affect confirmed Artifacts.

Provider multipart ETags are completion tokens, not content hashes. A complete
part or file must never be buffered in BFF or Core memory.

## Retention, trash, and versions

Successful originals and every immutable Version are retained indefinitely.
Deleting an Artifact moves it to Project trash and emits `artifact.deleted`;
the current Version and all historical bytes remain recoverable. Restore emits
`artifact.available` with `reason=restored`.

Permanent purge requires owner or maintainer authorization, only applies to a
trashed Artifact, and writes Audit. Blob reference counts are decremented in
the business transaction; object bytes are deleted only if no Version still
references the Project-local blob. Automatic cleanup is restricted to expired,
unconfirmed staging uploads and regenerable preview caches.

## Preview and semantic boundary

Worker job `artifact.preview` produces bounded:

- image dimensions/format and a safe thumbnail;
- PDF page count, first-page thumbnail, and bounded extracted text;
- CSV columns, row/column counts, and a bounded sample;
- JSON top-level structure and a bounded sample;
- text encoding validation and bounded text.

Unsupported or failed preview does not make the original unavailable.
`SemanticDescriptionGenerator` is reserved as an interface only. Stage 2 does
not call an LLM or multimodal model and does not automatically generate
description or `recommended_usage`; those capabilities are deferred to Article.

## RBAC

| Role               | Artifact capability                                                          |
| ------------------ | ---------------------------------------------------------------------------- |
| owner / maintainer | read, upload, download, create Version, edit metadata, trash, restore, purge |
| editor             | read, upload, download, create Version, edit metadata                        |
| viewer             | read, preview, download                                                      |
| Worker             | one-time transfer grants bound to a claimed preview Job                      |
| MCP                | caller's current Project role; no bypass                                     |

Every upload control call, metadata mutation, download grant, download, trash,
restore, purge, and Worker transfer is authorized by Core. Knowing an Artifact,
Version, upload, or transfer ID does not grant access.

## Stable errors

| Code                           | Meaning                                                 |
| ------------------------------ | ------------------------------------------------------- |
| `ARTIFACT_NOT_FOUND`           | Artifact or Version is absent in the authorized Project |
| `ARTIFACT_UPLOAD_EXPIRED`      | Upload session expired before confirmation              |
| `ARTIFACT_UPLOAD_INCOMPLETE`   | Confirmation did not supply every required part         |
| `ARTIFACT_SIZE_MISMATCH`       | Completed object size differs from the declared size    |
| `ARTIFACT_HASH_MISMATCH`       | Full-object SHA-256 differs from the declared hash      |
| `ARTIFACT_TOO_LARGE`           | File exceeds the system upload limit                    |
| `ARTIFACT_PART_INVALID`        | Part number, size, ETag, or signed transfer is invalid  |
| `ARTIFACT_PART_MISSING`        | Provider or Core cannot find a required part            |
| `ARTIFACT_UPLOAD_ABORTED`      | Upload was cancelled or aborted                         |
| `ARTIFACT_UPLOAD_CONFLICT`     | Concurrent or incompatible upload transition            |
| `ARTIFACT_KIND_INVALID`        | Public caller requested a disallowed kind               |
| `ARTIFACT_SOURCE_INVALID`      | Caller attempted to forge a system source               |
| `ARTIFACT_TAG_INVALID`         | Tags fail normalization, count, or length constraints   |
| `ARTIFACT_MIME_NOT_ALLOWED`    | File type is rejected by system policy                  |
| `ARTIFACT_NOT_AVAILABLE`       | Original Version has not passed verification            |
| `ARTIFACT_PREVIEW_UNAVAILABLE` | Requested preview is not available                      |
| `ARTIFACT_STORAGE_UNAVAILABLE` | Configured BlobStore operation is unavailable           |
| `ARTIFACT_NOT_TRASHED`         | Restore/purge requires the corresponding trash state    |
| `ARTIFACT_PURGE_CONFLICT`      | Purge conflicts with a concurrent reference change      |

Errors expose only the stable code, safe message, request ID, and bounded
structured details. They never copy provider bodies, credentials, signed URLs,
object keys, upload IDs, or file content.

## Events and integrations

Artifact writes authoritative state and their Outbox event in one transaction:

- `artifact.created` registers the stable Artifact and first pending Version;
- `artifact.available` identifies a verified immutable Version or restoration;
- `artifact.deleted` creates the Data Hub tombstone when moved to trash.

Data Hub projects `artifact` and `attachment_registry_entry`. `data.list`
returns metadata; `data.read` dispatches through the authorized Artifact reader
and may issue a short-lived controlled download. Project
`source_artifact_ids[]` must reference available, non-trashed Artifacts in the
same Project.

Stage 2 adds no Artifact CLI command. Stage 3 reuses these Core contracts after
CLI login and Project binding are implemented.
