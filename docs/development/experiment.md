# Stage 8 Experiment redesign baseline

This guide freezes the target Experiment boundary for the Stage 8 refactor.
The legacy implementation was a migration source only: it had one Box-only
lifecycle, hard-coded Web defaults, an Artifact-only result pointer, no
durable offline state, no self-run binding, and no result-branch product
workflow. Do not extend those assumptions.

The authoritative product requirements are the Stage 8 sections of the three
documents indexed by `docs/design/v0.1/README.md`. This guide turned that
design into an implementation handoff; the source OpenAPI and JSON Schema
contracts now carry the refactored model.

## Ownership and immutable identity

Experiment owns one immutable execution request and its final result binding.
It does not own Box identity, Git operations, Artifact bytes, Worker execution,
or Agent conversation state.

Every Experiment freezes:

- `experiment_id`, Project, creator, name, and `experiment_type`;
- source commit, fixed entrypoint, parameters, environment, inputs, and limits;
- environment manifest selection and preparation policy;
- requested Runtime policy and optional requested Box;
- Project timezone and `experiments/{exp_id}_{yyyymmdd_hhmm}/` result directory;
- idempotency key and creation time.

`experiment_type` is one of:

- `box`: first managed Box execution;
- `box-re`: a human-requested rerun with a new Exp ID;
- `self`: a Coding Agent executes outside Box and binds a pushed result commit.

Before final confirmation, a rerun copies the old request and permits edits.
After confirmation, it freezes like every other Experiment.

## State model

Execution and connectivity are separate dimensions.

Managed Box execution:

```text
created -> queued -> preparing -> running -> uploading
        -> processing_result -> succeeded
        -> failed | canceled | timed_out
```

Self execution:

```text
created -> awaiting_result -> verifying_result -> succeeded
        -> failed | canceled
```

Connectivity is `online | box_offline`. `preparing`, `running`, or `uploading`
may coexist with `box_offline`. A transient disconnect never cancels a Runtime,
renews/reassigns the task, or starts a duplicate attempt. At 72 hours of
continuous disconnection Core terminates the Experiment with
`failure_code=BOX_OFFLINE_TIMEOUT`. Once Core has accepted the complete
Execution Bundle, server-side result processing no longer depends on Box
connectivity.

Runtime execution failures are not retried automatically. A human rerun creates
a `box-re` Experiment and records:

```text
retry_of_experiment_id
root_experiment_id
superseded_by_experiment_id
latest_experiment_id
retry_sequence
```

Old IDs remain readable. `experiment.status`, `result.get`, and `data.read`
return the old record plus `EXPERIMENT_HAS_NEWER_RETRY`, the latest ID, and the
full retry chain.

## Failure record

Every terminal failure stores:

- `failure_stage`: `scheduling`, `box_preparation`, `runtime_execution`,
  `result_upload`, `bundle_validation`, `artifact_storage`,
  `result_processing`, `repo_commit`, `repo_push`, `result_binding`, or
  `box_revocation`;
- stable `failure_code` and safe `failure_message`;
- `failed_at`, actual Box, Runtime/version, task attempt, and retryability;
- last accepted log sequence, `logs_truncated`, and truncation time;
- partial or complete Execution Bundle pointer when one exists;
- staging/result/revert Commit identifiers and structured cleanup result.

Minimum new codes are `NO_ELIGIBLE_BOX`, `BOX_OFFLINE_TIMEOUT`,
`BOX_FORCE_REVOKED`, `RUNTIME_UNAVAILABLE`, `RUNTIME_EXIT_NONZERO`,
`RUNTIME_TIMED_OUT`, `RESULT_UPLOAD_FAILED`, `RESULT_INVALID`,
`ENVIRONMENT_INVALID`, `ENVIRONMENT_BUILD_FAILED`,
`ENVIRONMENT_UNAVAILABLE`,
`ARTIFACT_ARCHIVE_FAILED`, `RESULT_PROCESSING_FAILED`, `REPO_COMMIT_FAILED`,
`REPO_PUSH_FAILED`, and `RESULT_BINDING_FAILED`.

## Runtime selection and Project settings

Project Settings registers an Experiment definition with:

- IANA timezone;
- default Runtime policy: `auto | e2b | local-docker | local-process`;
- CPU, memory, timeout, disk, PID, and network defaults;
- Git large-file threshold bounded by the provider limit.

An Experiment may override defaults within Box capability. `auto` tries an
E2B-capable Box first and falls back to Local Docker only before scheduling is
frozen; `auto` never selects a `local-process` Box. Explicit `e2b`,
`local-docker`, or `local-process` never falls back. Among eligible bound
Boxes the scheduler chooses the lowest load unless the caller pins one Box.

`local-process` is a trusted-host bare-metal Runtime: the task runs as a
supervised process directly on the Box host with no container isolation, it
only serves tasks that request `network: enabled`, and Boxes advertise it
explicitly (disabled by default). It enforces timeout, cancellation, and
CPU/memory/PID limits over the whole process tree through cgroup v2 (Linux)
or Job Objects (Windows), and records a durable per-task state so a Gateway
restart reattaches while a host reboot yields the stable `HOST_RESTARTED`
failure. Because the host is shared, only trusted Boxes and code should opt
in; the Web UI shows a corresponding warning when this Runtime is selected.

## Environment preparation contract

For a managed Box run, `preparing` is an explicit stage between queue claim and
runtime execution. The Gateway uses the short-lived, read-only
`source_transfer` to download and verify the exact frozen `source_commit` on
the Box host, scans only allowlisted environment manifests, then calculates an
environment key. The verified source is mounted read-only into the execution
container; it is never cloned inside the container or copied into an
environment image.

For Local Docker, the key includes the immutable base-image identity,
platform/architecture, runtime version, manifest paths and content hashes,
builder version, and package-index configuration version. Same-key tasks use a
single-flight build or cache hit. A failed build never replaces a ready image.
Build/dependency output is a `system` stream and appears in the read-only
Terminal. E2B always uses a published Template and does not dynamically build
an image in this stage.

The run summary and `manifest.json` must retain reproducibility evidence in the
Execution Bundle: `environment_key`, base image ID/digest, final environment
image ID/digest (or E2B Template ID/version), manifest paths/hashes,
`builder_version`, and `cache_hit`. Thus local cache GC cannot remove the
audit trail. Box may GC only Box-managed Local Docker images with no active
reference and no use for four consecutive days.

## Managed result pipeline

Box uploads one immutable `execution-bundle.zip` containing `manifest.json`,
summary, logs, figures, tables, data, and models. This raw evidence is an
Artifact retained until the Project recycle period expires. It is not the
result-branch representation.

The server-side path is:

```text
Box upload
  -> Core bounded staging and manifest/path/hash validation
  -> Artifact records immutable Execution Bundle
  -> Worker safely extracts and preprocesses result
  -> Core result-finalization use case
  -> Artifact archives oversized result files
  -> Repo serializes result-branch write and push
  -> Repo fetches/verifies remote Commit
  -> Experiment binds Commit and becomes succeeded
```

Worker never opens the business database and never executes Git. It returns a
typed result to Core. Repo owns Git locking, fetch, commit, push, verification,
and compensating revert.

Small files are committed under the frozen result directory. Large files are
stored as Artifact Versions and represented by `.mmdash/artifacts.json`:

```json
{
  "schema_version": 1,
  "files": [
    {
      "path": "data/large-result.bin",
      "artifact_id": "uuid",
      "artifact_version_id": "uuid",
      "sha256": "lowercase hex",
      "size": 123456789,
      "media_type": "application/octet-stream"
    }
  ]
}
```

If forced Box revocation races after push but before binding, Experiment stays
failed and Repo pushes a compensating revert Commit. Never force-push or rewrite
the result branch.

## Self-run contract

`experiment.create` with `experiment_type=self` returns a machine-readable
`result_contract` containing:

- result branch and frozen directory;
- Manifest Schema URI/version and required files;
- Git/provider and Project size thresholds;
- `artifact.upload` instructions and the Artifact pointer schema;
- the requirement to commit and push before binding;
- the exact `experiment.result.bind` Tool and arguments.

The Coding Agent runs locally, uploads large files, writes the pointer manifest,
commits, pushes, and calls `experiment.result.bind(commit_sha)`. Experiment
asks Repo to fetch the remote result branch and verify the Commit, path, Exp ID,
source commit, Manifest, hashes, and same-Project Artifact Versions. The Web UI
does not show managed live logs for `self` Experiments.

## Target HTTP and MCP delta

Keep current list/get/create/run/cancel/archive/log/result/compare operations,
but update schemas for the new model and add:

- `POST /v1/projects/{projectId}/experiments/{experimentId}/rerun`, a
  human-Session-only command accepting `{overrides, idempotency_key}` and
  returning a frozen `box-re` record with a new Exp ID; Web prepares and edits
  the copied values client-side before the user confirms this request;
- `POST /v1/projects/{projectId}/experiments/{experimentId}/result/bind`, a
  self result-binding command accepting `{commit_sha, idempotency_key}` and
  returning the `verifying_result` Experiment;
- BFF
  `GET /api/projects/{projectId}/experiments/{experimentId}/logs/stream`, an
  authenticated SSE tail over already persisted logs, with `after_sequence`
  resume and periodic keepalive;
- result tree/virtual Artifact file aggregation;
- retry-chain and latest-ID fields on every Experiment read boundary.

`CreateExperimentRequest` is frozen to:

```text
name
experiment_type: box | self
source_commit: full immutable SHA
entrypoint: fixed typed argv contract
parameters / environment / inputs
runtime_policy: auto | e2b | local-docker | local-process  # box only
requested_box_id?: uuid                          # box only
limits_override?: cpu/memory/timeout/disk/pids/network
idempotency_key
```

`source_transfer` is derived by Core/Repo for each claimed Box task rather
than supplied by an Experiment caller. Environment manifest selection is
likewise derived by Box from the frozen source snapshot and its versioned
allowlist; callers cannot point preparation at an arbitrary file or command.

Core derives creator, Project, creation time, Project IANA timezone and frozen
`experiments/{exp_id}_{yyyymmdd_hhmm}/`; callers cannot supply the Exp ID or
result path. Create returns the complete Experiment. For `self`, it also
returns `result_contract`. `experiment.run` is valid only for `box`; a `self`
Experiment starts in `awaiting_result` and has no Box task. Rerun is never
accepted through create by passing `box-re`; only the human rerun command can
produce that type.

All Experiment reads return both dimensions, frozen request, actual
Box/Runtime, failure record, retry links, log completeness, Bundle pointer,
result directory/Commit/Manifest, progress, and permission-filtered actions.
When a newer retry exists they also return
`warning_code=EXPERIMENT_HAS_NEWER_RETRY` and `latest_experiment_id` without
redirecting or replacing the requested record.

Target MCP Tools:

```text
experiment.create
experiment.run
experiment.status
experiment.result.bind
result.get
artifact.upload
artifact.read
```

MCP request/response behavior is frozen as follows:

- `experiment.create` accepts the same request fields plus `project_id`. Its
  response always includes the Experiment ID/type/status/frozen path; `self`
  additionally returns the full machine-readable `result_contract` and a
  human-readable instruction summary.
- `experiment.run` accepts `{project_id, experiment_id, idempotency_key}` and
  returns the queued managed Experiment. It rejects `self` and already-started
  IDs with stable errors.
- `experiment.status` accepts `{project_id, experiment_id, log_tail?}` and
  returns the authoritative Experiment, bounded recent persisted logs,
  `logs_truncated`, progress, failure, and retry guidance. `self` never claims
  that managed logs exist.
- `experiment.result.bind` accepts
  `{project_id, experiment_id, commit_sha, idempotency_key}`. It is valid only
  for `self` in `awaiting_result`, requires a full SHA already reachable on the
  result branch, returns `verifying_result`, and is idempotent for the same
  SHA. A different SHA after acceptance is a conflict.
- `result.get` accepts `{project_id, experiment_id}` and returns status,
  result commit/directory/Manifest, read-only tree with Artifact virtual-file
  pointers, raw Bundle metadata when authorized, and retry guidance. Before
  binding it returns metadata with no successful result pointer.

The MCP Gateway forwards Project-scoped Agent identity and idempotency to Core;
it does not verify Git, mutate Experiment state, or synthesize directory
instructions. Tool descriptions must explicitly state that the Coding Agent
must push before binding and must fetch/pull managed results before reading
them from Git.

MCP descriptions, input/output JSON Schemas, examples, mocks, generated clients,
`docs/api/endpoints.md`, and `docs/api/mcp-tools.md` must change together.

## Migration plan from current main

Canonical migrations currently end at `000045_project_stage8_purge`; never
edit `000041_stage8_box_experiment`.

- `000043_box_account_nodes` is owned by Box Control and establishes account
  ownership/many-to-many assignment plus offline protocol storage.
- `000044_experiment_execution_results` is owned by Experiment and adds types,
  dual state, failure fields, retry links, result paths/Commit/Bundle pointers,
  log epoch/sequence, and safe backfill of existing records.
- `000045_project_stage8_purge` adds durable idempotent Project purge progress
  needed to delete Stage 8 database data, Artifact objects, Project-scoped
  grants/credentials, and internal Repo worktrees after the existing recycle
  window. It preserves account-level Boxes and Box Tokens and never deletes
  external repositories or remote branches.

Cover fresh, existing `000041`, revoked Boxes, active legacy Box
reauthorization, partial result processing, and down/up tests.

## Product completion and acceptance

The Web slice is incomplete until it provides cards with stage progress,
actual Box/Runtime, failure summary, retry relationships, a read-only terminal,
historical logs, a read-only result tree/previews, virtual Artifact files, and
compare mode.

Required focused verification after implementation:

```bash
pnpm contracts:generate
pnpm contracts:check
pnpm api:check
go test ./backend/internal/experiment ./backend/internal/boxcontrol ./backend/internal/artifact ./backend/internal/repo ./backend/internal/datahub
pnpm --filter @mmdash/web-bff test
pnpm --filter @mmdash/mcp-gateway test
go test ./box/...
```

Then run the complete `pnpm check` and Docker acceptance paths from `AGENTS.md`.
