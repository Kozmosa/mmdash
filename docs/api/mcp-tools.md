# MCP tool catalog

Search this file by tool name, project scope, token kind, or contract. The
machine-readable schemas live under `contracts/json-schema/mcp-tools`.

| Tool          | Purpose                                  | Project scoped | Token kinds | Input/output contract | Status              |
| ------------- | ---------------------------------------- | -------------- | ----------- | --------------------- | ------------------- |
| `data.list`   | List Data Hub object projections         | Yes            | CLI, Agent  | `data.list.json`      | Stage 1 read        |
| `data.read`   | Read through the authoritative adapter   | Yes            | CLI, Agent  | `data.read.json`      | Stage 1 read        |
| `system.echo` | Verify the complete MCP Gateway boundary | Yes            | CLI, Agent  | `system.echo.json`    | Stage 3.5 test tool |

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
