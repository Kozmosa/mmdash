# Native development environment

This guide starts the complete local baseline without Docker. Pixi is the only
global prerequisite: it provides and isolates Node.js, Go, Python,
PostgreSQL, MinIO, and `uv`. All installed tools, caches, service data, logs,
and temporary files stay below the repository-local `.testenv` directory.

## Prerequisites

Install [Pixi](https://pixi.sh) and ensure that `pixi` is available in the
shell. Docker, Node.js, Go, Python, PostgreSQL, and MinIO do not need to be
installed separately.

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
and starts PostgreSQL, MinIO, database migrations, Core, Web BFF, MCP Gateway,
and Web. Once it reports that the environment is ready, open
`http://127.0.0.1:13000`.

The bootstrap account is:

```text
Email:    admin@mmdash.local
Password: mmdash-local-admin
```

Use Ctrl+C in the terminal running `dev` to gracefully stop every service.

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
CLI startup, metrics, and one Worker job. The long-running development
supervisor does not keep a Worker process running; `smoke` starts one isolated
Worker invocation when it validates job processing.

## Ports

| Service | Default port |
| --- | ---: |
| Web | `13000` |
| Web BFF | `13001` |
| MCP Gateway | `13002` |
| Core | `18080` |
| PostgreSQL | `15432` |
| MinIO API | `19000` |
| MinIO Console | `19001` |

These defaults intentionally do not overlap with Docker Compose. To override
a port, set the corresponding `MMDASH_TESTENV_*_PORT` variable before calling
the wrapper; every selected port must be unique and available. For example:

```powershell
$env:MMDASH_TESTENV_WEB_PORT = "13010"
.\scripts\testenv.ps1 dev
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
while the environment or any isolated port is still in use.

Pixi isolates files and toolchains, not the operating-system kernel or network.
Repository-local build outputs such as `node_modules`, `.next`, and `dist`
remain in their normal ignored locations.
