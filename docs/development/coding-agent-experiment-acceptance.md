# Coding Agent Local Docker Experiment acceptance

`scripts/coding-agent-experiment-smoke.mjs` is a repeatable development-only
acceptance path for the environment-preparation flow. It acts as a local
Coding Agent: it creates a temporary Project and local Git remote containing a
small Python program and a non-empty, hash-pinned `requirements.lock`, calls
`experiment.create` and `experiment.run` through the MCP Gateway, polls
`experiment.status` and its persisted logs, and verifies `result.get`.

The script prefers an already-online, account-owned Box advertising the
`local-docker` Runtime. If none is available, it can start a temporary native
Box Gateway unless `MMDASH_ACCEPTANCE_REQUIRE_EXISTING_BOX=true` is set. The
selected Box downloads the immutable source transfer for the frozen Commit,
prepares or reuses the local environment image, runs the fixed
`python:run.py` entrypoint, and uploads the result Bundle. The Project
assignment and temporary source tree are removed during cleanup; an existing
Box is never revoked.

## Run it with the local development supervisor

In terminal 1, start the complete native development stack and leave it
running:

```powershell
$env:REPO_LOCAL_ALLOWED_ROOTS = (Join-Path (Get-Location) ".tmp")
.\.localscripts\dev.ps1
```

The native Core process disables the Local Git provider when
`REPO_LOCAL_ALLOWED_ROOTS` is empty. The acceptance script creates its
temporary bare remote below `.tmp`, so the allowlist must be present before
starting the supervisor. This is the only extra development prerequisite; the
stack is still started by `.\.localscripts\dev.ps1`.

The supervisor prints the compiled Box path, normally
`.tmp\dev-tools\mmdash-box.exe`. In terminal 2, from the repository root:

```powershell
node scripts/coding-agent-experiment-smoke.mjs
```

To exercise an already-bound local Box, start its persisted configuration in
another terminal and require the acceptance script to use an existing Box:

```powershell
mbox gateway
$env:MMDASH_ACCEPTANCE_REQUIRE_EXISTING_BOX = "true"
$env:AUTH_BOOTSTRAP_EMAIL = "your-account@example.com"
$env:AUTH_BOOTSTRAP_PASSWORD = "<local-development-password>"
node scripts/coding-agent-experiment-smoke.mjs
```

The defaults match `.localscripts/dev.mjs`: Core `http://127.0.0.1:8080`, Web
BFF `http://127.0.0.1:3001`, and MCP Gateway
`http://127.0.0.1:3002/mcp`. The bootstrap account is
`admin@mmdash.local` / `mmdash-local-admin` unless the supervisor's
`AUTH_BOOTSTRAP_EMAIL` and `AUTH_BOOTSTRAP_PASSWORD` overrides are set.

The host must have a working Docker daemon and the configured Local Docker
base image. The default is `python:3.12-slim`; pull it before the run if it is
not already cached:

```powershell
docker pull python:3.12-slim
```

The fixture contains NumPy and Matplotlib constraints. Before committing it,
the script runs `uv pip compile --generate-hashes` for Python 3.12 on x86-64
manylinux. The Local Docker builder then installs the resulting non-empty lock
with pip `--require-hashes`, so package-index access is required. Constraints
can be overridden, but must not be empty:

```powershell
$env:MMDASH_ACCEPTANCE_PYTHON_DEPENDENCIES = "numpy>=2,<3`nmatplotlib>=3,<4"
$env:MMDASH_ACCEPTANCE_LOCAL_IMAGE = "python:3.12-slim"
node scripts/coding-agent-experiment-smoke.mjs
```

The experiment imports both libraries, performs a NumPy calculation, renders
`figures/dependency-plot.png` through Matplotlib's non-interactive `Agg`
backend, prints the resolved versions to stdout, and records them in
`summary.md`.

Useful overrides are `MMDASH_ACCEPTANCE_BFF_URL`,
`MMDASH_ACCEPTANCE_CORE_URL`, `MMDASH_ACCEPTANCE_MCP_URL`,
`MMDASH_ACCEPTANCE_BOX_BINARY`, `MMDASH_ACCEPTANCE_BOX_ID`,
`MMDASH_ACCEPTANCE_REQUIRE_EXISTING_BOX`, and
`MMDASH_ACCEPTANCE_TIMEOUT_SECONDS`.

The script expects at least one persisted `system` log mentioning environment
or dependency preparation, the program's `MMDASH_CODING_AGENT_STDOUT` marker,
and a successful result containing both `summary.md` and
`figures/dependency-plot.png`. A failure leaves the concise last status in the
error message; inspect the Box output printed by the script for preparation
failures, then stop the supervisor with Ctrl+C. PostgreSQL and MinIO remain
available as documented by `.localscripts/dev.mjs`; use
`.\.localscripts\dev.ps1 --down` only when those development containers should
also be stopped.
