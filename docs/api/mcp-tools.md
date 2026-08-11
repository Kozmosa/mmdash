# MCP tool catalog

Search this file by tool name, project scope, token kind, or contract. The
machine-readable schemas live under `contracts/json-schema/mcp-tools`.

| Tool                   | Purpose                                              | Project scoped | Token kinds | Input/output contract       | Status                  |
| ---------------------- | ---------------------------------------------------- | -------------- | ----------- | --------------------------- | ----------------------- |
| `project.list`         | List projects visible to the identity                | No             | CLI, Agent  | `project.list.json`         | Stage 3 read            |
| `project.get`          | Read one authorized Project                          | Yes            | CLI, Agent  | `project.get.json`          | Stage 3 read            |
| `data.list`            | List Data Hub object projections                     | Yes            | CLI, Agent  | `data.list.json`            | Stage 1 read            |
| `data.read`            | Read through the authoritative adapter               | Yes            | CLI, Agent  | `data.read.json`            | Stage 1 read            |
| `context.promote`      | Submit a pending Context Proposal                    | Yes            | CLI, Agent  | `context.promote.json`      | Stage 5 proposal        |
| `progress.get`         | Read Progress state and evaluation provenance        | Yes            | CLI, Agent  | `progress.get.json`         | Stage 6 read            |
| `progress.recalculate` | Schedule a versioned Progress evaluation             | Yes            | CLI, Agent  | `progress.recalculate.json` | Stage 6 mutation        |
| `artifact.read`        | Obtain a short-lived grant for one attached Artifact | Yes            | Agent       | `artifact.read.json`        | Agent attachment read   |
| `artifact.upload`      | Upload an image/file as an Agent Artifact            | Yes            | Agent       | `artifact.upload.json`      | Agent Artifact mutation |
| `system.echo`          | Verify the complete MCP Gateway boundary             | Yes            | CLI, Agent  | `system.echo.json`          | Foundation test tool    |

## Client and principal paths

The token-kind column describes the identity accepted at the Gateway, not a
single shared client path:

```text
Local Coding Agent -> Go mmdash CLI stdio bridge -> remote MCP Gateway
Hermes             -> remote MCP Gateway
```

The first path delegates the locally signed-in user's CLI identity. The second
path uses a revocable Agent Token bound to the
Hermes Agent instance, Project, and allowed tools. Hermes never reaches MCP
through the CLI, and its Agent Token is not a user CLI credential. The Agent
stage supports `manual` onboarding, where the user installs and rotates the
credential in Hermes, and `auto` onboarding, where the Hermes Adapter uses a
server-reachable authenticated management endpoint. Both modes produce the
same runtime path shown above.

For the CLI path the Gateway validates the user JWT or API token through Core
and forwards that same token on business reads. Static `MCP_CLI_TOKEN` remains a
development fixture only. `project.list` is the one account-scoped discovery
tool; every other business tool requires an explicit `project_id` and current
Core RBAC.

For the product Agent path, Gateway forwards the original Agent Token as the
only Core credential. Gateway enforces the exact reviewed Tool grant and Core
authenticates that same Agent identity before applying Project/domain RBAC.
Core is private; the Caddy-exposed `/v1` user API terminates at Web BFF and
rejects Agent credentials. Tool authorization is never inferred from an HTTP
header.

## `project.list` and `project.get`

`project.list` calls Core `projects.list` and filters any additional static
foundation scope before returning active Project summaries. `project.get`
requires `project_id`, applies Gateway scope, and calls Core `projects.get` for
the authoritative problem metadata and source references. Both calls are
read-only, audited, and never query PostgreSQL from the Gateway.

## `data.list` and `data.read`

These read-only tools call the generated Core Client and never access Git or
PostgreSQL directly. `data.list` accepts a `project_id` plus optional `type`,
`cursor`, and `limit`. Stage 1 Repo types are `repository`, `repo_commit`, and
`repo_file`. `data.read` accepts a `project_id` and Data Hub `object_id`, then
Core resolves full content through the owning module's authorized reader.

Stage 4 adds Progress projections for `milestone`, `task`, and
`progress_proposal`. Their full content is resolved by Core's Progress reader;
Dependency and Reminder events stay in the domain event stream and are not
advertised as standalone Data Hub object types yet.

Stage 6 adds `progress_evaluation` and `progress_risk` projections. Their
authoritative readers return the versioned input/output, trigger provenance,
failure state, Agent Session/Run references, and bounded risk detail without
exposing credentials or raw provider errors.

Stage 7 adds `model_source`, `model_question`, and `model_snapshot`.
`model_source` resolves the single authorized source and synchronization
countdown; `model_question` resolves the question detail and latest Snapshot;
`model_snapshot` resolves one immutable version by its question and Snapshot
identifiers. MCP remains a read-only `data.list/read` surface for Model and
never receives the Notion integration token or a temporary media URL.

For `repo_file`, the returned content remains pinned to the full commit SHA
stored in the projection. Binary, oversized, LFS, symlink, and submodule
objects return safe metadata rather than editable text.

## `context.promote`

`context.promote` submits a title, explicit content, context type, optional
rationale, and optional source Data Hub object IDs to Core's existing Context
Proposal boundary. The proposal remains `pending` until a human with
`project.context.review` accepts or rejects it. An Agent Token must grant the
exact `context.promote` Tool name, and the Gateway passes the authenticated
Agent instance to Core. Optional `agent_session_id` and `agent_run_id`
provenance must appear together. Core accepts them only when both belong to
the authenticated Agent and Project and the Run belongs to the Session;
mmdash-started Runs receive these local IDs through safe instructions.
External Runs may omit both. The Tool cannot review proposals and never copies
full Agent conversation history into the Project Data Hub.

## `progress.get` and `progress.recalculate`

`progress.get` reads the same authorized aggregate used by the Web Progress
page: detected and effective stage, active human override, latest evaluation,
history-linked Tasks/Proposals, and tracking Settings. It is read-only.

`progress.recalculate` schedules work in the Core-owned PostgreSQL Job Queue;
the Gateway never evaluates or writes Progress directly. Product Agent Tokens
may request only `trigger_kind=cron` with `force=false`. A human CLI identity
may request `manual` and may set `force=true` to bypass the configured minimum
interval. The request is still project-scoped, debounced, input-versioned,
audited, and subject to Core RBAC. Neither Tool can accept/reject a Proposal or
remove a human stage/Task override.

## `artifact.upload`

`artifact.upload` is one reviewed product-Agent mutation; it is not a generic
filesystem or base64 Tool. `begin` accepts filename, exact byte size,
lowercase SHA-256, optional MIME/metadata, and a stable idempotency key. Core
creates only `kind=agent`, `source=agent` and returns a bounded batch of
short-lived direct multipart PUT grants. `parts` refreshes later batches,
`complete` submits every part number plus provider ETag, and `abort` cancels an
unfinished upload.

Hermes performs the PUT requests directly against reachable MinIO/S3-compatible
storage using the exact returned headers. Gateway and Core never buffer a
complete part or file and MCP requests never contain file bytes. Upload state
is bound to the exact Agent instance; a second Agent in the same Project cannot
continue it. Local/Core-proxy transfer mode fails with
`ARTIFACT_DIRECT_TRANSFER_REQUIRED` so remote clients are not handed a URL that
only the deployment host can reach. Completed content appears through the
normal Artifact library and Data Hub readers.

For an mmdash-started Run, `begin` should include both local
`agent_session_id` and `agent_run_id`. Core validates the pair and associates
the completed output with that Run, allowing Web to show an image preview or
file card in the transcript. The Run instructions require Hermes to use this
capability proactively whenever a useful file or image is a deliverable.

## `artifact.read`

Read-only Agent Tool for a user-uploaded chat attachment. It accepts one
Project-scoped Artifact ID plus an optional immutable Version ID and returns
Core's short-lived authorized GET grant. Hermes downloads with the exact
method and headers, and must not reveal the signed URL to the user. The Tool
does not widen Project access: Gateway enforces the exact Agent grant and Core
re-applies Artifact read authorization.

## `system.echo`

Read-only and idempotent test tool. It verifies parameter validation,
project-level and tool-level authorization, session correlation, safe error
conversion, and audit recording without touching business storage.

Audit is always emitted as structured JSON. When `MCP_CORE_AUDIT_TOKEN` is
configured, the same `request_id`, project, outcome, tool name, delegated
principal, and logical session are persisted through `audit.events.record`.

Input:

```json
{
  "message": "hello",
  "project_id": "project-1"
}
```

Successful structured output:

```json
{
  "message": "hello",
  "principal_kind": "cli",
  "project_id": "project-1",
  "request_id": "28fd1889-86dc-4ff1-bc27-97791c2a9d70",
  "session_id": "44707790-074b-4d6c-8d2b-0521ed8c450c"
}
```

The tool returns `isError: true` with a stable `code`, safe `message`, and
`request_id` when an authorized MCP request reaches the tool but cannot be
completed. Protocol and authentication failures remain HTTP/JSON-RPC errors.
