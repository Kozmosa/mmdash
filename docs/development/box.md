# Stage 8 Box Gateway and Sandbox

`box/` is an independently buildable Go 1.26 module. It is an outbound Core
client plus a bounded task executor; it never opens PostgreSQL, MinIO, or the
Core business database. The HTTP client only speaks the frozen Box Control
routes and sends a Box-scoped short-lived token.

## Gateway boundary

The gateway registers once, persists only its Box ID and token in a user-only
state file, heartbeats capabilities/load, claims one leased task at a time by
default, renews the lease, streams bounded logs, reports status, uploads
`artifact.zip`, and reports the validated manifest. Per-task output and the
staged archive are removed after every terminal path. Repeated callbacks are
safe at Core.

The default `StaticWorkspace` mode consumes a Repo-owned detached checkout
mounted by the operator. It requires both `MMDASH_BOX_WORKSPACE_COMMIT` and a
`.mmdash-commit` marker in the mounted directory to equal the frozen
`source_commit`; it never follows a branch or executes Git. A deployment that
needs remote checkout transfer must provide a reviewed Repo checkout transport
before enabling it; Box does not receive a long-lived Git credential.

## Sandbox security

Sandbox accepts only the frozen entrypoint contract and maps it to an argv
vector. It has no arbitrary shell or caller-supplied command field. Manifest
paths are relative, regular files only, and are checked for symlink escape,
zip-slip, duplicate names, size, and SHA-256 before packaging.

Local Docker uses a preconfigured image, read-only workspace, dropped Linux
capabilities, `no-new-privileges`, CPU/memory/PID/time/network limits, a
bounded writable output mount, and a disk quota. `disabled` and `restricted`
network policies use Docker `none`; only explicit `enabled` uses the bridge
network.

The E2B package implements the same `sandbox.Runtime` interface and is tested
against a provider-neutral fake for run/cancel/destroy semantics. It does not
claim real E2B acceptance without an external E2B account and credential.

## Configuration

| Variable | Purpose |
| --- | --- |
| `MMDASH_CORE_URL` | Core Box Control origin |
| `MMDASH_BOX_PROJECT_ID` | Project scope for registration |
| `MMDASH_BOX_REGISTRATION_TOKEN` | One-time Core-issued registration credential |
| `MMDASH_BOX_WORKSPACE` | Repo-owned detached checkout mount |
| `MMDASH_BOX_WORKSPACE_COMMIT` | Commit recorded in the checkout marker |
| `MMDASH_BOX_LOCAL_IMAGE` | Predefined Sandbox image |
| `MMDASH_BOX_STATE_PATH` | User-only restart state file |

When using the optional Compose profile with Local Docker, set `DOCKER_GID`
to the host Docker socket group ID. Mounting `/var/run/docker.sock` grants the
Box control over the host Docker daemon and must be treated as a deliberate
deployment privilege.

Build and test the independent module with:

```bash
go test ./box/...
go build ./box/cmd/mmdash-box
```
