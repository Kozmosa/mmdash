# API endpoint catalog

Search this file by `operationId`, method/name, path, service, or module. The
catalog is the human-readable lookup index; the linked OpenAPI contract is the
machine-readable source of truth.

| Service     | Kind          | Method / name | Path                                                | `operationId`                      | Module               | Contract           |
| ----------- | ------------- | ------------- | --------------------------------------------------- | ---------------------------------- | -------------------- | ------------------ |
| Core        | HTTP          | `GET`         | `/health/live`                                      | `health.live`                      | platform health      | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/health/ready`                                     | `health.ready`                     | platform health      | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/openapi.yaml`                                     | `system.openapi.get`               | platform contract    | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/example`                                       | `example.check`                    | engineering baseline | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/auth/login`                                    | `auth.login`                       | auth                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/auth/logout`                                   | `auth.logout`                      | auth                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/auth/me`                                       | `auth.me`                          | auth                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/auth/tokens`                                   | `auth.tokens.list`                 | auth                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/auth/tokens`                                   | `auth.tokens.create`               | auth                 | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/auth/tokens/{tokenId}`                         | `auth.tokens.revoke`               | auth                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects`                                      | `projects.list`                    | project              | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects`                                      | `projects.create`                  | project              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}`                          | `projects.get`                     | project              | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}`                          | `projects.update`                  | project              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/members`                  | `projects.members.list`            | project              | `core.yaml`        |
| Core        | HTTP          | `PUT`         | `/v1/projects/{projectId}/members/{userId}`         | `projects.members.upsert`          | project              | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/members/{userId}`         | `projects.members.remove`          | project              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/permissions`              | `projects.permissions.get`         | project              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/settings/types`                                | `settings.types.list`              | settings             | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/settings/system/{typeKey}`                     | `settings.system.get`              | settings             | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/settings/system/{typeKey}`                     | `settings.system.update`           | settings             | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/settings/system/{typeKey}`                     | `settings.system.delete`           | settings             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/settings/system/{typeKey}/test`                | `settings.system.test`             | settings             | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/settings/projects/{projectId}/{typeKey}`       | `settings.projects.get`            | settings             | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/settings/projects/{projectId}/{typeKey}`       | `settings.projects.update`         | settings             | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/settings/projects/{projectId}/{typeKey}`       | `settings.projects.delete`         | settings             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/settings/projects/{projectId}/{typeKey}/test`  | `settings.projects.test`           | settings             | `core.yaml`        |
| Web BFF     | HTTP          | `GET`         | `/health/live`                                      | `bff.health.live`                  | health               | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/health/ready`                                     | `bff.health.ready`                 | health               | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/example`                                      | `bff.example.check`                | example proxy        | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/auth/login`                                   | `bff.auth.login`                   | auth                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/auth/me`                                      | `bff.auth.me`                      | auth                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/auth/logout`                                  | `bff.auth.logout`                  | auth                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects`                                     | `bff.projects.list`                | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects`                                     | `bff.projects.create`              | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}`                         | `bff.projects.get`                 | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}`                         | `bff.projects.update`              | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/members`                 | `bff.projects.members.list`        | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PUT`         | `/api/projects/{projectId}/members/{userId}`        | `bff.projects.members.upsert`      | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/members/{userId}`        | `bff.projects.members.remove`      | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/permissions`             | `bff.projects.permissions.get`     | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/settings/types`                               | `bff.settings.system.types.list`   | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/settings/system/{typeKey}`                    | `bff.settings.system.get`          | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/settings/system/{typeKey}`                    | `bff.settings.system.update`       | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/settings/system/{typeKey}`                    | `bff.settings.system.delete`       | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/settings/system/{typeKey}/test`               | `bff.settings.system.test`         | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/settings/types`          | `bff.settings.projects.types.list` | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/settings/{typeKey}`      | `bff.settings.projects.get`        | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/settings/{typeKey}`      | `bff.settings.projects.update`     | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/settings/{typeKey}`      | `bff.settings.projects.delete`     | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/settings/{typeKey}/test` | `bff.settings.projects.test`       | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/pages/{pageId}`          | `bff.page.get`                     | page aggregation     | `web-bff.yaml`     |
| Web BFF     | SSE           | `GET`         | `/api/projects/{projectId}/events`                  | `bff.events.stream`                | stream proxy         | `web-bff.yaml`     |
| Web BFF     | WebSocket     | `CONNECT`     | `/api/projects/{projectId}/socket`                  | `bff.socket.connect`               | stream proxy         | `web-bff.yaml`     |
| Web BFF     | File stream   | `GET`         | `/api/projects/{projectId}/files/{filePath}`        | `bff.file.download`                | file proxy           | `web-bff.yaml`     |
| Web BFF     | File metadata | `HEAD`        | `/api/projects/{projectId}/files/{filePath}`        | `bff.file.head`                    | file proxy           | `web-bff.yaml`     |
| Web BFF     | File stream   | `PUT`         | `/api/projects/{projectId}/files/{filePath}`        | `bff.file.upload`                  | file proxy           | `web-bff.yaml`     |
| MCP Gateway | MCP Tool      | `tools/call`  | `/mcp`                                              | `system.echo`                      | engineering baseline | `system.echo.json` |

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

## Core platform conventions

- Core is the authoritative state boundary. Other processes call its
  contract and do not connect to PostgreSQL or object storage directly.
- `GET /health/live` only checks the process. `GET /health/ready` checks both
  `postgres` and `object_storage`, returning HTTP 503 and `not_ready` when
  either is unavailable.
- `GET /openapi.yaml` serves the canonical contract configured at process
  startup, so operators and generated clients can be compared against the
  running service.
- All Core JSON errors carry stable `code` and `message` fields; request-bound
  errors also carry `request_id`. Internal causes are never serialized.

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
