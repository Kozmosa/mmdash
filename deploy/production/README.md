# Production deployment with Cloudflare Tunnel

This directory defines the single-host production stack for
`https://prod.mmdash.moe`. Cloudflare terminates public TLS and a remotely
managed Cloudflare Tunnel forwards the hostname to the internal Caddy origin.
No service publishes a host port; PostgreSQL, MinIO, Core, Web BFF, MCP Gateway,
the Worker, and the MinIO console remain private Docker services.

## Topology

```text
Internet
  -> Cloudflare edge (HTTPS: prod.mmdash.moe)
  -> remotely managed Tunnel
  -> cloudflared
  -> Caddy http://caddy:8080
       /api/*                 -> Web BFF
       /v1/*                  -> Web BFF controlled user API
       /mcp and /mcp/*        -> MCP Gateway
       /<artifact-bucket>/*   -> MinIO signed object requests
       everything else       -> Web
```

Core is deliberately not a Caddy upstream. The `edge`, `app`, and `data`
networks are internal. Only `cloudflared` has the dedicated `tunnel-egress`
network. Core and the dedicated `mihomo` service share `mmdash-prod-egress`;
only mihomo receives Repo GitHub traffic, while Core retains the egress network
for separately reviewed integrations. The proxy publishes no host port and is
absent from the `edge`, `app`, and `data` networks. Internal PostgreSQL, MinIO,
Web BFF, and MCP traffic remains on Docker internal networks; cloudflared never
joins the Repo egress bridge.

## 1. Configure Cloudflare

In Cloudflare Zero Trust:

1. Create a remotely managed Tunnel and copy its token once.
2. Add the public hostname `prod.mmdash.moe` to that Tunnel.
3. Set the origin service to `http://caddy:8080`.
4. In the hostname's HTTP settings, set **HTTP Host Header** to
   `prod.mmdash.moe`. The production Caddyfile rejects other Host values.
5. Let Cloudflare create the proxied DNS record for the hostname.

The origin is intentionally plain HTTP because it exists only on an internal
Docker network between `cloudflared` and Caddy. TLS terminates at Cloudflare;
there is no host listener that can bypass the Tunnel.

Do not apply a blanket Cloudflare Access policy without planning non-browser
clients. MCP clients and presigned MinIO requests cannot complete an
interactive Access challenge. If Access is required, define explicit bypass or
service-token policies for `/mcp`, `/mcp/*`, and
`/<OBJECT_STORAGE_BUCKET>/*`, and test CLI, Agent, upload, and download flows.

Keep Cloudflare caching disabled for `/api/*`, `/v1/*`, `/mcp`, `/mcp/*`, and
the Artifact bucket path. Do not create a `Cache Everything` rule for the
hostname. Caddy also emits `Cache-Control: no-store` for API and signed object
traffic.

Do not export full Artifact request URLs into Cloudflare request logs: their
query strings contain short-lived SigV4 authorization material. The production
Caddyfile skips access logging for the Artifact bucket route for the same
reason; Core remains the authoritative audit source for grant issuance and
Artifact state changes.

The host firewall needs outbound DNS plus Cloudflare Tunnel connectivity.
Allow outbound TCP and UDP 7844; allowing TCP 443 as a fallback is also
recommended. No inbound firewall rule is required for mmdash.

## 2. Create production configuration

From the repository root:

```bash
cp deploy/production/.env.example deploy/production/.env.production
chmod 600 deploy/production/.env.production
```

Edit `.env.production` and fill every empty required value. The file is ignored
by Git. In particular:

- pin `MINIO_IMAGE` and `CLOUDFLARED_IMAGE` to reviewed immutable release tags
  or multi-platform digests;
- use independent high-entropy values for `AUTH_JWT_SECRET`,
  `BFF_COOKIE_SECRET`, `SETTINGS_ENCRYPTION_KEY`, database, MinIO, bootstrap,
  and Tunnel credentials;
- set `DATABASE_URL` to the same bundled PostgreSQL account, for example
  `postgres://mmdash:<url-encoded-password>@postgres:5432/mmdash?sslmode=disable`.

Hexadecimal secrets avoid `.env`, URL, and shell escaping ambiguity:

```bash
openssl rand -hex 32
```

Run that command separately for each application secret. Do not copy example
or development credentials into production.

The tracked source-build image variables use the `docker.1ms.run` pull-through
mirror requested for this host. They are passed as Docker build arguments and
do not modify `/etc/docker/daemon.json`, a shell profile, or any global
registry setting. Treat the mirror only as transport: verify each manifest
against its upstream image and replace mutable tags with reviewed digests for
a reproducible release. An operator outside the mirror's intended network can
replace the prefixes with the upstream registry without changing source files.

The bundled MinIO path currently uses `MINIO_ROOT_USER` and
`MINIO_ROOT_PASSWORD` for both idempotent bucket initialization and Core object
access. It is suitable for the initial single-host deployment because MinIO is
not host-exposed, but it is not least privilege. A hardened deployment should
provision a separate bucket-scoped application identity or use managed S3,
then inject those application credentials into Core; that lifecycle is not yet
automated by this Compose file.

## 3. Build and configure the Docker-managed Repo proxy

Production hosts require Docker access but not root access. Mihomo therefore
runs as a dedicated Compose service instead of a host systemd unit, privileged
container, TUN device, or personal desktop process. It joins only the Repo
egress bridge, publishes no host port, uses a read-only root filesystem, and
drops every Linux capability. The controller remains on container loopback and
is unreachable from Core.

Keep the binary build context, provider file, and subscription URL outside Git
with mode `0600`. The examples below use a sibling secret directory; choose an
absolute path appropriate for the host and copy those paths into
`.env.production`:

```bash
mihomo_root=../mmdash-secrets/mihomo
mkdir -p "$mihomo_root/image" "$mihomo_root/providers"
chmod 700 "$mihomo_root" "$mihomo_root/image" "$mihomo_root/providers"
install -m 0600 deploy/production/mihomo/config.example.yaml "$mihomo_root/config.yaml"
```

Provision `$mihomo_root/providers/upstream.yaml` through the deployment secret
system. It must be a Mihomo provider document with a top-level `proxies` list
and only approved upstream nodes. Never copy its subscription URL, node names,
or credentials into the repository, build context, shell logs, or chat.

Install the reviewed Linux amd64-compatible mihomo v1.19.30 binary into the
non-secret image context. The digest is for the upstream
`mihomo-linux-amd64-compatible-v1.19.30.gz` asset published 2026-08-16:

```bash
mihomo_archive="$mihomo_root/mihomo-linux-amd64-compatible-v1.19.30.gz"
curl --fail --show-error --location \
  --output "$mihomo_archive" \
  https://github.com/MetaCubeX/mihomo/releases/download/v1.19.30/mihomo-linux-amd64-compatible-v1.19.30.gz
echo "db214c7a2517e63c150d123178d16d102e03a241ccdae4e5e07ffbe9cf56c6f9  $mihomo_archive" | sha256sum --check
gzip --decompress --stdout "$mihomo_archive" > "$mihomo_root/image/mihomo"
chmod 0555 "$mihomo_root/image/mihomo"
```

Build Core first, then build the network-free minimal mihomo image. The
Dockerfile copies only the verified binary plus the CA bundle from the local
Core image; the provider and subscription never enter an image layer:

```bash
prod_compose=(docker compose --env-file deploy/production/.env.production -f deploy/production/compose.yaml)
"${prod_compose[@]}" build core
docker build --network=none --pull=false \
  --build-arg MMDASH_CORE_IMAGE=mmdash/core:0.1.0 \
  --file deploy/production/mihomo/Dockerfile \
  --tag mmdash/mihomo:1.19.30-local \
  "$mihomo_root/image"
docker run --rm --network none mmdash/mihomo:1.19.30-local -v
```

Set `MMDASH_MIHOMO_UID=$(id -u)`, `MMDASH_MIHOMO_GID=$(id -g)`, the two
absolute config/provider paths, and
`REPO_GITHUB_PROXY_URL=http://mihomo:17890` in `.env.production`. The same
UID/GID must own the mode-`0600` files so the non-root container can read them.

The tracked config has no DIRECT fallback. If every upstream is unavailable,
Core receives a safe retryable network error and retains existing Repo mirrors
for reads. Never work around an outage by enabling a host-wide TUN, changing
the default route, or routing cloudflared/internal services through the proxy.

## 4. Validate and start

Define a reusable shell array so every command uses the same file and project:

```bash
prod_compose=(docker compose --env-file deploy/production/.env.production -f deploy/production/compose.yaml)
```

Validate interpolation and the production Caddyfile before starting:

```bash
"${prod_compose[@]}" config --quiet
pnpm caddy:check
```

For a source checkout deployment:

```bash
"${prod_compose[@]}" build --pull
# Recreate the one-shot migrator so new migrations run before Core starts.
# Do not rely on an old exited migrate container satisfying depends_on.
"${prod_compose[@]}" up -d --force-recreate migrate
"${prod_compose[@]}" ps -a migrate
"${prod_compose[@]}" up -d
```

For immutable prebuilt images, set all `MMDASH_*_IMAGE` variables and use:

```bash
"${prod_compose[@]}" pull
"${prod_compose[@]}" up -d --force-recreate migrate
"${prod_compose[@]}" ps -a migrate
"${prod_compose[@]}" up -d --no-build
```

`migrate` and `minio-init` are idempotent one-shot services. The explicit
`migrate` run above is required on every release: Compose can otherwise reuse
an old exited container and let Core start against an older schema. Core also
checks the Repo webhook schema at startup and readiness, so a missing
`repo_webhook_deliveries` ledger or resilient-sync columns stops the stack with
an actionable migration error instead of serving webhook 500 responses.
Both one-shot services must exit with code 0 before Core starts. Inspect the
result and recent logs:

```bash
"${prod_compose[@]}" ps -a
"${prod_compose[@]}" logs --tail 200 postgres minio migrate minio-init mihomo core web-bff web mcp-gateway caddy cloudflared
"${prod_compose[@]}" exec -T caddy wget -qO- http://127.0.0.1:8080/_internal/health
curl --fail --show-error --location https://prod.mmdash.moe/
```

Expected state: the long-running services, including `mihomo`, are
healthy/running, while `migrate` and `minio-init` are exited with status 0.
Check logs for panic, fatal, repeated errors, failed Tunnel registration, and
accidental credential output.

Confirm the Docker-managed proxy after the stack starts:

```bash
"${prod_compose[@]}" ps mihomo core
"${prod_compose[@]}" exec -T mihomo /usr/local/bin/mihomo -v
"${prod_compose[@]}" exec -T core sh -ec \
  'proxy="$REPO_GITHUB_PROXY_URL"; GIT_TERMINAL_PROMPT=0 HTTPS_PROXY="$proxy" HTTP_PROXY="$proxy" git ls-remote https://github.com/Kozmosa/mmdash.git HEAD >/dev/null'
```

Do not print `REPO_GITHUB_PROXY_URL` if it contains userinfo. A real GitHub
acceptance should use the Repo connection-test and synchronization APIs so PAT
and proxy credentials remain in their reviewed headers/environment boundaries.

## 5. Start the Worker

The first boot excludes the Worker because its API token must be issued by the
new Core instance. Log in with the configured bootstrap administrator, create
a dedicated Core API token for the Worker, place the one-time token value in
`MMDASH_WORKER_API_TOKEN`, and then run:

```bash
"${prod_compose[@]}" --profile worker up -d worker
"${prod_compose[@]}" --profile worker logs --tail 100 worker
```

Until the Worker is running, asynchronous Artifact previews, Article builds,
and other Worker-owned jobs cannot complete. Rotate the Worker token through
Core, update `.env.production`, and recreate only the Worker when required:

```bash
"${prod_compose[@]}" --profile worker up -d --force-recreate worker
```

## Artifact uploads through Cloudflare

Core signs browser-facing MinIO URLs with the same public origin,
`https://prod.mmdash.moe`. Caddy routes only the configured bucket prefix to
MinIO, preserves the public Host header required by SigV4, and does not log the
signed query string.

The default multipart part size is 16 MiB and Caddy accepts up to 64 MB per
signed object request. Keep `ARTIFACT_MULTIPART_PART_BYTES` below both
`CADDY_ARTIFACT_MAX_REQUEST_BODY` and the Cloudflare plan's maximum request
body size. The total Artifact may be much larger because it is split into
multiple signed PUT requests.

Changing the bucket name changes the public storage path. Update
`OBJECT_STORAGE_BUCKET`, recreate Core/Caddy, and verify new signed upload and
download URLs before serving traffic. Do not use `api`, `v1`, `mcp`, or
`_internal`, which are reserved Caddy path segments. Existing objects are not
migrated by changing the variable.

## Upgrades and rollback

Back up PostgreSQL, the Artifact bucket, and the `repo-data` volume before an
upgrade. The Repo backup is mandatory because managed repositories are
authoritative Git data rather than a cache. Then update the checkout or
immutable image references and run `build --pull` (source mode) or `pull`
(prebuilt mode), followed by:

```bash
"${prod_compose[@]}" up -d --force-recreate migrate
"${prod_compose[@]}" ps -a migrate
"${prod_compose[@]}" up -d
"${prod_compose[@]}" ps -a
"${prod_compose[@]}" logs --tail 200 migrate mihomo core web-bff web mcp-gateway worker caddy cloudflared
```

Do not run `down` during a normal upgrade; Compose can replace services while
preserving named volumes. A code/image rollback is safe only when the older
release supports every migration already applied. Never run down migrations
or delete volumes as an ad hoc rollback.

The proxy service requires no database migration. During a proxy incident,
inspect `"${prod_compose[@]}" ps mihomo`, its bounded logs, and the Core error
code before restarting only that service with
`"${prod_compose[@]}" restart mihomo`. Keep `REPO_GITHUB_PROXY_URL` configured
so failure remains closed; do not silently fall back to the unstable direct
path. Rolling back the application release may leave the dedicated proxy
running safely because no other service receives its URL.

## Backup and restore

PostgreSQL logical backup, written outside Docker volumes:

```bash
backup_root=../mmdash-backups
mkdir -p "$backup_root/postgres"
chmod 700 "$backup_root" "$backup_root/postgres"
"${prod_compose[@]}" exec -T postgres sh -ec 'PGPASSWORD="$POSTGRES_PASSWORD" pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom' > "$backup_root/postgres/mmdash-$(date -u +%Y%m%dT%H%M%SZ).dump"
```

Managed Repo data, archived outside Docker volumes without printing
repository contents to the terminal:

```bash
mkdir -p "$backup_root/repo"
"${prod_compose[@]}" run --rm --no-deps -T core \
  sh -ec 'tar -C /var/lib/mmdash/repos -czf - .' \
  > "$backup_root/repo/repo-$(date -u +%Y%m%dT%H%M%SZ).tar.gz"
```

After an upgrade, run the managed repository acceptance path against the
private application network. It creates a temporary Project, verifies that
server-existing access is disabled, initializes all three managed branches,
commits through Core, and reads the files through Web BFF. Read only the
bootstrap login from the running Core container; do not print it:

```bash
smoke_email=$("${prod_compose[@]}" exec -T core printenv AUTH_BOOTSTRAP_EMAIL)
smoke_password=$("${prod_compose[@]}" exec -T core printenv AUTH_BOOTSTRAP_PASSWORD)
"${prod_compose[@]}" run --rm --no-deps -T \
  -e MMDASH_SMOKE_URL=http://web-bff:3001 \
  -e MMDASH_SMOKE_CORE_URL=http://core:8080 \
  -e MMDASH_SMOKE_EMAIL="$smoke_email" \
  -e MMDASH_SMOKE_PASSWORD="$smoke_password" \
  -e MMDASH_SMOKE_EXPECT_SERVER_REPO_DISABLED=1 \
  -v "$PWD/scripts:/workspace/scripts:ro" \
  -w /workspace web-bff node scripts/managed-repo-smoke.mjs
```

Never publish Core as a public Caddy upstream for acceptance.

For GitHub acceptance, connect a dedicated private test repository, verify all
three workspaces become `ready`, perform a manual sync and an external commit,
and restart Core before syncing again. In a scheduled maintenance window, run
`"${prod_compose[@]}" stop -t 30 mihomo`, request a sync, and confirm
`REPO_NETWORK_UNAVAILABLE` with `last_error_retryable=true`; run
`"${prod_compose[@]}" start mihomo` and confirm the same queued synchronization
succeeds through bounded retry. Check recent Core/mihomo/cloudflared logs for
panic/fatal/error loops and exact PAT, proxy credential, or subscription
matches without printing those secrets.

Use a pinned `minio/mc` container attached to the `mmdash-prod_data` network to
`mc mirror` the configured bucket into encrypted backup storage. This keeps the
operator toolchain containerized instead of installing it globally. Test both
PostgreSQL restore and MinIO mirror restore on a separate stack; a backup is
not complete until restore has been exercised.

Stopping the application does not delete data:

```bash
"${prod_compose[@]}" down
```

Never use `down -v` unless permanent deletion of PostgreSQL, MinIO, Repo, and
Artifact data has been explicitly approved and independently backed up.
