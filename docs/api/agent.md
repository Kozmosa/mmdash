# Agent sessions API and runtime boundary

Stage 5 adds the human-operated mmdash Agent workflow. Core owns Agent
instances, Project Grants, Agent Token lifecycle, project Prompt overrides,
Session indexes, remote ID mappings, Runs, Audit, and Outbox records. Hermes
continues to own complete message history and runtime execution state.

The canonical HTTP schemas are
[`core.yaml`](../../contracts/openapi/core.yaml) and
[`web-bff.yaml`](../../contracts/openapi/web-bff.yaml). Search the complete
operation list in [`endpoints.md`](endpoints.md).

## Runtime-independent boundary

`AgentAdapter` is the product boundary. It exposes normalized capability
checks, Session lifecycle, message history, streaming, Run control, Job API
mapping, and project-access configuration and verification. It does not own
database state and does not expose Hermes routes, SSE names, Profile fields,
Dashboard authentication, or other provider-specific details.

Stage 5 registers only `HermesAdapter`. Its implementation and mock contracts
target the authoritative upstream release:

```text
Repository: NousResearch/hermes-agent
Tag: v2026.8.3
Hermes Agent: 0.20.0
Commit: 3c27eb6234bf91b8ceee9e9071591b31e9b148cb
```

The adapter maps Hermes health, authentication, capability, Session, message,
SSE, Run, and Job APIs into the normalized boundary. Jobs are capability and
API mappings only in Stage 5; automatic progress evaluation, Cron creation,
and event-triggered Runs belong to Stage 6.

## Instance setup and management modes

An Agent instance has one Project Grant, one exact Tool allowlist, and one of
two management modes:

Its Hermes `profile` is a canonical lowercase identifier matching
`[a-z0-9][a-z0-9_-]{0,63}`. The special `default` profile is valid; `hermes`,
`test`, `tmp`, `root`, and `sudo` are reserved. Profile input is never silently
trimmed or lowercased. When `profile` is omitted while creating an instance,
the `default` profile is used.

| Mode     | mmdash responsibility | User responsibility |
| -------- | --------------------- | ------------------- |
| `manual` | Check Hermes runtime, issue a one-time Agent Token, and verify a real Hermes-to-Gateway call | Install or update the mmdash MCP entry in Hermes and rotate its credential |
| `auto`   | Use an authenticated Dashboard management connection to install a versioned MCP entry, verify it, activate the Token, and safely retire the old entry | Supply an address and management credentials reachable from the Adapter process |

The optional manual `management_url` is a browser convenience link only. Its
presence does not authorize Core to change Hermes configuration. Auto mode
requires the Dashboard management API and may additionally use Cloudflare
Access service credentials. A Dashboard Session Token alone does not make an
otherwise exposed non-loopback Dashboard safe.

All server-side management requests enforce URL parsing, approved schemes and
ports, DNS resolution, redirect, timeout, response-size, loopback, private
network, link-local, metadata-address, and DNS-rebinding policy. The browser
does not probe these addresses.

API responses return only configuration booleans and safe check categories.
They never return a stored Hermes API Key, Dashboard Session Token,
Cloudflare Secret, provider response body, or credential-bearing URL.

`POST agent.project_access.verify` re-runs the reverse-connection check and
returns `verified=false` until evidence actually exists. In `manual` mode
mmdash cannot drive Hermes, so the endpoint stays at
`gateway_verification_missing` and the instance remains `setup_pending` until
MCP Gateway records a real credential-owned `tools/list` evidence callback for
an active Agent Token (see the Token lifecycle below). In `auto` mode the
Adapter exercises the reverse connection through the managed Dashboard API.
Runtime and management checks never satisfy this requirement on their own.

## Product Agent Token lifecycle

Agent Tokens are Auth-issued high-entropy opaque credentials. Core stores only
their SHA-256 hashes. Each Token is bound to one Agent instance, Project
Grant, Project, and exact MCP Tool names; wildcards are not accepted. Token
plaintext is returned at most once during a manual issue or rotation and is
never returned by ordinary instance, credential, Audit, event, or log reads.

The Stage 6 grant contract is deliberately closed to these six Tool names:
`project.get`, `data.list`, `data.read`, `context.promote`, `progress.get`, and
`progress.recalculate`. OpenAPI request,
identity, Grant, and credential schemas use the same enum as the Core Agent
domain; a syntactically valid but unowned MCP Tool name is therefore rejected
at the browser/Core contract boundary. Expanding this set requires an explicit
later-stage contract and domain change rather than a free-form scope string.

Changing an instance's `allowed_tools` is a credential-scope change, not a
plain Grant edit. `PATCH` therefore returns an
`AgentInstanceProvisioningResult`: ordinary settings changes contain only the
updated `instance`, while a manual Tool-scope change may include one-time
rotation material. Auto mode consumes the new plaintext server-side. When an
old Token exists, both modes keep it and its prior scope valid until the new
Token is verified; the implementation never silently widens an already issued
Token.

Rotation is a two-phase workflow:

1. Issue the Grant's only new `pending` Token while the old `active` Token
   remains valid. Concurrent pending rotations for the same Grant are rejected.
2. In manual mode, wait for the user to update Hermes. In auto mode, add a
   parallel versioned MCP entry and reload or restart the Hermes Gateway when
   required.
3. Verify that the new identity completes MCP initialization and then performs
   protocol negotiation (current `server/discover`, or legacy `initialize`)
   and then performs a successful `tools/list` through MCP Gateway with the
   bound Project and exact Tool scope.
4. Activate the new Token and only then revoke the old Token.

Verification or management failure leaves any old Token active. Initial auto
provisioning has no old Token, so its failure leaves the instance pending and
reports `old_token_remains_active=false`; a failed replacement reports `true`.
Aborting a pending rotation revokes only the new credential. Explicit revoke
takes effect immediately; manual cleanup of Hermes configuration remains the
user's responsibility.

An expired pending Token cannot receive verification evidence or be activated,
including when it expires after `tools/list` but before activation. Expiry and
pending status are checked both before the Auth operation and again inside the
activation transaction, so a concurrent expiry or replacement cannot revoke
the old active Token. Issuing a replacement serializes on the Project Grant and
atomically retires any expired pending credential before inserting the new one;
an expired manual rotation therefore cannot permanently block recovery.

`auth.me` is authentication, not proof that Hermes completed the reverse
connection. Current MCP `server/discover` and legacy `initialize` only mark the
Session as negotiated and are insufficient on their own. After the first
successful `tools/list` in a negotiated MCP Session, Gateway calls
`auth.agent_tokens.verification.record` with the pending Token ID, Agent
instance, Project, bounded mmdash Session ID, and request ID. Core stores the
first evidence record. Repeated `tools/list` callbacks are idempotent: Core
returns that original evidence, and Gateway accepts it only when the stable
Token, Agent, Project, and `tools/list` binding still matches. The first
Session and request IDs are never overwritten. Core accepts the callback only
when all of these are true:

- the caller is an admin `api` identity;
- its Auth Token ID exactly equals `AUTH_AGENT_VERIFICATION_TOKEN_ID`;
- any Project scope on that API Token equals the evidence Project; and
- the target Token is still pending and bound to the supplied Agent and
  Project.

The matching secret is configured on Gateway as `MCP_CORE_ACCESS_TOKEN` and is
never the pending Agent Token. A normal admin credential cannot stand in for
the configured Gateway credential. If the callback is unavailable, Gateway
returns a stable, credential-free `AGENT_VERIFICATION_UNAVAILABLE` failure and
does not report the pending `tools/list` as successful verification.

After activation, Gateway keeps the product Agent Token as the primary Core
identity and sends `MCP_CORE_ACCESS_TOKEN` separately in the internal
`X-Mmdash-Gateway-Authorization` attestation header. Except for direct
`GET /v1/auth/me` introspection, Core rejects every Agent-authenticated `/v1`
request without that second credential. Core accepts the attestation only for
the configured Token ID, an active admin API Token, and a matching optional
Project scope. The header does not replace the Agent actor and does not carry a
Tool name; Gateway exact-Tool authorization and Core Agent RBAC both still run.

Create and update requests may set `request_timeout_seconds` from 1 through
300. The Hermes Adapter may further reduce that value to its
deployment-configured maximum; the browser cannot raise the server-side cap.

Hermes connects directly to MCP Gateway:

```text
Hermes -- Agent Token --> MCP Gateway -- Agent Token + Gateway attestation --> Core
                                                                        --> Project/Data Hub
```

The Go CLI is not part of Agent authentication, provisioning, or runtime
transport.

## Sessions, messages, and Runs

mmdash stores a local Session index and the corresponding Hermes Session ID.
Supported Session types are `main`, `progress`, and `experiment`. Stage 5 uses
`main` for human chat; the other two stable types reserve module ownership for
later Progress and Experiment workflows.

The API supports creating, listing, selecting a default, renaming, ending,
continuing, and forking Sessions. Ending is a logical state transition with an
`end_reason`; it does not delete Hermes history. Continuing creates or resumes
the product workflow without losing the prior mapping. Forking records a
parent Session and a distinct remote Session.

The chat surface reads normalized Hermes message history and starts a Run for
each user message. Product chat uses the Hermes Run API and Run SSE stream so a
Run has a stable remote ID and can be stopped. The normalized SSE stream passes
through without proxy buffering and returns message, Tool Call
start/progress/completion, approval, subagent, Run status, done, heartbeat, and
safe error events. The Core boundary may accept `Last-Event-ID` for client
compatibility, but pinned Hermes v2026.8.3 neither reads that header nor emits
`id:` fields; the adapter therefore does not forward it. Hermes Run events are
live-only and its queue is consumed once, so disconnect recovery relies on
polling Run status and reconciling Hermes message history rather than event
replay. `tool.progress` carries only the normalized Tool Call summary. Raw Tool
arguments, Tool results, reasoning, provider errors, and secrets are not part
of the browser contract.

When a Run enters `waiting_for_approval`, the SSE event carries only the
provider-neutral approval ID and allowed choices. The browser responds through
`POST .../runs/{runId}/approvals/{approvalId}` with exactly one of `once`,
`session`, `always`, or `deny`; Core returns the current normalized Run. The
approval request never exposes raw Tool arguments or bypasses Hermes policy.
Pinned Hermes v2026.8.3 has no provider approval-ID field and resolves only its
oldest pending request when `resolve_all=false`, so Core generates and persists
the normalized ID, accepts only the FIFO head, and maps an ID-less upstream
response back to that persisted ID. This limitation is documented with exact
upstream source references in `docs/development/agent.md`.

`regenerate` is non-destructive: it forks the Session and replays the selected
or latest user turn. `rerun` replays the selected or latest user turn in the
current Session. Both record their source Run for traceability.

## Project Prompt and Context Proposal

Core generates a default project Prompt from authorized, versioned project
facts. A user can store a Project-specific override or reset it to the current
generated default. Prompt reads identify the default, effective text, custom
state, and version without exposing credentials.

The MCP Tool `context.promote` submits an explicit conclusion through the
existing Data Hub Context Proposal boundary. For Runs started by mmdash, the
Run instructions safely provide the local Session and Run IDs; the Tool may
send them only as a pair. Core verifies that the Session belongs to the
authenticated Agent and Project and that the Run belongs to that Session.
External or otherwise non-mmdash Hermes Runs may omit both IDs. In every case,
an Agent can create only a pending Proposal and cannot accept or reject it;
review remains a human permission.

The Agent workbench lists pending Agent-created Context Proposals without
discarding their Agent, Session, Run, or source-object provenance. A signed
browser session with `project.context.review` may accept or reject each item
and add an optional review note; the browser never rewrites the proposed
content or its provenance.

## Authorization, Audit, events, and diagnostics

Project RBAC separates Agent read, use, instance management, and Token
management. MCP Gateway checks Token status, Project binding, and exact Tool
scope; Core authenticates the same Agent identity, requires the dedicated
Gateway attestation for business routes, and re-applies domain RBAC.
Management, Token, Prompt, Session, and Run transitions are auditable with
bounded, credential-free metadata and publish stable Agent lifecycle events
through the transactional Outbox.

Connection checks report runtime, authentication, capabilities, Sessions,
messages, SSE, Runs, Jobs, management, and reverse project-access results
independently. Metrics use bounded adapter, mode, operation, and outcome labels;
they never include URLs, Session IDs, Run IDs, Tool arguments, or Tokens.

Automated acceptance uses a mock Hermes HTTP/SSE server pinned to the version
above. Real Hermes interoperability remains an environment-dependent release
check and must be reported explicitly when it was not run.

The 2026-08-10 release check ran the pinned upstream tag and commit against the
production Core, Worker, MCP Gateway, PostgreSQL, and Dashboard management
paths. It covered capability probing, Sessions, numeric message IDs, Runs, SSE,
stop, approvals, Jobs, Tool progress, manual and automatic MCP setup, Token
activation and rotation, and a Stage 6 `core_agent` Progress evaluation. That
check established two provider-compatibility rules now protected by contract
tests: Hermes capability metadata may contain non-boolean values beside feature
flags, and message IDs may be either JSON numbers or opaque strings.
