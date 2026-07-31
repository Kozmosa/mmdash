# Artifact Web and BFF

Stage 2 exposes the Project-scoped Artifact library at
`/projects/{projectId}/artifacts`. The browser never receives object-storage
credentials or permanent object URLs. It asks the BFF for short-lived transfer
grants and keeps only resumable upload metadata in `localStorage`; file bytes
remain in the user-selected `File`.

## Process boundary

All Artifact JSON routes follow the normal authenticated boundary:

```text
Browser -> Web BFF -> Go Core -> PostgreSQL / BlobStore
```

The BFF validates query and body shapes with Zod and delegates every business
operation to Core through `@mmdash/core-client`. It does not write Artifact,
Version, Upload, Preview, or Project state.

MinIO and S3-compatible multipart grants are returned unchanged so the browser
can PUT each part directly to the provider. Core-local grants are rewritten
from `/v1/artifact-transfers/{token}` to the same-origin
`/api/artifact-transfers/{token}` route. That BFF route forwards request and
response streams without buffering a complete part or file and does not add a
Project header: the signed token is the transfer authority.

## Browser multipart engine

`MultipartUploadTask` implements the Stage 2 upload contract:

- incrementally calculate the complete file SHA-256 before initialization or
  recovery;
- request a short-lived grant immediately before each part transfer;
- upload with bounded concurrency and retry an individual part with a newly
  signed grant;
- pause by aborting in-flight requests, then resume the missing provider parts;
- recover after refresh from Core/provider state plus local upload metadata;
- cancel by aborting both active requests and the provider upload;
- confirm with a sorted part manifest only after every part is present.

Recovery prechecks the selected file name and size. The complete SHA-256 is the
authoritative identity check; `lastModified` is recorded for display and
diagnostics but is not treated as identity because browsers and copied files
may change it without changing bytes. Local storage records no file contents,
credentials, or signed URLs.

## Project UI

The library provides active and trash views, fixed `kind`/`source` filters,
status and free-tag filters, pagination, upload and version-upload drawers,
metadata editing, version history, download grants, generated previews,
restore, and permanent purge. Project roles gate visible mutations:
owner/maintainer/editor may upload or edit metadata; only owner/maintainer may
manage trash.

Project creation remains two-step: create the Project, route to the library in
setup mode, upload/select available problem Artifacts, then PATCH
`source_artifact_ids[]`. Previously selected IDs that are no longer shown in
the active library remain removable. The Project home consumes the typed home
aggregate and links each validated source Artifact back to its library detail.

System Artifact kinds remain read-only in the editor. Users may still edit
their display name, tags, and description without submitting an invalid public
`kind` replacement. Trash details use the item returned by the trash listing
and do not request active-only detail, preview, or download grants.

## Focused verification

```bash
pnpm --filter @mmdash/core-client test
pnpm --filter @mmdash/web-bff test
pnpm --filter @mmdash/web test
pnpm --filter @mmdash/core-client --filter @mmdash/web-bff --filter @mmdash/web build
pnpm lint:ts
```

The final Stage 2 gate must additionally exercise the real Local and MinIO
multipart paths through Compose, run the repository-wide `pnpm check`, and
complete the browser screenshot acceptance described in the Artifact handoff.
