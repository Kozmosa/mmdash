# API documentation

This directory is the human-readable index for mmdash APIs. Canonical
machine-readable contracts live in
[`contracts/openapi`](../../contracts/openapi), currently:

- [`core.yaml`](../../contracts/openapi/core.yaml) for Core consumers;
- [`web-bff.yaml`](../../contracts/openapi/web-bff.yaml) for browser-facing
  HTTP, SSE, WebSocket, and file-stream routes.
- [`contracts/json-schema/mcp-tools`](../../contracts/json-schema/mcp-tools)
  for MCP tool input and structured-output schemas.

## Find an API quickly

1. Search [`endpoints.md`](endpoints.md) by service, method, path, module, or
   `operationId`.
   For tool details, search [`mcp-tools.md`](mcp-tools.md) by tool name,
   project scope, or token kind.
2. Open the matching `operationId` in the named OpenAPI contract for request
   and response schemas.
3. Use `pnpm api:check` after editing the contract. CI rejects operations that
   are absent from the endpoint catalog.

## Public ingress

The repository-root `Caddyfile` is the authoritative public route map for
`https://mmdash.com`:

| Public path         | Upstream            | Traffic                                |
| ------------------- | ------------------- | -------------------------------------- |
| `/`                 | Web `:3000`         | Browser pages and static assets        |
| `/api/*`            | Web BFF `:3001`     | Browser API, files, SSE, and WebSocket |
| `/mcp` and `/mcp/*` | MCP Gateway `:3002` | Streamable HTTP MCP                    |
| `/box` and `/box/*` | Core `:8080`        | Box control-plane traffic              |

Caddy preserves these path prefixes. Reverse proxies support WebSocket upgrade
and use low-latency response flushing for streams.

## Contract rules

- Every operation has a stable, searchable `operationId`.
- `/v1/*` is the Core API. Browsers reach browser-safe projections under
  `/api/*` through Web BFF.
- MCP methods are cataloged here and implemented by MCP Gateway; they never
  access storage directly.
- Error responses use a stable code, a safe message, and a `request_id` when a
  request context exists.
- Timestamps are UTC RFC 3339 strings. IDs are opaque strings.
- New list endpoints use cursor pagination unless their result is explicitly
  bounded.

The browser client lives at `apps/web/src/lib/api-client.ts`. It uses same-origin
`/api`, sends browser credentials, serializes JSON bodies, and converts BFF
errors into a stable `ApiError` containing `status`, `code`, and `requestId`.

The generated Core types and runtime wrapper live in `packages/core-client`.
Regenerate its types after changing `core.yaml` with:

```bash
pnpm --filter @mmdash/core-client generate
```

## Related documents

- [Endpoint catalog](endpoints.md)
- [MCP tool catalog](mcp-tools.md)
- [Local development](../development/README.md)
- [Web BFF development](../development/web-bff.md)
- [Architecture](../architecture/README.md)
