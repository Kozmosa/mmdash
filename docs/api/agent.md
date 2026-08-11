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
SSE, Run, and Job APIs into the normalized boundary. The lightweight `Probe`
requires the exact method and path for every Session and Run endpoint that
mmdash calls. Hermes v2026.8.3 advertises `jobs_admin=false` and omits Jobs
from its capability endpoint; the adapter therefore does not require Jobs in
that payload. Job support is confirmed independently by a real `GET
/api/jobs` probe, and automatic progress evaluation, Cron creation, and
event-triggered Runs belong to Stage 6.

`Probe` is not by itself evidence that the runtime can execute a chat Run.
During instance creation and an explicit `runtime`/`all` connection check,
Core follows a successful Probe with the adapter's bounded `CheckRuntime`
exercise. It creates a temporary Session with a fixed, tool-free prompt,
reads the Session and its messages, starts one short Run, calls the real
`POST /v1/runs/{run_id}/stop`, drains the live SSE queue (Hermes does not
replay it), reads Run status, and best-effort deletes the temporary Session.
The stop-before-SSE order relies on Hermes v2026.8.3 retaining the queue until
the stream drains; the exercise has explicit timeouts and never runs from a
background health endpoint. Cleanup errors are surfaced without replacing a
primary runtime error, and the persisted `runtime_check` remains an aggregate
status/category rather than per-operation evidence.

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

The reviewed grant contract is deliberately closed to these seven Tool names:
`project.get`, `data.list`, `data.read`, `context.promote`, `progress.get`, and
`progress.recalculate`, plus `artifact.upload`. The last Tool is the only
Agent mutation that creates file content: it creates an immutable Artifact
classified as `kind=agent` and `source=agent` through short-lived direct
multipart grants. OpenAPI request,
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
instance, Project, bounded mmdash Session ID, request ID, and the single-use
challenge carried by the one-time MCP endpoint. Core stores only the challenge
hash and consumes it when writing the first evidence record. Repeated
`tools/list` callbacks are idempotent: Core
returns that original evidence, and Gateway accepts it only when the stable
Token, Agent, Project, and `tools/list` binding still matches. The first
Session and request IDs are never overwritten. Core accepts the callback only
when all of these are true:

- the caller is the same pending `agent` identity named by the target Token;
- the Token, Agent instance, and Project bindings all match;
- the challenge hashes to the unconsumed value stored for that Token; and
- the target Token is still pending and bound to the supplied Agent and
  Project.

Gateway authenticates that callback with the same pending Agent Token; there is
no second Gateway credential. A missing, stale, or mismatched challenge fails
closed without disclosing it. If the callback is unavailable, Gateway returns
a stable, credential-free `AGENT_VERIFICATION_UNAVAILABLE` failure and does
not report the pending `tools/list` as successful verification.

After activation, Gateway forwards the original Agent Token to Core for every
business Tool call. Gateway checks the exact Tool grant first; Core then
authenticates the same Agent identity and remains the final Project/domain
permission authority. Core is private on the deployment network. Caddy routes
the user/CLI `/v1` surface through Web BFF, which admits only user sessions,
user API Tokens, and explicitly public operations; it rejects Agent and
service credentials.

Create and update requests may set `request_timeout_seconds` from 1 through
300. The Hermes Adapter may further reduce that value to its
deployment-configured maximum; the browser cannot raise the server-side cap.

Hermes connects directly to MCP Gateway:

```text
Hermes -- Agent Token --> MCP Gateway -- same Agent Token --> private Core
                                                               --> Project/Data Hub
```

The Go CLI is not part of Agent authentication, provisioning, or runtime
transport.

## Sessions, messages, and Runs

mmdash stores a local Session index and the corresponding Hermes Session ID.
Supported persisted Session types are `main`, `progress`, and `experiment`.
The human Agent API and workbench can create only `main`; `progress` and
`experiment` are internal module-owned Sessions used by automatic evaluation
and result analysis and are hidden from the human Session list.

The API supports creating, listing, selecting a default, renaming, ending,
continuing, and forking Sessions. Ending is a logical state transition with an
`end_reason`; it does not delete Hermes history. Continuing creates or resumes
the product workflow without losing the prior mapping. Forking records a
parent Session and a distinct remote Session.

The chat surface reads normalized Hermes message history and starts a Run for
each user message. Before starting the next Run, Core reads that remote Session
and supplies the newest 200 non-empty user/assistant turns as explicit
`conversation_history`; the stable Hermes Session ID alone is not treated as a
memory guarantee. Product chat uses the Hermes Run API and Run SSE stream so a
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

`POST .../sessions/{sessionId}/runs` accepts optional `reasoning_effort` with
`none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, or `ultra`. Core
validates the closed set and the Hermes Adapter maps it to request-scoped
`model_options.reasoning_effort`; omitting it preserves the runtime default.
Session reads expose the latest local `last_run_id`, allowing the workbench to
recover the authoritative non-terminal Run after switching away and back.

The workbench reconnects to a non-terminal Run's SSE stream after navigation,
renders only safe reasoning availability/status and normalized Tool progress
inline, and reconciles final Hermes history after terminal events. It never
exposes private chain-of-thought. The composer uses Enter to send and
Shift+Enter for a newline; the in-composer stop action targets the current Run.
Session lifecycle actions live in the Session context menu, while Run, Context
Proposal, and Prompt information live in a collapsible right drawer. The drawer
automatically starts closed below the desktop-wide breakpoint so it cannot
cover the composer action, and every create/fork/select transition clears the
previous Session's transient Run state before the new Session becomes active.

Hermes file and image output is not inferred from a provider-local path or
embedded as base64 in chat. A Run that needs to retain a local file calls
`artifact.upload`: `begin` returns direct part PUT grants, `parts` refreshes a
bounded batch, `complete` verifies every ETag plus the declared size/SHA-256,
and `abort` cancels unfinished state. The resulting Artifact is visible in the
normal library and Data Hub. An upload may carry the current local Session/Run
pair; Core validates that provenance and records an output relation so the
available file is rendered inline as an image preview or file card. Run
instructions tell Hermes to upload useful deliverables proactively rather than
waiting for an additional user request.

Output attachments remain outside the live reasoning/Tool sequence and are
ordered after their Run settles. Hermes message-history Tool Calls are treated
as settled history even when the provider omits a terminal status; live
`queued`/`running` state comes only from the current Run stream.

The browser composer may upload up to ten ordinary `kind=attachment`,
`source=user_upload` Artifacts before starting a Run. Core records them as
input relations to that Run and supplies their immutable Artifact/Version IDs
to Hermes. Hermes uses the exact-scope `artifact.read` Tool to obtain a
short-lived authorized download grant. Signed URLs are never copied into chat.
Synthetic attachment messages and remote Hermes messages are reconciled into
one transcript; the persisted final answer and Tool Calls replace matching
streamed state instead of being rendered a second time.

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
state, and version without exposing credentials. The generated Prompt and every
mmdash-started Run explicitly tell the Agent that the chat supports Markdown
and KaTeX-compatible LaTeX (`$...$` / `$$...$$` and `\(...\)` / `\[...\]`),
so it must not downgrade mathematical responses to plain-text formulas because
the renderer is assumed unknown.

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
scope; Core authenticates the same Agent identity and re-applies domain RBAC.
Management, Token, Prompt, Session, and Run transitions are auditable with
bounded, credential-free metadata and publish stable Agent lifecycle events
through the transactional Outbox.

Deleting an Agent instance is a logical removal, not a destructive history
rewrite. Core marks `removed_at`, disables the instance, and revokes its active
Project Grant and credentials. Removed instances disappear from normal lists,
while Agent Sessions, Runs, Audit, and retained Artifact history remain for
traceability.

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
