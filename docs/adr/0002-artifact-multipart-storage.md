# ADR 0002: Immutable Artifact versions and multipart object storage

Status: accepted for Stage 2

## Context

Stage 2 must provide one file identity and storage boundary for problem files,
attachments, previews, future experiment results, model files, and article
builds. Later modules must reference stable IDs instead of creating parallel
file tables or persisting expiring object-storage URLs. Uploads may be large,
must survive browser refreshes, and must work with Local, MinIO, and
S3-compatible deployments.

## Decision

- `Artifact` is the stable logical identity. Every content change creates an
  immutable `ArtifactVersion`; available versions are never overwritten.
- System-owned `kind` and `source` are separate from user-owned free-form
  `tags[]`. Public Stage 2 uploads use source `user_upload`.
- Object content is addressed and deduplicated only within one Project by
  `(project_id, sha256, size)`. Object keys never contain user filenames.
- Successful files and all immutable versions are retained indefinitely.
  Normal deletion moves an Artifact to recoverable trash. Object bytes may be
  removed only after an explicit trash purge and only when no Version
  references the blob.
- Stage 2 uses multipart uploads for all non-deduplicated object content.
  Core chooses a MiB-aligned part size within the S3 10,000-part limit, signs
  parts on demand, lists provider state, completes or aborts idempotently, and
  verifies the completed object's full size and streaming SHA-256.
- MinIO and S3-compatible adapters return short-lived presigned part PUTs.
  Local storage uses short-lived signed BFF/Core streaming transfer routes.
  No route buffers a complete part or file in memory.
- Refresh recovery uses the application upload ID and Core-authorized session
  state. Provider upload IDs, object keys, credentials, and signed URLs are
  never persisted in Data Hub, events, Audit, or logs.
- Object-storage endpoints, bucket, credentials, upload limits, part size,
  signing TTL, session TTL, and staging sweep settings are system deployment
  environment variables. Stage 2 adds no Project storage setting or storage
  administration panel.
- Worker creates bounded previews, thumbnails, and structural summaries.
  `SemanticDescriptionGenerator` is an interface only; LLM/multimodal
  description and automatic `recommended_usage` are deferred to Article.
- CLI login and Project selection are deferred to the Stage 3 Go CLI
  foundation. Artifact commands are added by a later Artifact iteration through
  the CLI feature registry.

## Consequences

Core remains the sole authority for Artifact metadata, authorization, upload
state, object promotion, reference counts, Audit, Outbox events, and Worker
transfer grants. Browser, BFF, Worker, MCP, and future modules never write
Artifact tables or object storage directly except through a scoped, expiring
transfer grant.

Multipart completion is not proof of content identity: provider ETags are used
only for part completion, while Core performs full-object size and SHA-256
verification before a Version becomes available. Expired unconfirmed staging
uploads and regenerable preview caches may be swept; confirmed originals and
historical Versions may not.

## Alternatives considered

- Single-request upload first: rejected because it creates a temporary product
  path without refresh-safe resume and does not satisfy Stage 2 acceptance.
- S3 ETag as content hash: rejected because multipart ETags are not whole-file
  SHA-256 values.
- Cross-Project hash deduplication: rejected because it leaks content
  existence across authorization boundaries.
- Permanent public URLs: rejected because authorization and revocation belong
  to Core and every transfer grant must be short-lived.
- Automatic cleanup of old Versions: rejected because immutable historical
  content is a reproducibility contract.
