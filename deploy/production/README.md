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
network, and only Core has the separate, fixed `mmdash-prod-egress` bridge for
approved external integrations. GitHub API and Git HTTPS traffic goes from Core
to the host-owned `mmdash-mihomo` listener on that bridge gateway. Internal
PostgreSQL, MinIO, Web BFF, and MCP traffic remains on Docker internal networks;
cloudflared never joins the Repo egress bridge.

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

## 3. Install the host-owned Repo proxy

The production Repo proxy is a dedicated host service, not a container, TUN,
shared network namespace, or personal desktop process. The tracked templates
are under `deploy/production/mihomo/`; the actual node/provider file and any
subscription URL remain outside Git with mode `0600`.

First verify that `MMDASH_EGRESS_SUBNET` does not overlap a host route, VPN,
site network, or existing Docker network:

```bash
ip -brief address
ip route show
docker network inspect $(docker network ls -q) --format '{{range .IPAM.Config}}{{println .Subnet}}{{end}}'
```

Change both subnet/gateway values in `.env.production`, the mihomo config, and
the nftables fragment together if `172.31.240.0/24` conflicts. Compose assigns
the stable bridge interface name `mmdash-egress`; do not expose its listener on
`0.0.0.0` or a LAN address.

Install the reviewed Linux amd64-compatible mihomo v1.19.30 binary. The digest
below is for the upstream release asset
`mihomo-linux-amd64-compatible-v1.19.30.gz` published 2026-08-16:

```bash
mihomo_archive=/tmp/mihomo-linux-amd64-compatible-v1.19.30.gz
mihomo_binary=/tmp/mmdash-mihomo-v1.19.30
curl --fail --show-error --location \
  --output "$mihomo_archive" \
  https://github.com/MetaCubeX/mihomo/releases/download/v1.19.30/mihomo-linux-amd64-compatible-v1.19.30.gz
echo 'db214c7a2517e63c150d123178d16d102e03a241ccdae4e5e07ffbe9cf56c6f9  /tmp/mihomo-linux-amd64-compatible-v1.19.30.gz' | sha256sum --check
gzip --decompress --stdout "$mihomo_archive" > "$mihomo_binary"
sudo install -o root -g root -m 0755 "$mihomo_binary" /usr/local/libexec/mmdash-mihomo
rm "$mihomo_archive" "$mihomo_binary"
```

Create a least-privilege account and directories, then install the templates:

```bash
sudo useradd --system --home-dir /var/lib/mmdash-mihomo --shell /usr/sbin/nologin mmdash-mihomo
sudo install -d -o root -g mmdash-mihomo -m 0750 /etc/mmdash-mihomo /etc/mmdash-mihomo/providers
sudo install -d -o mmdash-mihomo -g mmdash-mihomo -m 0700 /var/lib/mmdash-mihomo
sudo install -o root -g mmdash-mihomo -m 0600 \
  deploy/production/mihomo/config.example.yaml /etc/mmdash-mihomo/config.yaml
sudo install -o root -g root -m 0644 \
  deploy/production/mihomo/mmdash-mihomo.service /etc/systemd/system/mmdash-mihomo.service
```

Provision `/etc/mmdash-mihomo/providers/upstream.yaml` through the deployment
secret/configuration system. It must contain only the approved upstream nodes
needed by this service and must not be copied into the repository, shell logs,
or chat. Keep the controller on `127.0.0.1:19090`; the Core container must not
reach it.

Install the narrow host firewall fragment after reviewing it against the
existing nftables policy. It permits TCP 17890 only from the fixed Repo bridge
subnet to its gateway and drops other traffic to that listener:

```bash
sudo install -o root -g root -m 0600 \
  deploy/production/mihomo/mmdash-mihomo.nft /etc/nftables.d/mmdash-mihomo.nft
sudo nft --check --file /etc/nftables.d/mmdash-mihomo.nft
sudo nft --file /etc/nftables.d/mmdash-mihomo.nft
```

Include the fragment from the host's persistent nftables configuration using
the distribution's normal mechanism. Do not flush an existing ruleset. Enable
the service; it will restart until Compose creates the `mmdash-egress` bridge:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now mmdash-mihomo.service
```

The proxy has no DIRECT fallback. If its upstream is unavailable, Core receives
a safe retryable network error and retains existing Repo mirrors for reads.
Never work around an outage by enabling a host-wide TUN, changing the default
route, or routing cloudflared/internal services through this listener.

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

Confirm the host proxy after the bridge exists:

```bash
sudo systemctl is-active mmdash-mihomo.service
curl --fail --silent --show-error http://127.0.0.1:19090/version
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
"${prod_compose[@]}" up -d
"${prod_compose[@]}" ps -a
"${prod_compose[@]}" logs --tail 200 migrate core web-bff web mcp-gateway worker caddy cloudflared
```

Do not run `down` during a normal upgrade; Compose can replace services while
preserving named volumes. A code/image rollback is safe only when the older
release supports every migration already applied. Never run down migrations
or delete volumes as an ad hoc rollback.

The proxy service and fixed bridge require no database migration. During a
proxy incident, inspect `systemctl status mmdash-mihomo` and the bounded Core
error code before restarting only the proxy. Keep `REPO_GITHUB_PROXY_URL`
configured so failure remains closed; do not silently fall back to the unstable
direct path. Rolling back the application release may leave the dedicated
proxy running safely because no other service receives its URL.

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
and restart Core before syncing again. In a scheduled maintenance window, stop
`mmdash-mihomo`, request a sync, and confirm
`REPO_NETWORK_UNAVAILABLE` with `last_error_retryable=true`; start the proxy and
confirm the same queued synchronization succeeds through bounded retry. Check
recent Core/mihomo/cloudflared logs for panic/fatal/error loops and exact PAT,
proxy credential, or subscription matches without printing those secrets.

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
