# API endpoint catalog

Search this file by `operationId`, method/name, path, service, or module. The
catalog is the human-readable lookup index; the linked OpenAPI contract is the
machine-readable source of truth.

| Service | Kind | Method / name | Path | `operationId` | Module | Contract |
| --- | --- | --- | --- | --- | --- | --- |
| Core | HTTP | `GET` | `/v1/example` | `example.check` | engineering baseline | `core.yaml` |
| Web BFF | HTTP | `GET` | `/health/live` | `bff.health.live` | health | `web-bff.yaml` |
| Web BFF | HTTP | `GET` | `/health/ready` | `bff.health.ready` | health | `web-bff.yaml` |
| Web BFF | HTTP | `GET` | `/api/example` | `bff.example.check` | example proxy | `web-bff.yaml` |
| Web BFF | HTTP | `GET` | `/api/projects/{projectId}/pages/{pageId}` | `bff.page.get` | page aggregation | `web-bff.yaml` |
| Web BFF | SSE | `GET` | `/api/projects/{projectId}/events` | `bff.events.stream` | stream proxy | `web-bff.yaml` |
| Web BFF | WebSocket | `CONNECT` | `/api/projects/{projectId}/socket` | `bff.socket.connect` | stream proxy | `web-bff.yaml` |
| Web BFF | File stream | `GET` | `/api/projects/{projectId}/files/{filePath}` | `bff.file.download` | file proxy | `web-bff.yaml` |
| Web BFF | File metadata | `HEAD` | `/api/projects/{projectId}/files/{filePath}` | `bff.file.head` | file proxy | `web-bff.yaml` |
| Web BFF | File stream | `PUT` | `/api/projects/{projectId}/files/{filePath}` | `bff.file.upload` | file proxy | `web-bff.yaml` |
| MCP Gateway | MCP Tool | `tools/call` | `/mcp` | `system.echo` | engineering baseline | `system.echo.json` |

## Browser BFF conventions

- Except for health and engineering checks, BFF operations require the signed
  `mmdash_session` browser cookie.
- Project context comes from the path, `project_id` query parameter, or
  `x-mmdash-project-id`; multiple different values yield
  `PROJECT_CONTEXT_CONFLICT`.
- Every response carries `x-request-id`. Error JSON contains `code`, a safe
  `message`, and `request_id`.
- SSE and file bodies pass through without application buffering. The
  WebSocket route proxies frames bidirectionally.
- `/api/projects/{projectId}/pages/{pageId}` is an extension point. Stage 3.4
  registers `workspace-shell`, whose `context` fragment contains the current
  browser-safe user and project projection.

## `example.check`

Verifies the complete engineering-baseline path. It executes
`SELECT CURRENT_TIMESTAMP` through the Core PostgreSQL pool and returns:

```json
{
  "status": "ok",
  "storage": "postgres",
  "checked_at": "2026-07-28T12:00:00Z"
}
```

This is an engineering endpoint rather than a product-domain API. It may be
removed only after a replacement smoke path is documented.
