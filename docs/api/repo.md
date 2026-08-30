# Repo API and safety contract

Repo is the Stage 1 project module. Go Core is the sole owner of repository
configuration, Git subprocesses, managed storage, worktrees, commits, webhook
delivery metadata, and Repo Outbox events. Web BFF, Web, MCP Gateway, Data Hub,
Worker, and future modules may call Core; they never read or write Git storage
or Repo tables directly.

Canonical schemas and error responses live in
[`core.yaml`](../../contracts/openapi/core.yaml) and
[`web-bff.yaml`](../../contracts/openapi/web-bff.yaml). The endpoint catalog is
[`endpoints.md`](endpoints.md).

## Connection and workspace model

One Project may use one of three repository paths:

- `managed`: Core creates and owns the authoritative bare repository under
  `REPO_STORAGE_ROOT`; this is the recommended default;
- `github`: Core connects an existing GitHub HTTPS repository with a
  fine-grained PAT;
- `server_existing`: Core connects an existing repository mounted into the
  Core container and covered by `REPO_LOCAL_ALLOWED_ROOTS`.

The saved `repo.connection` setting contains:

- provider and remote URL/path;
- optional fine-grained GitHub PAT;
- three distinct existing branches mapped to logical `code`, `article`, and
  `result` workspaces.

For `managed`, Core creates one empty initial commit and initializes the
configured `code`, `article`, and `result` branches (defaulting to `main`,
`article`, and `result`). For GitHub and server-existing repositories the
three branches must already exist; Core never silently creates an external
branch. Repository locations and credentials are secret Settings fields and
every Settings HTTP read returns `********`; trusted Repo resolution can still
read legacy server paths promoted from the former public field. A newly
generated GitHub webhook secret is returned only once.

Core keeps a bare repository under its generated storage key and maintains
three long-lived worktrees on the fixed local branches `mmdash/code`,
`mmdash/article`, and `mmdash/result`. A managed repository uses its bare
repository as an internal self-origin so the same fetch/push and worktree
invariants apply. Repository responses expose a canonical `remote_url` only
for GitHub. API responses never expose managed storage locations,
server-existing paths, storage keys, PATs, AskPass state, or checkout paths.

## Public operations

The browser-safe BFF surface is:

| Method   | Path                                                       | Purpose                                      |
| -------- | ---------------------------------------------------------- | -------------------------------------------- |
| `GET`    | `/api/projects/{projectId}/repository`                     | Status and workspace heads                   |
| `GET`    | `/api/projects/{projectId}/repository/capabilities`        | Browser-safe provider availability           |
| `PUT`    | `/api/projects/{projectId}/repository`                     | Connect, recover, or replace tested settings |
| `DELETE` | `/api/projects/{projectId}/repository`                     | Delayed managed disconnect                   |
| `POST`   | `/api/projects/{projectId}/repository/test`                | Safe provider/branch checks                  |
| `POST`   | `/api/projects/{projectId}/repository/sync`                | Coalesced synchronization                    |
| `POST`   | `/api/projects/{projectId}/repository/webhook-secret`      | Rotate one-time secret                       |
| `PATCH`  | `/api/projects/{projectId}/repository/workspaces`          | Replace distinct mappings                    |
| `GET`    | `/api/projects/{projectId}/repository/branches`            | Remote branch heads                          |
| `GET`    | `/api/projects/{projectId}/repository/commits`             | Workspace commit page                        |
| `GET`    | `/api/projects/{projectId}/repository/commits/{commitSha}` | Commit detail                                |
| `GET`    | `/api/projects/{projectId}/repository/tree`                | One directory level                          |
| `GET`    | `/api/projects/{projectId}/repository/content`             | Safe immutable content                       |
| `POST`   | `/api/webhooks/github/{hookId}`                            | Exact raw GitHub webhook body                |

Core additionally exposes fixed-SHA detached checkout and controlled
commit/push operations for trusted internal callers. They are intentionally
absent from BFF, Web, and MCP. `ArticleWorkspace` is the narrow future-facing
in-process write interface; the Article product module is not implemented in
Stage 1.

## Immutable reads

Branch names are accepted only while listing branch history. Tree, content,
commit detail, checkout, and write preconditions use a complete 40- or 64-hex
commit SHA. Tree pages return one level and an opaque cursor. Content reads use
Git objects, not filesystem traversal, and return UTF-8 text only within
`REPO_MAX_TEXT_BYTES`.

Binary, oversized, LFS pointer, symlink, and submodule objects return bounded
metadata with a `preview_status`; Core does not follow links, materialize LFS,
or recurse into submodules. The independent Web Repository browser is reached
from Repo settings rather than the workspace sidebar. It keeps
`workspace/revision/path` in the URL and isolates every response with a query
key containing the pinned workspace, revision, and path. In-flight immutable
reads may finish into cache when a view unmounts, avoiding development-mode
abort noise without allowing an old response to replace the active location.
The browser never offers Save, Commit, Push, Delete, or an editor callback. The
solving-record route is reserved for future experiment records that bind a
commit and compose its file tree, preview, and result analysis.

## Synchronization, writes, and cleanup

Sync jobs are leased from PostgreSQL and different repositories may run in
parallel. The Git client enforces the process-wide concurrency cap. Fetch is
authoritative; a webhook payload can request synchronization but cannot supply
trusted commit content. Force pushes retain already fetched immutable objects.

Internal commits require:

- an expected workspace head;
- an idempotency key;
- bounded ordinary-file changes;
- an authenticated author;
- a normal non-force push.

Prepared commits are persisted before push. A rejected push restores the
managed worktree and leaves a safe retryable record. Disconnect marks the
repository first. During `REPO_DISCONNECT_GRACE`, the same provider and
canonical location may restore that row in place, reusing its repository ID,
storage key, Git objects, and metadata while allowing GitHub PAT and branch
mapping updates. Managed repository workspaces cannot be remapped after
connection because Core owns their initialized branch topology. A different
binding requires explicit user confirmation through
`RepoConnectRequest.replace_disconnected=true`. Core tests the new connection
before taking a replacement lease, removes exactly the old generated
storage-key directory and metadata, and then creates the new pending binding.
That cleanup deletes a disconnected managed repository because its generated
storage is authoritative; it never deletes or modifies a GitHub or
server-existing source repository. If cleanup fails, Core releases the
replacement lease and preserves the old disconnected binding. Without that
flag, the mismatch remains a conflict. After the grace period, a cleanup lease
wins atomically over reconnect; the worker validates and removes exactly the
generated storage-key directory, then deletes metadata. Failure releases the
lease and reschedules cleanup without creating or deleting another repository
row.

## Events, Data Hub, and MCP

Repo writes its business change and Outbox record in one transaction:

- `repo.connected`;
- `repo.commit.created`;
- `repo.commit.detected`.

Data Hub projects stable `repository`, immutable `repo_commit`, and
current-code-head `repo_file` objects. Projection rows contain metadata only.
`data.read` dispatches to an authorized Repo reader, which resolves full content
from the pinned Git object. MCP `data.list` and `data.read` use those Core APIs
with tool/project authorization and an audit record.

GitHub webhooks require the exact request body, `X-Hub-Signature-256`, event,
and delivery headers. Core verifies HMAC-SHA256, caps the body at 1 MiB,
deduplicates delivery IDs and payload hashes, and coalesces mapped push refs.

## RBAC and stable errors

Owners and maintainers have `project.repo.manage`; editors and trusted internal
module identities may have `project.repo.write`; project members with
`project.repo.read` may browse immutable data. Project and token scope is
checked by Core on every operation.

Representative safe error codes include `REPO_AUTH_FAILED`,
`REPO_BRANCH_NOT_FOUND`, `REPO_HEAD_CONFLICT`, `REPO_WORKTREE_DIRTY`,
`REPO_WEBHOOK_SIGNATURE_INVALID`, `REPO_PATH_INVALID`, and
`REPO_GIT_TIMEOUT`. Provider bodies, credentials, command arguments, and
filesystem paths are never copied into API errors.

See [Repo development and operations](../development/repo.md) for migration,
configuration, readiness, metrics, tests, and Docker acceptance.
