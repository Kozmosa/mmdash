# Repo development, deployment, and acceptance

Stage 1 Repo requires Git 2.20 or newer in addition to the normal mmdash
toolchains. The Core image installs Git and the static `mmdash-git-askpass`
helper. Native development must put both executables on `PATH`.

## Migration and storage

Migration `000012_repo` creates:

- `repo_repositories` and three constrained `repo_workspaces`;
- immutable `repo_commits` and idempotent `repo_commit_events`;
- deduplicated `repo_webhook_deliveries`;
- leased `repo_checkouts`;
- idempotent `repo_commit_requests`.

Migration `000050_repo_provider_paths` adds the `managed` provider and renames
the old `local` meaning to `server_existing`. Existing repository rows and
`repo.connection` settings are migrated explicitly; compatibility reads keep a
legacy server path usable while the Settings HTTP projection returns only the
redaction marker. The next actual path edit moves it into encrypted secret
storage.

Migration `000051_repo_webhook_deliveries_repair` idempotently restores the
webhook delivery ledger for an older database that recorded the Repo baseline
without retaining that relation. It preserves repositories, Git data, and any
existing webhook delivery history.

Apply it with the normal migrator:

```bash
docker compose -f deploy/compose/compose.yaml run --rm migrate
```

`REPO_STORAGE_ROOT` must be on durable, backed-up storage supporting writable
directories and atomic rename. Compose mounts `repo-data` at
`/var/lib/mmdash/repos`. For `managed`, this volume contains the authoritative
bare Git repository; for GitHub and server-existing providers it contains the
Core-owned mirror, worktrees, and checkouts.
Do not share one local volume between active Core instances unless the
filesystem provides the same POSIX atomicity and locking guarantees. Never
manually edit `bare.git`, `worktrees`, or `checkouts`.

Disconnect is asynchronous: Core retains metadata and Git data for
`REPO_DISCONNECT_GRACE`, then removes one validated storage-key directory
before deleting its database row. This deletes the authoritative Git data for a
managed repository, but never deletes or rewrites a GitHub or server-existing
external repository. Preserve the volume during normal shutdown;
do not use `docker compose down -v` unless Repo data deletion is explicitly
approved.

## Runtime configuration

| Variable                   | Default                 | Purpose                                                 |
| -------------------------- | ----------------------- | ------------------------------------------------------- |
| `REPO_STORAGE_ROOT`        | `/var/lib/mmdash/repos` | Authoritative managed repos plus Core mirrors/worktrees |
| `REPO_LOCAL_ALLOWED_ROOTS` | empty                   | OS-separated server-existing provider allowlist         |
| `REPO_MAX_CONCURRENT_GIT`  | `4`                     | Process Git concurrency                                 |
| `REPO_COMMAND_TIMEOUT`     | `2m`                    | Ordinary command timeout                                |
| `REPO_CLONE_TIMEOUT`       | `15m`                   | Clone/fetch timeout                                     |
| `REPO_SYNC_POLL_INTERVAL`  | `2s`                    | Idle sync coordinator delay                             |
| `REPO_SYNC_LEASE`          | `20m`                   | Recoverable sync lease                                  |
| `REPO_CHECKOUT_TTL`        | `1h`                    | Default detached checkout lease                         |
| `REPO_MAX_TEXT_BYTES`      | `1048576`               | Read/write text ceiling                                 |
| `REPO_DISCONNECT_GRACE`    | `24h`                   | Delayed managed cleanup                                 |
| `REPO_ASKPASS_PATH`        | `mmdash-git-askpass`    | Static credential helper                                |
| `REPO_GITHUB_PROXY_URL`    | empty                   | Repo-only HTTP(S) proxy for GitHub API and Git HTTPS    |
| `REPO_GITHUB_NO_PROXY`     | loopback addresses      | Explicit internal/loopback proxy bypasses               |

`REPO_GITHUB_PROXY_URL` is deployment configuration, not a Project Setting.
It accepts only an `http://` or `https://` origin, optionally with userinfo;
path, query, fragment, malformed host/port, and SOCKS URLs are rejected. Treat
the complete value as a secret when it contains credentials. The dedicated
GitHub client never reads process-wide proxy variables. Git commands receive
only the validated `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` values plus
their lowercase libcurl-compatible aliases for the single subprocess;
`ALL_PROXY` and arbitrary Core environment variables remain unavailable.

`REPO_GITHUB_NO_PROXY` accepts loopback/private IPs and CIDRs, single-label
internal service names, and `.local`, `.localhost`, or `.internal` names. Public
targets such as `github.com` and wildcard bypasses are rejected so a configured
proxy fails closed instead of silently returning to direct egress. With no Repo
proxy URL, both GitHub metadata and Git operations remain direct and still do
not inherit a process-wide proxy.

Server-existing roots are colon-separated on Linux/macOS and semicolon-separated on
Windows. An empty allowlist disables this provider. Core canonicalizes the source
and rejects traversal, symlink escape, non-bare/non-worktree repositories, and
paths outside every configured root. `GET .../repository/capabilities` reports
only that the provider is enabled or disabled; it never returns configured
roots, mounts, canonical paths, storage keys, or checkout paths.

## Provider product paths

- `managed` is the default. Core creates a bare repository below
  `REPO_STORAGE_ROOT`, creates one empty initialization commit, initializes
  `main`, `article`, and `result`, and materializes the three standard
  worktrees. v0.1 exposes no external clone/fetch/push endpoint; all reads,
  commits, Article writes, and Result writes go through Repo.
- `github` connects an existing GitHub HTTPS repository with a fine-grained PAT
  and the existing webhook/branch mapping flow. Canonical GitHub URL is the
  only provider location returned by Repository APIs.
- `server_existing` connects an administrator-mounted existing repository.
  The user supplies an absolute path inside the Core service container, not a
  workstation path. The UI disables this option when the allowlist is empty,
  and Settings/Repository APIs never return that path.

Every Git subprocess receives the stable internal maintenance identity
`mmdash <repo@mmdash.local>` so ref and reflog updates never depend on host
name resolution. Authenticated workspace commits override both author and
committer with the requesting user's validated identity.

## Readiness, logs, and metrics

`GET /health/ready` verifies PostgreSQL, object storage, Git availability and
minimum version, and Repo root create/write/atomic-rename behavior.

Prometheus exposes only bounded labels:

```text
mmdash_repo_operations_total{operation,outcome,provider}
mmdash_repo_operation_duration_seconds{operation,provider}
mmdash_repo_sync_queue_depth
mmdash_repo_checkouts_active
mmdash_repo_storage_bytes
```

Structured Repo logs may contain operation, provider, safe error code,
retryable, duration, repository ID, and request ID. They must not contain PATs, webhook
secrets, AskPass variables, file content, remote provider bodies, or paths.

GitHub failures use stable retry semantics:

| Condition                                      | Code                                    | Automatic retry |
| ---------------------------------------------- | --------------------------------------- | --------------- |
| DNS, connect, TLS, or proxy connection failure | `REPO_NETWORK_UNAVAILABLE`              | yes             |
| Git or metadata request timeout                | `REPO_GIT_TIMEOUT`                      | yes             |
| GitHub 429 or 5xx                              | `REPO_PROVIDER_TEMPORARILY_UNAVAILABLE` | yes             |
| GitHub/Git authentication failure              | `REPO_AUTH_FAILED`                      | no              |
| Authenticated GitHub 404                       | `REPO_REMOTE_NOT_FOUND`                 | no              |
| Missing mapped branch                          | `REPO_BRANCH_NOT_FOUND`                 | no              |
| Missing contents write permission              | `REPO_WRITE_PERMISSION_REQUIRED`        | no              |

Retryable failures keep the current bounded exponential backoff. Terminal
failures clear the automatic request but can be retried explicitly after a
configuration, permission, or remote-state change. Existing fetched objects
and worktrees remain available for immutable reads while synchronization is in
an error state.

## Native checks

Run focused tests while changing Repo and the full gate before handoff:

```bash
pnpm contracts:generate
pnpm contracts:check
pnpm api:check
pnpm test:go
pnpm --filter @mmdash/core-client test
pnpm --filter @mmdash/web-bff test
pnpm --filter @mmdash/web test
pnpm --filter @mmdash/mcp-gateway test
pnpm check
```

Focused proxy tests use a fake HTTP proxy for GitHub metadata and a real local
Git dumb-HTTP repository for `git ls-remote`. They also prove that proxy/PAT
credentials are absent from command results and errors and that unreviewed Core
proxy variables are not inherited.

Real Git tests create temporary bare remotes. On Windows, run them in an
environment permitted to create symbolic links so the Local-root and cleanup
escape tests execute rather than skip.

## Repo E2E

Managed acceptance needs no mount or allowlist and proves initialization plus
Core-owned commits in all three workspaces:

```bash
MMDASH_SMOKE_REPO_MODE=managed pnpm smoke
```

The server-existing fixture additionally proves path allowlisting and external
push detection:

The Stage 1 smoke helper creates a unique bare server-existing Git fixture inside the
running Core container, maps `main/article/result`, binds a new project,
browses full-SHA commits/tree/content, creates and releases a detached
checkout, performs an external commit, synchronizes it, proves the old SHA is
unchanged, checks Data Hub routing, and verifies Repo metrics.

Allow only the smoke fixture root and enable the optional Repo portion:

```powershell
$env:REPO_LOCAL_ALLOWED_ROOTS = "/tmp"
$env:MMDASH_SMOKE_WORKER_MODE = "docker"
$env:MMDASH_SMOKE_REPO_MODE = "docker"
$env:MMDASH_SMOKE_COMPOSE_COMMAND = "docker-compose" # omit when plugin exists
docker-compose -f deploy/compose/compose.yaml up -d --build
pnpm smoke
```

The signed GitHub webhook path is covered by Go HMAC/deduplication/force-push
tests and BFF exact-raw-body tests. A server-existing provider deliberately has no
GitHub webhook secret, so the Docker fixture uses the same public manual
sync endpoint after its external push.

Inspect without leaking environment values:

```bash
docker compose -f deploy/compose/compose.yaml ps
docker compose -f deploy/compose/compose.yaml logs --tail 100 core web-bff web mcp-gateway
```
