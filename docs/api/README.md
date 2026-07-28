# API documentation

This directory is the human-readable index for mmdash APIs. The canonical
machine-readable Core contract is
[`contracts/openapi/core.yaml`](../../contracts/openapi/core.yaml).

## Find an API quickly

1. Search [`endpoints.md`](endpoints.md) by service, method, path, module, or
   `operationId`.
2. Open the matching operation in the OpenAPI contract for request and response
   schemas.
3. Use `pnpm api:check` after editing the contract. CI rejects operations that
   are absent from the endpoint catalog.

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

## Related documents

- [Endpoint catalog](endpoints.md)
- [Local development](../development/README.md)
- [Architecture](../architecture/README.md)
