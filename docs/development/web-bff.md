# Web BFF development

`apps/web-bff` is the browser boundary. It validates browser identity and
project context, converts errors, aggregates page projections, and proxies
streaming traffic. It calls Core only through `packages/core-client`; it must
not import PostgreSQL, MinIO, Git, Hermes, or Box adapters.

## Run and verify

```bash
pnpm --filter @mmdash/web-bff dev
pnpm --filter @mmdash/web-bff test
pnpm --filter @mmdash/web-bff build
```

The default local address is `http://127.0.0.1:3001`. Configure it with:

| Variable | Purpose |
| --- | --- |
| `BFF_HOST` | Listen address |
| `BFF_PORT` | Listen port |
| `BFF_COOKIE_SECRET` | At least 32 characters; signs browser session assertions |
| `CORE_BASE_URL` | Core HTTP origin |

Production startup rejects the documented development cookie secret.

## Request lifecycle

1. Fastify accepts or creates `x-request-id` and returns it in the response.
2. Public routes opt out; all other routes require a signed
   `mmdash_session` cookie containing a short-lived session assertion.
3. Project-aware routes resolve the ID from the path, `project_id` query, or
   `x-mmdash-project-id`. Conflicting values are rejected.
4. The route calls the generated-contract-backed `CoreClient`, propagating
   request, user, and project headers.
5. Core, validation, and unexpected failures become the stable error envelope
   documented in `contracts/openapi/web-bff.yaml`.

## Extend the BFF

- Register normal HTTP routes from `src/app.ts`.
- Register WebSocket routes inside `websocketRoutesScope`; the WebSocket plugin
  must finish loading before Fastify constructs those routes.
- Add page-specific contributors to `PageAggregatorRegistry`. Contributors run
  concurrently and return browser-safe fragments only.
- Keep file, SSE, and WebSocket bodies streaming. Do not buffer large payloads.
- Add each callable operation to the Web BFF OpenAPI contract and
  `docs/api/endpoints.md`, then run `pnpm api:check`.

Tests use signed short-lived assertions and real local WebSocket servers. They
cover authentication, project conflicts, request propagation, aggregation,
safe error conversion, SSE/file streaming, and bidirectional WebSocket proxying.
