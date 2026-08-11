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

Registration creates a dedicated project-scoped Box credential. Operators
retire a Box with `DELETE /v1/boxes/{boxId}` after active tasks have reached a
terminal state. Core atomically marks the Box revoked, removes its Project
binding, records Audit/Outbox lifecycle evidence, and then asks the Auth owner
to revoke the credential. The normal Stage 8 terminal smoke performs this
cleanup and treats a leftover active Box as an acceptance failure.

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

The E2B package implements the same `sandbox.Runtime` interface using the
official Platform REST, Envd Connect JSON, and `/files` HTTP chain. Its
permanent provider mock covers create/detail/metrics/delete, secure routing,
workspace transfer, direct argv, logs, output collection, timeout,
cancellation, malformed-create reconciliation, credential redaction, and
cleanup. A paid hosted acceptance on 2026-08-11 passed success/artifact,
two-second timeout, active cancellation, and final zero leaked sandboxes. The
complete product chain was then rerun through Core, a registered Box Gateway,
E2B, Artifact, and Result retrieval; automatic Box/Token cleanup and the
official provider list both finished at zero active resources.

E2B can be self-hosted only by operating the complete E2B infrastructure, not
by starting a standalone sandbox container. `E2B_DOMAIN`, `E2B_API_URL`, and
`E2B_SANDBOX_URL` support hosted, BYOC, and self-hosted routing. With no fixed
Sandbox URL, custom domains use the sandbox-specific domain returned by the
create API, matching the official SDK. Only `E2B_API_KEY` is sent; dashboard
API Key IDs and Project IDs are not request credentials.

## Configuration

| Variable | Purpose |
| --- | --- |
| `MMDASH_CORE_URL` | Core Box Control origin |
| `MMDASH_BOX_PROJECT_ID` | Project scope for registration |
| `MMDASH_BOX_REGISTRATION_TOKEN` | One-time Core-issued registration credential |
| `MMDASH_BOX_WORKSPACE` | Repo-owned detached checkout mount |
| `MMDASH_BOX_WORKSPACE_COMMIT` | Commit recorded in the checkout marker |
| `MMDASH_BOX_LOCAL_IMAGE` | Predefined Sandbox image |
| `MMDASH_BOX_LOCAL_USER` | Optional numeric `uid:gid`; defaults to the Box process user |
| `MMDASH_BOX_STATE_PATH` | User-only restart state file |
| `MMDASH_BOX_CPU_MILLIS` | Box-wide CPU admission ceiling |
| `MMDASH_BOX_MEMORY_BYTES` | Box-wide memory admission ceiling |
| `MMDASH_BOX_TIMEOUT_SECONDS` | Box-wide execution timeout ceiling |
| `MMDASH_BOX_DISK_BYTES` | Box-wide output/disk admission ceiling |
| `MMDASH_BOX_PIDS` | Box-wide process ceiling |
| `MMDASH_BOX_NETWORK` | Maximum network policy: disabled/restricted/enabled |
| `MMDASH_BOX_MAX_CONCURRENT` | Concurrent task capacity |
| `E2B_API_KEY` | Secret platform credential; enables E2B advertisement |
| `E2B_DOMAIN` | Hosted or self-hosted base domain; default `e2b.app` |
| `E2B_API_URL` | Optional Platform API origin override |
| `E2B_SANDBOX_URL` | Optional fixed Envd/proxy origin override |
| `MMDASH_E2B_TEMPLATE` | Fixed E2B Template ID or alias; default `base` |
| `MMDASH_E2B_USER` | Unprivileged execution user; default `user` |
| `MMDASH_E2B_ADMIN_USER` | Setup user; default `root` |
| `MMDASH_E2B_REQUEST_TIMEOUT` | Platform/setup request timeout |
| `MMDASH_E2B_CLEANUP_TIMEOUT` | Bounded delete/reconciliation timeout |
| `MMDASH_E2B_SANDBOX_GRACE` | Provider TTL grace beyond task timeout |

E2B is omitted from Box heartbeats when `E2B_API_KEY` is empty. The hosted
`base` Template exposed 512 MiB in the acceptance environment, so experiments
using it must request at most `536870912` memory bytes. Custom Templates may
provide different CPU/memory capacity; the adapter reads the created sandbox
detail and rejects requests that exceed it. Keep Box-wide advertised limits no
higher than the capacity operators intend every enabled runtime to accept.

When using the optional Compose profile with Local Docker, set `DOCKER_GID`
to the host Docker socket group ID. Mounting `/var/run/docker.sock` grants the
Box control over the host Docker daemon and must be treated as a deliberate
deployment privilege.

Build and test the independent module with:

```bash
go test ./box/...
go build ./box/cmd/mmdash-box
```

Run the opt-in paid provider acceptance only with a short-lived credential in
the process environment:

```bash
export E2B_API_KEY
E2B_LIVE_ACCEPTANCE=1 go test ./box/runtimes/e2b -run TestLiveE2BAcceptance -count=1 -v
```
