# API endpoint catalog

Search this file by `operationId`, HTTP method, path, service, or module. The
catalog tracks callable interfaces as they are introduced.

| Service | Kind | Method / name | Path           | `operationId`       | Module        | Status   |
| ------- | ---- | ------------- | -------------- | ------------------- | ------------- | -------- |
| Core    | HTTP | `GET`         | `/v1/example`  | `example.check`     | example       | Baseline |
| Web BFF | HTTP | `GET`         | `/api/example` | `bff.example.check` | example proxy | Baseline |

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
