# E2B runtime

E2B is the production Sandbox runtime adapter behind the same
`sandbox.Runtime` interface as Local Docker. Provider sandbox IDs, the E2B API
key, and per-sandbox Envd access tokens remain Box-local and never enter Core
task, result, Audit, or Data Hub contracts.

## Official protocol chain

The adapter follows the E2B JavaScript SDK `2.38.3` and provider OpenAPI/Envd
specification:

1. `POST /sandboxes` with `X-API-Key`, a fixed Template, bounded TTL,
   `secure: true`, no public inbound traffic, and the frozen network policy.
2. `GET /sandboxes/{id}` to verify Template CPU, memory, disk, and Envd
   compatibility before user code starts.
3. Envd Connect JSON calls for directory setup, direct argv process execution,
   log streaming, and process signals. No `/bin/bash -c` or caller-supplied
   shell command is exposed.
4. Envd `/files` streaming upload/download for the Repo-owned workspace and
   result files. `.git` is excluded; path, type, symlink, count, and size
   checks apply in both directions.
5. `GET /sandboxes/{id}/metrics` maps provider metrics to provider-neutral
   resource usage.
6. `DELETE /sandboxes/{id}` runs on success, failure, timeout, and
   cancellation. If a successful create response loses its sandbox identity,
   the adapter reconciles by the unique mmdash task metadata and deletes the
   matching sandbox. Provider TTL remains the final crash-only failsafe.

The stable `https://sandbox.<domain>` Envd route is used for E2B's supported
hosted domains. For a custom/self-hosted domain, the adapter follows the
official SDK behavior and uses the domain returned by sandbox creation as
`https://49983-<sandbox-id>.<domain>`. `E2B_SANDBOX_URL` is an explicit
override for deployments with a fixed proxy or local Envd endpoint.

## Credentials and self-hosting

Only `E2B_API_KEY` is sent as the platform credential. An API Key ID or Project
ID shown by the E2B dashboard is metadata and is not an API request field.

E2B can be self-hosted, but that means deploying the E2B control plane,
orchestrator, networking, storage, and sandbox hosts described by the official
[`e2b-dev/infra` self-hosting guide](https://github.com/e2b-dev/infra/blob/main/self-host.md),
not merely starting one sandbox container. Point mmdash at that deployment
with `E2B_DOMAIN` and, when needed, `E2B_API_URL` and
`E2B_SANDBOX_URL`. Self-hosted deployments may issue nonstandard key formats;
mmdash requires only a non-empty key and does not impose the hosted prefix.

## Resource behavior

E2B CPU and memory are Template-level capacities. The adapter rejects a frozen
request that exceeds the actual sandbox detail returned by E2B. PID count is
enforced for the execution user with `prlimit`; output count and bytes are
enforced while collecting `/output`; timeout and cancellation signal the
process and destroy the sandbox. `disabled` and `restricted` deny all egress in
v0.1 because the frozen contract has no allowlist; only `enabled` permits
internet access. Public inbound traffic is always disabled.

The hosted `base` Template used in the 2026-08-11 acceptance exposed one CPU,
512 MiB memory, and 10 GiB disk. Use `memory_bytes <= 536870912` with that
Template, or configure `MMDASH_E2B_TEMPLATE` to a custom Template whose
capacity covers the frozen request. The Box-wide advertised limits are also an
admission ceiling and should be set no higher than the smallest enabled
runtime capacity when a project can select multiple runtimes.

## Verification

The permanent mock exercises official Platform REST, Connect JSON framing,
secure headers, dynamic/explicit routing, uploads, downloads, logs, metrics,
timeout, cancellation, malformed-create reconciliation, output rejection,
credential redaction, and unconditional deletion:

```bash
go test -race ./box/runtimes/e2b
```

The paid live test is opt-in and creates three short-lived sandboxes for
success/artifact, timeout, and active cancellation. Export the key through the
process environment without writing it to source or shell history:

```bash
export E2B_API_KEY
E2B_LIVE_ACCEPTANCE=1 go test ./box/runtimes/e2b -run TestLiveE2BAcceptance -count=1 -v
```

The test records the sandbox count before and after and fails unless cleanup
returns it to the original value. Normal repository tests never incur E2B
charges.
