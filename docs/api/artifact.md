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
  `attachment`, and `other`. An authenticated product Agent may create only
  `agent` through the dedicated private Agent-upload operation.
- `source` is a fixed system enum and cannot be forged by ordinary upload
  callers. Public uploads use `user_upload`; dedicated Agent uploads use
  `agent`.
- `tags[]` are user-defined and normalized by Core.
- Object content is deduplicated only within one Project by SHA-256 and size.
  API responses never reveal whether another Project contains the same bytes.
- Provider upload IDs, object keys, credentials, and signed URLs are omitted
  from persistent projections, events, Audit, metrics, and logs.

Project folders are authoritative Core data shared by the Artifact library and
Article sidebar. Folder deletion never deletes Artifact data. The default
`recursive=false` mode moves direct Artifacts to the project root and rejects a
folder with children. `recursive=true` removes the complete descendant folder
structure after moving every contained Artifact to the project root. Both
choices are explicit in the browser UI.

## Multipart upload

1. The browser or Agent runtime incrementally computes SHA-256 and initializes an upload with a
   filename, size, MIME hint, kind, tags, idempotency key, an optional Project
   folder, and an optional existing Artifact ID through the version-upload
   route. For a new browser Artifact, Core assigns the folder in the same
   transaction that creates the stable Artifact; callers do not need a
   follow-up move request.
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

## Agent image and file uploads

`POST /v1/projects/{projectId}/artifacts/agent-uploads` is a private-Core
operation authenticated by the same Agent Token received by MCP Gateway. It
does not accept a caller-controlled `kind`; Core always creates
`kind=agent`, `source=agent`. The upload session records both the issuing user
in the existing `created_by` field and the exact `agent_instance_id`, so another
Agent instance cannot sign, complete, or abort it.

Hermes reaches this operation only through the exact `artifact.upload` MCP
grant. File bytes never traverse MCP JSON or Gateway: Hermes PUTs exact parts
to the returned MinIO/S3 grants and reports ETags to `complete`. Deployments
used by a remote Hermes must configure an object-storage endpoint reachable by
that runtime. A Local/Core proxy transfer is rejected by the Tool instead of
returning an unusable URL. Browser upload and metadata-edit routes continue to
reject `agent`, so a user cannot forge Agent provenance.

When `begin` includes an `agent_session_id` and `agent_run_id`, both are
required. Core verifies that the Run belongs to that Session, Agent instance,
and Project, then creates an immutable `output -> agent_run` relation when the
first Version becomes available. The Agent transcript derives its inline file
cards and image previews from these relations rather than parsing provider
paths or assistant prose.

The browser chat composer reuses the public multipart API with
`kind=attachment`, `source=user_upload`. Starting a Run binds each selected
available Artifact Version through an `attachment -> agent_run` relation.
Hermes can inspect those bytes only via exact-scope `artifact.read`, which
reuses Core's normal Artifact RBAC and short-lived download-grant operation.

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

## Project Artifact folders

Folders are durable, Project-scoped metadata owned by Core. A folder contains
`folder_id`, `project_id`, an optional `parent_folder_id`, a display `name`,
and a non-negative sibling `position`; it does not own or duplicate file
bytes. `GET /v1/projects/{projectId}/artifacts/folders` returns the complete
nested tree. `POST` creates a folder, `PATCH` renames it, and
`POST /folders/{folderId}/move` changes its parent or makes it a root folder.
Folder names are unique case-insensitively among siblings, including the
project root.

`PUT /v1/projects/{projectId}/artifacts/{artifactId}/folder` assigns an
Artifact to a folder. Sending `{"folder_id": null}` moves it to the project
root. The target folder must belong to the same Project, and all mutations are
authorized by the Project Artifact permission boundary.

`POST /v1/projects/{projectId}/artifacts/uploads` also accepts an optional
`folder_id`. The assignment is validated by the same Project-scoped foreign
key and persisted atomically with Artifact initialization. Reusing an
idempotency key with a different target folder returns
`ARTIFACT_UPLOAD_CONFLICT`.

Deleting a folder has an intentionally safe, non-recursive policy: direct
Artifacts are moved to the root in the same transaction, but a folder with
child folders returns `ARTIFACT_FOLDER_HAS_CHILDREN` and remains unchanged.
Self/descendant moves return `ARTIFACT_FOLDER_CYCLE`; sibling name collisions
return `ARTIFACT_FOLDER_CONFLICT`.

## Preview and semantic boundary

Worker job `artifact.preview` produces bounded:

- image dimensions/format and a safe thumbnail;
- PDF page count, first-page thumbnail, and bounded extracted text;
- CSV columns, row/column counts, bounded numeric statistics, and a bounded
  sample;
- JSON top-level structure and a bounded sample;
- text encoding validation and bounded text.

Preview reads are explicitly Version-scoped through
`GET /v1/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/previews`.
The Project file drawer lets callers select any retained available Version and
renders that Version's independent preview state. PDF rendering combines the
first-page thumbnail, page count, and bounded extracted text; it does not
depend on a permanent object URL.

Unsupported or failed preview does not make the original unavailable.
`SemanticDescriptionGenerator` is reserved as an interface only. Stage 2 does
not call an LLM or multimodal model and does not automatically generate
description or `recommended_usage`; those capabilities are deferred to Article.

The Worker receives only target IDs in the `artifact.preview` payload. It uses
its existing API token to obtain Job-bound short-lived Core GET/PUT grants.
Generated thumbnails are completed through provider multipart state and must
pass full size and SHA-256 verification before the Job and preview projection
become available atomically. The additional internal persistence migration is
`000014_artifact_preview_transfer`.

## RBAC

| Role               | Artifact capability                                                          |
| ------------------ | ---------------------------------------------------------------------------- |
| owner / maintainer | read, upload, download, create Version, edit metadata, trash, restore, purge |
| editor             | read, upload, download, create Version, edit metadata                        |
| viewer             | read, preview, download                                                      |
| Worker             | short-lived retry-safe grants bound to a claimed preview Job                 |
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
| `ARTIFACT_FOLDER_CONFLICT`     | Folder name conflicts with a sibling                    |
| `ARTIFACT_FOLDER_HAS_CHILDREN` | Folder has child folders and cannot be deleted          |
| `ARTIFACT_FOLDER_CYCLE`        | Folder move would create a parent cycle                 |

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
and may issue a short-lived controlled download. Pending objects remain
metadata-readable without a download capability. A trashed Artifact and its
registry entry remain as hidden tombstones until restoration; signed URLs and
storage identifiers are never persisted in either projection. Project
`source_artifact_ids[]` must reference available, non-trashed Artifacts in the
same Project. Project creation intentionally accepts no source IDs: the Web
flow creates the Project first, uploads within it, then PATCHes the validated
references. The Project-home Problem aggregate exposes those sources and their
current preview state.

Stage 2 adds no Artifact CLI command. Stage 3 provides the Go CLI foundation,
login, and Project selection; a later Artifact iteration reuses these Core
contracts when it registers Artifact commands with that foundation.
