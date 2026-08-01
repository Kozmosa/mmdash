# MCP tool catalog

Search this file by tool name, project scope, token kind, or contract. The
machine-readable schemas live under `contracts/json-schema/mcp-tools`.

| Tool           | Purpose                                  | Project scoped | Token kinds | Input/output contract | Status               |
| -------------- | ---------------------------------------- | -------------- | ----------- | --------------------- | -------------------- |
| `project.list` | List projects visible to the identity    | No             | CLI, Agent  | `project.list.json`   | Stage 3 read         |
| `project.get`  | Read one authorized Project              | Yes            | CLI, Agent  | `project.get.json`    | Stage 3 read         |
| `data.list`    | List Data Hub object projections         | Yes            | CLI, Agent  | `data.list.json`      | Stage 1 read         |
| `data.read`    | Read through the authoritative adapter   | Yes            | CLI, Agent  | `data.read.json`      | Stage 1 read         |
| `system.echo`  | Verify the complete MCP Gateway boundary | Yes            | CLI, Agent  | `system.echo.json`    | Foundation test tool |

## Client and principal paths

The token-kind column describes the identity accepted at the Gateway, not a
single shared client path:

```text
Local Coding Agent -> Go mmdash CLI stdio bridge -> remote MCP Gateway
Hermes             -> remote MCP Gateway
```

The first path delegates the locally signed-in user's CLI identity. The second
path, added in the later Agent stage, uses a revocable Agent Token bound to the
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

For `repo_file`, the returned content remains pinned to the full commit SHA
stored in the projection. Binary, oversized, LFS, symlink, and submodule
objects return safe metadata rather than editable text.

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
