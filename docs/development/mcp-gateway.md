# MCP Gateway development

`apps/mcp-gateway` is the only remote MCP boundary. It exposes project-scoped
tools and calls Core through `packages/core-client`; it must not connect to
PostgreSQL, MinIO, Git, Hermes, or Box directly.

## Protocol compatibility

The gateway uses the official MCP TypeScript SDK 2.x:

- 2026-07-28 Streamable HTTP is the preferred wire protocol.
- The SDK's stateless fallback serves 2025-era clients.
- The current protocol removed `Mcp-Session-Id`. mmdash therefore owns the
  `X-Mmdash-Session-Id` header for audit/session correlation, with a sliding TTL
  and principal binding. The pinned Hermes client still uses the 2025
  Streamable HTTP `Mcp-Session-Id` header, so Gateway accepts and emits both
  names with the same value. Conflicting dual headers are rejected.

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

Stage 5 integrates the Gateway with the Core/Auth-managed opaque Agent Token
model. That token is issued once for an Agent instance, scoped to one Project
and an exact MCP Tool allowlist, stored by Hermes, and presented by Hermes
directly to this Gateway. The user CLI has a separate user-delegated identity
and does not proxy, mint, persist, or validate Hermes credentials.

Stage 6 expands the reviewed exact Tool set to `progress.get` and
`progress.recalculate`. The former reads the Core Progress aggregate. The
latter schedules a Core-owned PostgreSQL evaluation request; Agent credentials
are limited by Core to `trigger_kind=cron` and `force=false`. The Gateway never
calls Hermes or PostgreSQL and never reviews Proposals or changes human
overrides. Both Tools enforce Gateway Project/Tool scope, Core RBAC, safe error
mapping, and the normal MCP/Core Audit path.

The Agent Artifact iteration adds exact Tools `artifact.read` and
`artifact.upload`. `artifact.read` delegates one authorized Artifact/Version
to Core's short-lived download grant and never emits a permanent URL.
Upload's four actions initialize, sign bounded part batches, confirm, or abort through the
generated Core Client with the original inbound Agent Token. The Gateway never
accepts file bytes/base64 and never proxies a multipart PUT. It returns only
direct MinIO/S3-compatible grants; Local/Core-proxy mode fails closed with
`ARTIFACT_DIRECT_TRANSFER_REQUIRED`. Core fixes the classification to
`kind/source=agent` and binds upload state to the exact Agent instance.
For an mmdash-started Run, upload provenance is a required pair when present;
Core validates it before relating the available output to the Run.

A pending product Agent Token can authenticate only for its verification
handshake. `auth.me`, current-protocol `server/discover`, and legacy
`initialize` do not mark it verified. Once the client has negotiated an mmdash
MCP Session, the first successful `tools/list` causes Gateway to record evidence
through Core `auth.agent_tokens.verification.record`. The one-time endpoint
carries the Token-specific challenge, and Gateway authenticates the callback
with the same pending Agent Token. Core matches the Token, Agent, Project, and
challenge hash, then consumes the challenge atomically.

If evidence cannot be recorded, Gateway fails the pending `tools/list` with
`AGENT_VERIFICATION_UNAVAILABLE` and does not treat the Token as verified.
After activation, normal exact-allowlist authorization applies. Gateway sends
the original product Agent Token as Core's only credential. Core authenticates
the same Agent identity and re-applies Project/domain RBAC. Request logs,
errors, and MCP results must not contain the credential or challenge.

The gateway also validates Host and Origin using `MCP_ALLOWED_HOSTS` and
`MCP_ALLOWED_ORIGINS` to prevent DNS rebinding.

CLI reads and product Agent calls both delegate the original inbound identity;
they never substitute a Gateway service credential. Set
`MCP_CORE_AUDIT_TOKEN` to a Core-issued API token to persist tool audit events
in Core's queryable Audit ledger. Without an audit token, JSON audit logging
remains enabled. The audit token is never used for Tool authorization and is
not written to logs or Tool output.

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

`GET /health/live` and `GET /mcp/health/live` are public. Every other `/mcp`
and `/mcp/*` request requires a token.
