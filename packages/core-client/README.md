# Core Client

Contract-backed TypeScript client for `contracts/openapi/core.yaml`, used by
Web BFF, MCP Gateway, and internal TypeScript tooling.

```bash
pnpm --filter @mmdash/core-client generate
pnpm --filter @mmdash/core-client test
pnpm --filter @mmdash/core-client build
```

`src/generated/core.ts` is generated and committed so consumers do not need a
generator at runtime. `CoreClient` adds request context propagation and maps
non-success responses into `CoreClientError`.
