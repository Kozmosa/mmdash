# Native development environment

This guide starts the complete local development workbench with Pixi. Pixi
provides and isolates Node.js, Go, Python, PostgreSQL, MinIO, Caddy, and `uv`.
Those tools, their caches, service data, logs, and temporary files stay below
the repository-local `.testenv` directory. The asynchronous Worker runs
natively when a compatible Pandoc/LaTeX toolchain is available and otherwise
uses its pinned Docker image so Article builds remain reproducible.

## Prerequisites

Install [Pixi](https://pixi.sh) and ensure that `pixi` is available in the
shell. Node.js, Go, Python, PostgreSQL, and MinIO do not need to be installed
separately. For the default complete workbench, start Docker Desktop unless
the host already provides `pandoc`, `latexmk`, and `xelatex`. A base-only
environment can explicitly disable the Worker as described below.

If the package registries require an explicit proxy, configure it before the
first installation:

```powershell
$env:HTTP_PROXY = "http://127.0.0.1:16888"
$env:HTTPS_PROXY = $env:HTTP_PROXY
```

```bash
export HTTP_PROXY=http://127.0.0.1:16888
export HTTPS_PROXY="$HTTP_PROXY"
```

## Install and start

On Windows PowerShell:

```powershell
.\scripts\testenv.ps1 install
.\scripts\testenv.ps1 check
.\scripts\testenv.ps1 dev
```

On Bash:

```bash
./scripts/testenv.sh install
./scripts/testenv.sh check
./scripts/testenv.sh dev
```

`install` downloads the isolated toolchain and workspace dependencies.
`check` runs the repository validation suite and `test` runs the full test
suites, both through the isolated environment. `dev` runs in the foreground
and starts PostgreSQL, MinIO, database migrations, Core, the asynchronous
Worker, Web BFF, MCP Gateway, and Web. It also builds the local CLI and Box
binaries, writes a correctly configured CLI launcher, and publishes the
current-platform binaries to the development download center. Once it reports
that the environment is ready, open
`http://127.0.0.1:13000`.

To expose the development Web service through a temporary Cloudflare Quick
Tunnel, start Docker Desktop and append `--cf`:

```powershell
.\scripts\testenv.ps1 dev --cf
```

The supervisor starts the `cloudflare/cloudflared` container first, captures
the generated `https://*.trycloudflare.com` URL, and injects it as
`MMDASH_TESTENV_PUBLIC_URL` before starting the application services. The URL
is also printed in the `[cloudflared]` log output. The tunnel is
unauthenticated and public, and the container is removed automatically when
`dev` stops. Next's development origin allowlist is updated for the generated
hostname so HMR can connect through the tunnel. The same option works with
the Bash wrapper:

```bash
./scripts/testenv.sh dev --cf
```

The launcher reads repository-root `.env` first and lets variables already set
in the invoking shell take precedence. Required isolated service paths and
ports still override incompatible deployment values. Core public, internal,
Agent MCP, OAuth, Worker, and transfer URLs are derived from the selected Pixi
ports rather than the Docker Compose defaults.

The bootstrap account is:

```text
Email:    admin@mmdash.local
Password: mmdash-local-admin
```

Use Ctrl+C in the terminal running `dev` to gracefully stop every service.

## Worker modes

`MMDASH_TESTENV_WORKER_MODE` accepts:

| Value      | Behavior                                                                                                  |
| ---------- | --------------------------------------------------------------------------------------------------------- |
| `auto`     | Default. Use native Pandoc/LaTeX when complete; otherwise require Docker and run the pinned Worker image. |
| `native`   | Run the Python Worker through Pixi/uv. The operator is responsible for compatible Pandoc/LaTeX binaries.  |
| `docker`   | Require a running Docker daemon, build `mmdash-worker:testenv`, and run the Worker container.             |
| `disabled` | Start only the base services. Worker-backed Jobs remain queued and the launcher prints a warning.         |

The launcher issues a temporary Worker API token after Core becomes ready and
revokes it during shutdown. A preconfigured `MMDASH_WORKER_API_TOKEN` is used
as-is and is never revoked by the launcher. Docker mode binds the Core and MinIO
API listeners for container access; browser-facing services remain on loopback.
Worker Artifact transfers stream through Core, so their Core origin is
translated to `host.docker.internal` while the signed URL path and original
host header are preserved.

Container download settings are:

```text
MMDASH_TESTENV_PYPI_INDEX_URL
MMDASH_TESTENV_DEBIAN_MIRROR
MMDASH_TESTENV_DEBIAN_SECURITY_MIRROR
MMDASH_TESTENV_DOCKER_PROXY_URL
```

The Debian and Python settings default to Aliyun mirrors. The proxy value may
be an HTTP(S) origin or `none`; it is opt-in because a host proxy that works for
package managers can still reject Docker build traffic. A loopback proxy such
as `http://127.0.0.1:22334` is translated to
`http://host.docker.internal:22334` when explicitly configured.

## Development binaries

After startup, the launcher reports these paths:

```text
.testenv/runtime/bin/mmdash[.exe]
.testenv/runtime/bin/mmdash-box[.exe]
.testenv/runtime/bin/mbox[.exe]
.testenv/runtime/bin/mmdash-local[.cmd|.sh]
apps/web/public/downloads/dev/
```

Use `mmdash-local` for a CLI whose unified Web, Core, MCP, and isolated config
paths already match the active Pixi ports. The server-existing Repo provider
is enabled only below `.testenv/repositories` by default; set
`REPO_LOCAL_ALLOWED_ROOTS` before startup to use another explicit allowlist.

## Verify the complete flow

With `dev` still running, use a second terminal:

```powershell
.\scripts\testenv.ps1 smoke
```

```bash
./scripts/testenv.sh smoke
```

The smoke test covers the Web → BFF → Core → PostgreSQL chain, login and
project creation, MinIO/Data Hub access, outbox and audit behavior, MCP health,
CLI startup, metrics, and one isolated `system.test` Worker invocation. The
normal development Worker remains online for Artifact previews, Model sync,
Progress evaluation, semantic descriptions, Experiment result processing, and
Article template/preview/formal builds.

To verify full startup and immediately shut everything down:

```powershell
.\scripts\testenv.ps1 dev-check
```

```bash
./scripts/testenv.sh dev-check
```

## Ports

| Service       | Default port |
| ------------- | -----------: |
| Web           |      `13000` |
| Web BFF       |      `13001` |
| MCP Gateway   |      `13002` |
| Core          |      `18080` |
| PostgreSQL    |      `15432` |
| MinIO API     |      `19000` |
| MinIO Console |      `19001` |

These defaults intentionally do not overlap with Docker Compose. To override
a port, set the corresponding `MMDASH_TESTENV_*_PORT` variable before calling
the wrapper; every selected port must be unique and available. For example:

```powershell
$env:MMDASH_TESTENV_WEB_PORT = "13010"
.\scripts\testenv.ps1 dev
```

### Webpack fallback for the web dev server

Next 16 starts the web dev server with Turbopack by default. On hosts whose
commit memory runs out (small page files plus memory-heavy resident apps) the
Turbopack native binding can abort the process with exit code `0xC0000409`.
Set the following variable before `dev` to start the web dev server with
webpack instead; everything else in the stack is unaffected:

```powershell
$env:MMDASH_TESTENV_WEB_WEBPACK = "1"
.\scripts\testenv.ps1 dev
```

```bash
MMDASH_TESTENV_WEB_WEBPACK=1 ./scripts/testenv.sh dev
```

## Inspect, stop, and reset

```powershell
.\scripts\testenv.ps1 doctor  # tool versions, paths, ports, supervisor state
.\scripts\testenv.ps1 stop    # graceful shutdown from another terminal
.\scripts\testenv.ps1 reset   # remove only .testenv/runtime while stopped
```

```bash
./scripts/testenv.sh doctor
./scripts/testenv.sh stop
./scripts/testenv.sh reset
```

`reset` preserves downloaded tools and dependency caches. It refuses to run
while the environment or any isolated port is still in use. It deletes the
isolated PostgreSQL and MinIO data below `.testenv/runtime/data`; ordinary
`stop`, Ctrl+C, and restarts preserve that data.

Pixi isolates files and toolchains, not the operating-system kernel or network.
Repository-local build outputs such as `node_modules`, `.next`, and `dist`
remain in their normal ignored locations.
