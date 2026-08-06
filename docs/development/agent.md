# Stage 5 Agent development and acceptance

The Agent module is the Core authority for Runtime-independent Agent
instances, Project Grants, project Prompt overrides, Session indexes, remote
Session/Run mappings, normalized Tool Call state, Token-rotation orchestration,
Audit, and Outbox events. Hermes owns full message history and execution; Web,
Web BFF, MCP Gateway, and Data Hub do not read Agent tables directly.

Read the public boundary in [`docs/api/agent.md`](../api/agent.md) and the
canonical operations in `contracts/openapi/core.yaml` and
`contracts/openapi/web-bff.yaml` before changing this module.

## Persistence and ownership

After Stage 4 fixes are integrated, migration `000026_agent_sessions` owns:

- `agent_instances` and redacted runtime/management check snapshots;
- `agent_project_grants`, exact Tool scope, default Session, Prompt override,
  and opaque remote project-access reference;
- `agent_sessions`, including `main`, `progress`, and `experiment` indexes and
  local-to-Hermes Session IDs;
- `agent_runs` and browser-safe `agent_tool_calls` summaries;
- Auth-owned `auth_agent_tokens` hashes and Agent-owned
  `agent_token_rotations` workflow state;
- resource-scoped encrypted Settings values for per-instance Hermes secrets;
- Context Proposal actor kind plus optional Agent Session/Run provenance.

Migrations `000023_progress_reminder_processing`,
`000024_progress_project_references`, and `000025_project_invitation_expiry`
belong to the accepted Stage 4 fixes and must remain before this migration.
Both fresh and existing development databases must preserve their Progress,
Notification, and Project data.

Auth owns Token generation, SHA-256 hashing, authentication, activation, and
revocation. Settings owns encryption and authorized reads for the Hermes API
Key, Dashboard Session Token, and optional Cloudflare Access Service Token.
Agent coordinates those boundaries but never stores plaintext in its own
tables.

## Adapter and Hermes mock contracts

`backend/internal/agent/AgentAdapter` is provider-neutral. The only Stage 5
registration is `HermesAdapter`, pinned for contract tests to:

```text
NousResearch/hermes-agent v2026.8.3
Hermes Agent 0.20.0
3c27eb6234bf91b8ceee9e9071591b31e9b148cb
```

HTTP and SSE tests use a local mock server and exact pinned request/response
fixtures for health, authentication, capabilities, Sessions, messages, Runs,
approvals, streaming, stopping, Jobs, and Dashboard MCP management. Never infer
an upstream route or event from an older Hermes release. A contract change
requires updating the pinned fixture and documenting the upstream evidence.

Hermes Jobs are mapped and probed in this stage but are not scheduled by the
product. Do not add automatic Progress evaluation, Cron creation, event
consumers, debounce, or Stage 6 triggers here.

`StreamChat` is a tested interface port (session event streaming). The current
product message path executes through StartRun plus StreamRun so that Run
status, Tool Calls, and stop/regenerate/rerun stay consistent; StreamChat
remains available for a future session-event-stream path. A new Runtime
Adapter still must implement the whole interface, including StreamChat.

## Connector safety

Runtime and management connectors use deployment-owned network policy. Tests
must cover URL credentials/fragments, scheme and port limits, DNS failure,
mixed public/private DNS answers, rebinding-safe dialing, link-local and cloud
metadata ranges, loopback/private policy, redirect origin and target
revalidation, redirect count, connect/header/request timeouts, response-size
limits, and credential-free normalized errors.

Each Agent instance owns an isolated Adapter and HTTP authentication context.
Do not share credential-bearing transports across instances. Manual Dashboard
URLs are links only; Core must not call management APIs in manual mode. Auto
mode requires authenticated Dashboard access and rejects unsupported exposed
authentication rather than silently downgrading.

The deployment-owned Connector settings are:

| Boundary | Variables | Secure default |
| --- | --- | --- |
| MCP endpoint installed in Hermes | `AGENT_MCP_GATEWAY_URL` | `http://localhost:3002/mcp` outside Compose |
| Hermes runtime API | `AGENT_RUNTIME_ALLOW_LOOPBACK`, `AGENT_RUNTIME_ALLOW_PRIVATE`, `AGENT_RUNTIME_ALLOWED_PORTS`, `AGENT_RUNTIME_CONNECT_TIMEOUT`, `AGENT_RUNTIME_RESPONSE_HEADER_TIMEOUT`, `AGENT_RUNTIME_REQUEST_TIMEOUT`, `AGENT_RUNTIME_MAX_REDIRECTS`, `AGENT_RUNTIME_MAX_RESPONSE_BYTES` | loopback/private denied; ports `80,443,8642`; `5s/10s/30s`; 3 redirects; 4 MiB |
| Hermes Dashboard management API | matching `AGENT_MANAGEMENT_*` variables, plus `AGENT_MANAGEMENT_MINIMUM_INTERVAL` | loopback/private denied; ports `80,443,9119`; `5s/10s/30s`; 3 redirects; 4 MiB; 250 ms between management operations per Agent instance |

`AGENT_MCP_GATEWAY_URL` is the endpoint Hermes sees, not necessarily the
Core-to-Gateway container address. Set it to the externally routable HTTPS MCP
URL in production. Enabling loopback or private destinations is a deployment
decision and does not bypass scheme, port, DNS, redirect, timeout, response
size, or Hermes identity checks.

## Token and Tool-scope acceptance

Tests must prove:

- Token plaintext is high entropy, returned once, and absent from database,
  ordinary responses, events, Audit, errors, metrics, and logs;
- MCP Gateway and Core independently enforce status, Agent instance, Project,
  and exact Tool name;
- changing `allowed_tools` issues a new pending Token instead of widening an
  existing credential;
- manual and auto rotation keep the old Token valid until a real new-Token MCP
  verification succeeds;
- auto rotation prepares a parallel versioned Hermes MCP entry, atomically
  activates the new Token and persists its opaque remote reference, and only
  then finalizes deletion of the prior entry; failed final cleanup is a safe
  `project_access_cleanup_pending` condition because the prior Token is already
  revoked and the new path remains active;
- any auto management, reload/restart, or verification failure records a safe
  failure; a replacement failure leaves the old Token active and reports
  `old_token_remains_active=true`, while initial provisioning has no old Token
  and reports `false`;
- abort revokes only the pending Token, while explicit revoke is immediate.

Auto mode should use parallel versioned Hermes MCP entries during rotation so
failure cannot destroy the last working configuration.

For pinned Hermes v2026.8.3, Dashboard MCP `/test` connects with the current MCP
SDK, negotiates through `server/discover`, and then performs `tools/list`; it
does not execute a business Tool. Stage 5 therefore defines the required real
MCP verification as a successful `tools/list` made with the pending credential
in the same fully negotiated, credential-owned MCP session. Gateway also
accepts legacy MCP `initialize` as negotiation for compatible clients, but
neither negotiation request is evidence by itself. The pending credential may
discover only its reviewed exact Tool grant and cannot execute any
`tools/call`. After the list succeeds, MCP Gateway records durable evidence
through a dedicated trusted Core callback. Authentication and `/auth/me` also
never create evidence.

The callback uses the Core API credential whose plaintext is configured as
`MCP_CORE_ACCESS_TOKEN`; Core accepts it only when its token ID matches
`AUTH_AGENT_VERIFICATION_TOKEN_ID`. Keep these values paired and separate from
the pending Agent Token. If the callback is unavailable, verification fails
closed and any previous active Token remains valid.

The same dedicated credential attests active Agent business requests in the
internal `X-Mmdash-Gateway-Authorization` header while the Agent Token remains
the primary `Authorization` identity. Direct Agent access is limited to
`GET /v1/auth/me`; all other `/v1` routes fail closed without the exact trusted
Gateway Token ID, admin role, and compatible Project scope. Never replace this
check with a caller-controlled Tool-name header.

## Session, Run, SSE, and Context acceptance

Exercise create/list/get/rename/end/continue/fork/default for all stable
Session types. Logical end must not delete Hermes history. Product chat starts
a Hermes Run, persists local/remote IDs, streams normalized SSE without proxy
buffering, renders safe Tool Call progress, responds to a stable approval ID,
and stops the same Run. Regenerate must fork and replay; rerun must replay in
the current Session.

`context.promote` accepts optional local `agent_session_id` and `agent_run_id`
only as a pair. Core must verify that both belong to the authenticated Agent
and Project and that the Run belongs to the Session. External Hermes Runs may
omit both. The result is always a pending Context Proposal; an Agent cannot
review it.

## Focused checks

```bash
pnpm contracts:generate
pnpm contracts:check
pnpm api:check
go test ./backend/internal/agent/... ./backend/internal/auth ./backend/internal/datahub ./backend/internal/project
pnpm --filter @mmdash/web-bff test
pnpm --filter @mmdash/mcp-gateway test
pnpm --filter @mmdash/web test
pnpm check
```

Full acceptance uses the repository Compose build, `pnpm smoke`, service
health/readiness, and recent log inspection, then stops with `docker compose
... down` without `-v`. When no real Hermes instance is available, run the
pinned mock HTTP/SSE acceptance and report explicitly that real Hermes
interoperability was not executed.
