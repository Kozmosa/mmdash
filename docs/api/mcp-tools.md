# MCP tool catalog

Search this file by tool name, project scope, token kind, or contract. The
machine-readable schemas live under `contracts/json-schema/mcp-tools`.

| Tool | Purpose | Project scoped | Token kinds | Input/output contract | Status |
| --- | --- | --- | --- | --- | --- |
| `system.echo` | Verify the complete MCP Gateway boundary | Yes | CLI, Agent | `system.echo.json` | Stage 3.5 test tool |

## `system.echo`

Read-only and idempotent test tool. It verifies parameter validation,
project-level and tool-level authorization, session correlation, safe error
conversion, and audit recording without touching business storage.

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
