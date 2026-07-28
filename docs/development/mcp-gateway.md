# MCP Gateway development

`apps/mcp-gateway` is the only remote MCP boundary. It exposes project-scoped
tools and calls Core through `packages/core-client`; it must not connect to
PostgreSQL, MinIO, Git, Hermes, or Box directly.

## Protocol compatibility

The gateway uses the official MCP TypeScript SDK 2.x:

- 2026-07-28 Streamable HTTP is the preferred wire protocol.
- The SDK's stateless fallback serves 2025-era clients.
- The current protocol removed `Mcp-Session-Id`. mmdash therefore uses the
  application-owned `X-Mmdash-Session-Id` header for audit/session correlation,
  with a sliding TTL and principal binding. This avoids claiming a removed MCP
  protocol feature while preserving the design's logical session boundary.

Clients send the returned mmdash session header on later requests. An
authenticated `DELETE /mcp` with that header explicitly terminates it.

## Authentication and authorization

`Authorization: Bearer <token>` accepts two independently configured static
foundation tokens:

| Variable          | Principal     | Default permissions variable            |
| ----------------- | ------------- | --------------------------------------- |
| `MCP_CLI_TOKEN`   | Local CLI     | `MCP_CLI_PROJECTS`, `MCP_CLI_TOOLS`     |
| `MCP_AGENT_TOKEN` | Agent runtime | `MCP_AGENT_PROJECTS`, `MCP_AGENT_TOOLS` |

Permission lists are comma-separated exact names or prefix patterns ending in
`*`; a single `*` allows all. Production startup rejects development tokens.
Tokens are compared in constant time, represented in logs only by a short
SHA-256-derived principal ID, and never returned by tools.

The gateway also validates Host and Origin using `MCP_ALLOWED_HOSTS` and
`MCP_ALLOWED_ORIGINS` to prevent DNS rebinding.

Set `MCP_CORE_AUDIT_TOKEN` to a Core-issued API token to persist tool audit
events in Core's queryable Audit ledger. Without it, JSON audit logging remains
enabled. The token is used only for Core audit ingestion and is never written
to logs or tool output.

## Register a tool

1. Implement a `ToolModule` under `src/tools`.
2. Declare one Zod input schema; the SDK publishes and validates it.
3. Enforce project and tool permissions before calling Core.
4. Convert expected failures to a safe tool result and write an audit event.
5. Register the module in `createDefaultToolRegistry`.
6. Add its machine schema under `contracts/json-schema/mcp-tools`, document it
   in `docs/api/mcp-tools.md`, and run `pnpm api:check`.

## Run and verify

```bash
pnpm --filter @mmdash/mcp-gateway dev
pnpm --filter @mmdash/mcp-gateway test
pnpm --filter @mmdash/mcp-gateway build
```

`GET /health/live` is public. `/mcp` and `/mcp/*` require a token.
