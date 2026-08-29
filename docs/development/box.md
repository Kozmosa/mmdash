# Stage 8 Box Gateway and Sandbox redesign baseline

This guide freezes the target Box boundary for the Stage 8 refactor. The
current Project-owned registration, Project-scoped Box Token, one-to-one
binding, short claim polling, lease-loss cancellation, and ephemeral callback
state are legacy behavior to migrate, not extend.

`box/` remains an independently buildable Go 1.26 module. It is an outbound
Core client and local capability controller. It never opens PostgreSQL, MinIO,
or the Core business database and never needs an inbound public endpoint.

## Conceptual boundary

```text
mmdash Core
└── Box Control (identity, assignment, scheduling, authority)

User-owned device
└── mmdash Box
    └── Box Gateway (outbound controller)
        └── Sandbox Capability
            ├── E2B Runtime Adapter
            └── Local Docker Runtime Adapter
```

Experiment selects a Runtime policy; Core schedules an eligible Box; only Box
Gateway invokes the Runtime Adapter. Core and business modules never call E2B
or Local Docker directly.

## Device authorization and ownership

Installation runs a CLI-like browser device flow:

1. Box starts Auth device authorization with `client_kind=box` and displays the
   user code and verification URL.
2. The authenticated user approves the device in Web.
3. Exchange returns a short-lived Box registration grant, not a user Session.
4. Box registers its name/version and receives a dedicated opaque Box Token
   once.
5. Box persists only Box ID and Token in a user-only state file.

`box_nodes.owner_user_id` is authoritative. One Box can be assigned to many
Projects and one Project can bind many Boxes. The token is account-level; each
claim/callback is still restricted to tasks from currently assigned Projects.

Personal Box Management lists, renames, inspects, drains, force-revokes, and
shows assignments. Project Box Settings assigns/removes Boxes. Project owner,
maintainer, and editor can use assigned Boxes; editor cannot change assignment.
When a Box owner leaves a Project, only that Project assignment is force
removed and its active Box tasks fail; the global Box and other Projects remain.

## Installer releases and first start

Box installers are ordinary immutable Artifact Versions in the hidden mmdash
system Artifact Project. A release must be tagged `box-release`,
`platform:windows` or `platform:linux`, and `version:x.y.z`; the Artifact name
should include the platform and architecture. The read-only `box.releases.list`
catalog signs the current Version for each platform, so the Box Management page
can show a short-lived download button without exposing the system Project or a
permanent object-storage URL. Publishing a new Version preserves every older
Version for audit and rollback.

After downloading, use the cross-platform `mbox` command. The first-run setup
is deliberately terminal based so the same workflow works on Windows and
Linux:

```text
mbox setup [--root PATH]
mbox account login
mbox config show
mbox service init
mbox service status
```

`mbox setup` guides the operator through the public Box Control address (both
`http://` and `https://` are accepted), Box name, Local Docker image, and the
hosted or self-deployed E2B adapter settings. `mbox account login` starts the
one-time browser device flow, registers the account-owned Box, and persists the
opaque Box Token under the selected Box root. `mbox config set` updates any
Runtime/adapter setting without editing JSON by hand. `mbox service init`
registers an auto-start Windows service or a systemd unit; `start`, `stop`, and
`status` operate on that registration.

The default root is `%LocalAppData%\\MMDash Box` on Windows and
`$XDG_DATA_HOME/mmdash-box` (or `~/.local/share/mmdash-box`) on Linux. Use
`--root` to choose another directory. Configuration, state, logs, task spools,
outputs, and workspace transfer data remain below that root. `mbox uninstall`
stops and removes the registered service before deleting the initialized Box
root; it does not touch the user's project repository. E2B secrets remain on
the Box host and are never uploaded to mmdash.

## Outbound control protocol

Gateway sends heartbeat and uses a maximum 60-second bounded long poll for
task claims. A claim freezes Box ID, Runtime/version, execution epoch, and
RunSpec. PostgreSQL remains the queue and Core claims with
`FOR UPDATE SKIP LOCKED`.

Gateway callbacks are idempotent:

- heartbeat advertises version, capabilities, runtimes, limits, and load;
- resume handshake sends task, execution epoch, local phase, last local log
  sequence, Bundle state, and callback acknowledgements;
- log upload sends bounded ordered batches and Core returns the highest
  contiguous accepted sequence;
- status, Artifact, Bundle, and result callbacks reject stale epochs and
  force-revoked credentials.

The Box host requires only outbound HTTPS to mmdash and configured Providers.
NAT, private addresses, and loss of public inbound reachability are supported.

## Offline execution and durable spool

Every claimed task has durable local state. The root `state.json` persists
the durable identity and every task's phase, execution epoch and log sequence
bookkeeping, acknowledged callbacks, bounded log spool, and Bundle/Manifest
pointers; task outputs live under `outputs/`:

```text
{box root}
├── state.json
└── outputs/
    ├── {task_id}/logs/stdout.log, stderr.log
    ├── {task_id}/manifest.json
    └── {task_id}-execution-bundle.zip
```

Logs carry task ID, execution epoch, monotonic sequence, source timestamp,
stream (`stdout | stderr | system`), and payload. Core deduplicates on
`(task_id, execution_epoch, sequence)`.

Network loss never cancels a running Runtime. Gateway continues execution,
spools logs/status/results, and resumes from Core's last acknowledgement after
reconnect or process restart. When the configured local log budget is
exhausted, Runtime continues, new log bytes are discarded, and Gateway durably
records `logs_truncated=true`, truncation time, and last sequence.

Core marks connectivity `box_offline` separately from execution phase. At 72
hours offline it fails the Experiment with `BOX_OFFLINE_TIMEOUT`; late logs may
be retained as `late_after_failure`, but late Bundle/result cannot become a
valid result commit.

## Draining and force revocation

Normal revoke changes Box to `draining`, disables new claims, lets every
claimed task finish through result binding, then revokes Token and assignments.

Force revoke immediately expires the token. Unclaimed tasks return to normal
scheduling; preparing/running/uploading tasks fail with `BOX_FORCE_REVOKED`.
If result processing already pushed but did not bind a Commit, Repo pushes a
compensating revert. Box cleanup is recorded even if the offline device cannot
be contacted.

## Sandbox and Runtime security

Sandbox accepts only the frozen entrypoint contract and maps it to an argv
vector. It has no arbitrary shell or caller-supplied command. Manifest paths
are relative regular files and reject traversal, symlink escape, duplicate
paths, size/hash mismatch, excessive count, and excessive expansion.

Adapters are compiled into Box but advertised only after availability probes.

- E2B is preferred for `auto`. The same Adapter supports hosted E2B and a full
  self-hosted E2B deployment through local API/Sandbox URL, domain, Template,
  and API Key configuration. Secrets never leave Box.
- Local Docker is a development, lightweight self-hosted, and explicitly
  configured fallback. It needs a local Docker daemon; ordinary Box
  installation does not require Docker.
- Local Process is a trusted-host bare-metal Runtime for hosts without
  Docker. It is disabled by default, never selected by `auto`, and must be
  enabled explicitly (`mbox setup --enable-local-process --local-process-python
  <path>` or `mbox config set local-process.enabled true`). See the next
  section for its supervision and isolation characteristics.

The Local Docker Adapter retains read-only source, dropped capabilities,
`no-new-privileges`, CPU/memory/PID/time/network/disk bounds, a bounded output
mount, and fixed ENTRYPOINT replacement. On Windows Docker Desktop the disk
bound is skipped because `--storage-opt` requires XFS project quotas; the
output and log budgets guard disk usage there. Docker socket access remains an
explicit high-privilege deployment choice.

## Local Process supervision

Local Process executes a task as a supervised process directly on the Box
host. Because the host kernel and filesystem are shared, it provides **no
container-equivalent isolation** and serves only experiments that explicitly
select `runtime_policy: local-process` with `network: enabled` — the Runtime
rejects every other network policy instead of degrading silently.

The product support target is **Linux** (VMs/containers without nested
virtualization); Windows is not a support target — use Docker Desktop
(`local-docker`) or run the Box inside WSL. The Windows Job Object path below
is a best-effort implementation for development/testing only (see ADR 0005 in
`docs/adr/0005-local-process-linux-support-scope.md`).

One detached runner process (`mmdash-box task-runner --state-dir ... --task-id
...`) exists per task. It starts the frozen entrypoint from an argument array
without a shell, applies the frozen hard limits to the complete process tree
(CREATE_SUSPENDED plus Job Object CPU/memory/PID limits before the first task
instruction on Windows; a fresh cgroup v2 subtree whose limits are written
before the process is moved into it on Linux, which leaves a brief
pre-enforcement start window), and enforces timeout and the cancel sentinel
over the whole tree. Task stdout/stderr are captured into runner-owned
`task-stdout.log` / `task-stderr.log` files.

Supervision state is durable under `<state-root>/runner/<task>/state.json`
(boot ID, runner/task PID, state, exit code, and the launch failure reason for
`RUNNER_FAILED`). A restarted Gateway reattaches to a live runner; a different
boot ID terminates the recorded task with `HOST_RESTARTED`; a dead supervisor
on the same boot yields `RUNNER_LOST` — reattach terminates the surviving
tree immediately, and a Gateway following a live task fails with `RUNNER_LOST`
after a short grace period once the supervisor disappears, because a dead
supervisor enforces neither timeout nor cancellation (on Windows the Job
Object additionally kills the whole tree when the supervisor dies). Output
replays from persisted spool byte offsets, so gap output survives Gateway
restarts.

Python environments use a content-addressed cache keyed by builder strategy,
platform, interpreter and installer identities, and manifest paths + content
hashes. Manifest discovery is allowlist-only at the workspace root
(`requirements.lock`, hash-pinned `requirements.txt`, `uv.lock`,
`poetry.lock`, `Pipfile.lock`; conda returns
`ENVIRONMENT_MANIFEST_UNSUPPORTED`, multiple families
`ENVIRONMENT_MANIFEST_AMBIGUOUS`). Builds run in a temp directory with atomic
publish; failed builds are never cached. The result Manifest reports
`provider: local-process`, interpreter identity as the base identity, the
prepared environment identity, and the resolved dependency list.

`config.LocalProcess.User` is accepted in configuration but not yet applied
when spawning the runner; low-privilege execution is still TODO.

## Environment preparation and Local Docker cache

`preparing` is a durable task phase owned by the Gateway. After a claim, the
Gateway uses the short-lived, read-only `source_transfer` from Repo/Core to
download the exact frozen `source_commit` into the Box host's task workspace.
It verifies the Commit, transfer URL expiry and size limits, and archive path
boundaries before the workspace can be used. The Runtime never receives Git credentials and never
clones the repository from inside a container. The verified source directory
is mounted into the execution container as `/workspace:ro`; it is not copied
into an environment image.

The Gateway scans only a versioned allowlist of environment manifests. The
first complete Local Docker slice supports:

- Python: a hash-pinned `requirements.lock`.

Known but not yet implemented combinations such as `pyproject.toml` with
`uv.lock`, Node lockfiles, `go.mod`/`go.sum`, and a controlled
`.mmdash/environment.yaml` must fail with `ENVIRONMENT_INVALID`
instead of being ignored or interpreted heuristically. They can be added as
separate builder versions with their own lockfile and integrity rules.

Other files, arbitrary Dockerfiles, CI scripts, shell setup
commands, and unrecognized package-manager files do not affect preparation.
Conflicting manifests fail preparation with an auditable reason instead of
choosing implicitly.

For Local Docker, the cache key contains at least:

```text
base_image_id_or_digest
platform_and_architecture
runtime_version
environment_manifest_paths_and_content_hashes
builder_version
package_index_configuration_version
```

The key is scoped to the Box and its build configuration. A single-flight lock
ensures concurrent tasks build one environment per key and wait for that
result. The image is usable only after a successful build;
the final image ID/digest and `last_used_at` are recorded. A failed build never
replaces a previous ready image and never enters the reusable cache. Build and
dependency-install output is emitted as `system` stream entries, persisted in
the durable spool, and visible in the read-only Experiment Terminal.

The resulting image contains runtime and dependencies only. The source and
result mounts remain separate. Dependency installation may use a bounded,
temporary package-index network during preparation according to Box policy;
the run phase returns to the requested network restriction. Lockfiles and
integrity hashes remain mandatory.

Every successful preparation writes reproducibility evidence to the run
summary and `manifest.json`, which is included in the Execution Bundle and
therefore persisted by Core/Artifact:

```text
environment.environment_key
environment.base_image_id
environment.environment_image_id
environment.manifest_paths / environment.manifest_hashes
environment.builder_version
environment.cache_hit
```

E2B always creates from the configured published Template, records no dynamic
environment fields, and never invokes Local Docker's dynamic image builder or
its local cache/GC path.

Box GC runs during environment preparation and runtime probes. It may delete
only an image built
and marked as managed by that Box, using its exact image ID, when it has had no
reference for four consecutive days. Images under build, running, uploading,
or any other active task reference are retained. User-provided base images and
images owned by another tool are never GC targets. Removing a local cache entry
does not remove the environment evidence already stored in an Execution Bundle.

## Target API delta

Replace legacy registration requiring `project_id` and singular Project `/box`
binding with:

```text
POST  /v1/boxes                                  account registration grant
GET   /v1/users/me/boxes                         personal inventory
GET   /v1/users/me/boxes/{boxId}                 personal detail
PATCH /v1/users/me/boxes/{boxId}                 rename
POST  /v1/users/me/boxes/{boxId}/revoke          drain | force

GET    /v1/projects/{projectId}/boxes            assigned inventory
PUT    /v1/projects/{projectId}/boxes/{boxId}     assign
DELETE /v1/projects/{projectId}/boxes/{boxId}     remove assignment
```

The source OpenAPI change must freeze these operation shapes:

- `auth.device.authorize` accepts `client_kind="box"`; the matching
  `auth.device.token` exchange returns
  `{registration_grant, grant_expires_at}` instead of a CLI Session. The grant
  is single-use, short-lived, user-bound, and accepted only by `box.register`.
- `box.register` accepts
  `{installation_id, name, version}` and returns
  `{box, box_token, token_expires_at}`. `installation_id` is the owner-scoped
  idempotency key; `box_token` is returned once and the request has no
  `project_id`.
- `box.personal.list/get/update` returns only Boxes owned by the current user.
  Update accepts `{name}`. `box.revoke` accepts `{mode: "drain" | "force"}`
  and returns the resulting Box plus drain progress instead of using HTTP
  `DELETE` for both modes.
- `box.project.list` returns assignment plus safe Box status/capability/load.
  `box.project.assign` and `box.project.remove` have no secret material and
  return the authoritative binding/removal outcome. Repeated calls are
  idempotent.

Freeze the Box-authenticated control operations as:

```text
POST /v1/boxes/{boxId}/heartbeat
POST /v1/boxes/{boxId}/tasks/claim
POST /v1/boxes/{boxId}/tasks/{taskId}/resume
POST /v1/boxes/{boxId}/tasks/{taskId}/logs
POST /v1/boxes/{boxId}/tasks/{taskId}/status
POST /v1/boxes/{boxId}/tasks/{taskId}/artifact
POST /v1/boxes/{boxId}/tasks/{taskId}/result
```

- claim accepts `{wait_seconds}` with `0..60`; `200` returns a frozen task with
  `execution_epoch`, RunSpec, source transfer grant, environment descriptor and
  result contract, while `204` means no work;
- resume accepts `{execution_epoch, local_phase, last_local_sequence,
bundle_state, acknowledged_callbacks}` and returns `{action, accepted_phase,
accepted_through_sequence}` where `action` is `continue | stop_failed |
stop_canceled | cleanup`;
- logs accepts `{execution_epoch, first_sequence, entries,
logs_truncated, logs_truncated_at}` with bounded ordered entries and returns
  `{accepted_through_sequence}`; duplicate batches return the same ack;
- status accepts `{execution_epoch, phase, occurred_at, runtime_details,
failure}` and can only advance the frozen attempt;
- artifact streams `execution-bundle.zip` with execution epoch, SHA-256 and
  size headers, returning the immutable Artifact Version pointer;
- result references the accepted Bundle and Manifest hash; it never sends a
  result commit directly from Box.

Every callback validates Box Token, Box ID, assigned Project, Task,
Experiment, execution epoch, current revocation state and idempotency identity.
Responses use the repository standard error envelope and stable codes. No
callback accepts a user Session or long-lived Git/Artifact credential.

Keep Box-authenticated heartbeat/task callbacks but revise schemas for account
identity, assignment checks, long poll, execution epoch, resume, batch logs,
acknowledgements, truncation, and late diagnostics. Update source OpenAPI,
examples, mocks, event schemas/catalog, generated clients, BFF routes, and API
catalog in the same contract change.

## Target data and migration

`000043_box_account_nodes` follows current `000042_article_module` and must not
rewrite `000041_stage8_box_experiment`.

It must:

- add/backfill owner from the legacy creator and remove Project ownership from
  the Box identity;
- replace one-row-per-Project binding assumptions with composite
  `(project_id, box_id)` authority;
- add `draining`, offline timestamps, durable execution epoch/log sequence,
  truncation, resume, and callback state;
- preserve historical Box/Task/Audit references;
- require active legacy Boxes to complete the new device authorization/token
  transition rather than inventing a recoverable secret;
- support fresh, existing, partial, concurrent, and down/up tests.

Project purge later removes assignments and Project-owned task/log data but
does not delete the user-owned Box. Global Box revoke removes all assignments.

## Implementation order

1. Change source contracts and generated clients.
2. Add migration/backfill and dual-state domain tests.
3. Extend Auth device flow for Box registration grants.
4. Refactor Core ownership, many-to-many assignment, scheduling, drain/force,
   offline timeout, resume, and log acknowledgement.
5. Refactor Gateway state storage and remove lease-loss Runtime cancellation.
6. Add host-side source transfer verification, allowlisted environment
   preparation, Local Docker single-flight image cache/GC, and reproducibility
   evidence in Manifest/Execution Bundle.
7. Add hosted/self-hosted E2B probes and explicit Local Docker availability;
   E2B remains Template-based and does not use dynamic image preparation.
8. Add personal and Project Box management UI/BFF.
9. Integrate Experiment settings and result flow.
10. Run full Box offline/restart/reconnect, multi-Project fairness, revoke, purge,
    environment cache hit/miss/failure/GC, hosted E2B, self-hosted route mock,
    and Local Docker acceptance.

## Verification

Focused checks after implementation:

```bash
pnpm contracts:generate
pnpm contracts:check
pnpm api:check
go test ./backend/internal/auth ./backend/internal/boxcontrol ./backend/internal/experiment
go test ./box/...
pnpm --filter @mmdash/web-bff test
```

Then run `pnpm check` and the Docker acceptance paths from `AGENTS.md`. Paid
hosted-provider acceptance remains opt-in and must use short-lived credentials.
