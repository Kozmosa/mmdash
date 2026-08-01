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

`Authorization: Bearer <token>` accepts a Core user session or API token for
the Stage 3 CLI path. The Gateway validates it through `auth.me`, maps it to a
CLI principal, and forwards the same delegated token to Core. Current Project
membership and RBAC therefore remain authoritative at the owning Core module.

Two independently configured static foundation tokens remain available for
local development and boundary tests:

| Variable          | Principal     | Default permissions variable            |
| ----------------- | ------------- | --------------------------------------- |
| `MCP_CLI_TOKEN`   | Local CLI     | `MCP_CLI_PROJECTS`, `MCP_CLI_TOOLS`     |
| `MCP_AGENT_TOKEN` | Agent runtime | `MCP_AGENT_PROJECTS`, `MCP_AGENT_TOOLS` |

Permission lists are comma-separated exact names or prefix patterns ending in
`*`; a single `*` allows all. Production startup rejects development token
values and does not require either static token.
Tokens are compared in constant time, represented in logs only by a short
SHA-256-derived principal ID, and never returned by tools.

These environment-configured values exist for local development, integration
tests, and validation of the Gateway authorization boundary. In particular,
`MCP_AGENT_TOKEN` is not the first product Agent Token scheme and must not be
used as the production Hermes credential lifecycle.

The later Agent stage integrates the Gateway with the Core/Auth-managed opaque
Agent Token model. That token is issued once for an Agent instance, scoped to a
Project and allowed MCP tools, stored by Hermes, and presented by Hermes
directly to this Gateway. The user CLI has a separate user-delegated identity
and does not proxy, mint, persist, or validate Hermes credentials.

The gateway also validates Host and Origin using `MCP_ALLOWED_HOSTS` and
`MCP_ALLOWED_ORIGINS` to prevent DNS rebinding.

Set `MCP_CORE_ACCESS_TOKEN` to a Core-issued, project-authorized API/Agent token
for business reads such as `data.list` and `data.read`. Set
`MCP_CORE_AUDIT_TOKEN` to a Core-issued API token to persist tool audit events
in Core's queryable Audit ledger. If the access token is omitted, the audit
token is used for Core reads as a compatibility fallback. Without an audit
token, JSON audit logging remains enabled. Neither token is written to logs or
tool output.

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
