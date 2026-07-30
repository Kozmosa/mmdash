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

Apply it with the normal migrator:

```bash
docker compose -f deploy/compose/compose.yaml run --rm migrate
```

`REPO_STORAGE_ROOT` must be on durable storage supporting writable directories
and atomic rename. Compose mounts `repo-data` at `/var/lib/mmdash/repos`.
Do not share one local volume between active Core instances unless the
filesystem provides the same POSIX atomicity and locking guarantees. Never
manually edit `bare.git`, `worktrees`, or `checkouts`.

Disconnect is asynchronous: Core retains metadata and Git data for
`REPO_DISCONNECT_GRACE`, then removes one validated storage-key directory
before deleting its database row. Preserve the volume during normal shutdown;
do not use `docker compose down -v` unless Repo data deletion is explicitly
approved.

## Runtime configuration

| Variable                   | Default                 | Purpose                               |
| -------------------------- | ----------------------- | ------------------------------------- |
| `REPO_STORAGE_ROOT`        | `/var/lib/mmdash/repos` | Managed Git root                      |
| `REPO_LOCAL_ALLOWED_ROOTS` | empty                   | OS-separated Local provider allowlist |
| `REPO_MAX_CONCURRENT_GIT`  | `4`                     | Process Git concurrency               |
| `REPO_COMMAND_TIMEOUT`     | `2m`                    | Ordinary command timeout              |
| `REPO_CLONE_TIMEOUT`       | `15m`                   | Clone/fetch timeout                   |
| `REPO_SYNC_POLL_INTERVAL`  | `2s`                    | Idle sync coordinator delay           |
| `REPO_SYNC_LEASE`          | `20m`                   | Recoverable sync lease                |
| `REPO_CHECKOUT_TTL`        | `1h`                    | Default detached checkout lease       |
| `REPO_MAX_TEXT_BYTES`      | `1048576`               | Read/write text ceiling               |
| `REPO_DISCONNECT_GRACE`    | `24h`                   | Delayed managed cleanup               |
| `REPO_ASKPASS_PATH`        | `mmdash-git-askpass`    | Static credential helper              |

Local roots are colon-separated on Linux/macOS and semicolon-separated on
Windows. An empty allowlist disables Local Git. Core canonicalizes the source
and rejects traversal, symlink escape, non-bare/non-worktree repositories, and
paths outside every configured root.

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
duration, repository ID, and request ID. They must not contain PATs, webhook
secrets, AskPass variables, file content, remote provider bodies, or paths.

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

Real Git tests create temporary bare remotes. On Windows, run them in an
environment permitted to create symbolic links so the Local-root and cleanup
escape tests execute rather than skip.

## Docker Local Git E2E

The Stage 1 smoke helper creates a unique bare Local Git fixture inside the
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
tests and BFF exact-raw-body tests. A Local provider deliberately has no
GitHub webhook secret, so the Local Docker fixture uses the same public manual
sync endpoint after its external push.

Inspect without leaking environment values:

```bash
docker compose -f deploy/compose/compose.yaml ps
docker compose -f deploy/compose/compose.yaml logs --tail 100 core web-bff web mcp-gateway
```
