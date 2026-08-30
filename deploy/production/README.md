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
network, and only Core has the separate `egress` network for approved external
integrations.

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

## 3. Validate and start

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
"${prod_compose[@]}" up -d
```

For immutable prebuilt images, set all `MMDASH_*_IMAGE` variables and use:

```bash
"${prod_compose[@]}" pull
"${prod_compose[@]}" up -d --no-build
```

`migrate` and `minio-init` are idempotent one-shot services. They must exit
with code 0 before Core starts. Inspect the result and recent logs:

```bash
"${prod_compose[@]}" ps -a
"${prod_compose[@]}" logs --tail 200 postgres minio migrate minio-init core web-bff web mcp-gateway caddy cloudflared
"${prod_compose[@]}" exec -T caddy wget -qO- http://127.0.0.1:8080/_internal/health
curl --fail --show-error --location https://prod.mmdash.moe/
```

Expected state: the long-running services are healthy/running, while `migrate`
and `minio-init` are exited with status 0. Check logs for panic, fatal, repeated
errors, failed Tunnel registration, and accidental credential output.

## 4. Start the Worker

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
"${prod_compose[@]}" up -d
"${prod_compose[@]}" ps -a
"${prod_compose[@]}" logs --tail 200 migrate core web-bff web mcp-gateway worker caddy cloudflared
```

Do not run `down` during a normal upgrade; Compose can replace services while
preserving named volumes. A code/image rollback is safe only when the older
release supports every migration already applied. Never run down migrations
or delete volumes as an ad hoc rollback.

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
private application network. It creates a temporary Project, initializes all
three branches, commits through Core, and reads the files through Web BFF.
Load only the bootstrap login into shell variables; do not print them:

```bash
set -a
. deploy/production/.env.production
set +a
"${prod_compose[@]}" run --rm --no-deps -T \
  -e MMDASH_SMOKE_URL=http://web-bff:3001 \
  -e MMDASH_SMOKE_CORE_URL=http://core:8080 \
  -e MMDASH_SMOKE_EMAIL="$AUTH_BOOTSTRAP_EMAIL" \
  -e MMDASH_SMOKE_PASSWORD="$AUTH_BOOTSTRAP_PASSWORD" \
  -v "$PWD/scripts:/workspace/scripts:ro" \
  -w /workspace web-bff node scripts/managed-repo-smoke.mjs
```

Never publish Core as a public Caddy upstream for acceptance.

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
