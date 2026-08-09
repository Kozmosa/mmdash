#!/usr/bin/env node
// Stage 5 Agent end-to-end acceptance against the pinned mock Hermes server.
//
// Requires the acceptance Compose stack on isolated ports:
//   docker compose -f deploy/compose/compose.yaml \
//     -f deploy/compose/compose.acceptance.yaml up --build -d
// and the paired Gateway attestation credential (see compose.acceptance.yaml):
// create a Core admin API token after first start, then restart core and
// mcp-gateway with AUTH_AGENT_VERIFICATION_TOKEN_ID / MCP_CORE_ACCESS_TOKEN.
//
// Env: CORE_URL (default http://localhost:18080), MCP_URL (default
// http://localhost:19002), MMDASH_SMOKE_EMAIL/PASSWORD for the admin login,
// MOCK_HERMES_RUNTIME_URL / MOCK_HERMES_MANAGEMENT_URL (default to the
// mock-hermes Compose service), MOCK_HERMES_RUNTIME_KEY (default
// hermes-runtime-key), and MOCK_HERMES_DASHBOARD_TOKEN (default
// hermes-dashboard-token).
//
// Covers: manual and automatic instance setup, runtime checks and management,
// manual verification evidence,
// exact Tool scope, sessions CRUD/fork/default, message Run lifecycle with
// SSE stream and stop/regenerate/rerun, context.promote provenance, prompt
// override and reset, two-phase rotation, abort, and revocation.

const coreUrl = trim(process.env.CORE_URL ?? "http://localhost:18080");
const mcpUrl = trim(process.env.MCP_URL ?? "http://localhost:19002");
// Host-side URL used by this script for the direct mock sanity check.
const hermesHealthUrl = trim(
  process.env.MOCK_HERMES_HEALTH_URL ?? "http://localhost:18642",
);
// URL stored on the Agent instance; must be reachable from Core and use an
// allowed runtime port, so it points at the Compose service name.
const hermesRuntimeUrl = trim(
  process.env.MOCK_HERMES_RUNTIME_URL ?? "http://mock-hermes:8642",
);
const hermesManagementUrl = trim(
  process.env.MOCK_HERMES_MANAGEMENT_URL ?? hermesRuntimeUrl,
);
const hermesKey = process.env.MOCK_HERMES_RUNTIME_KEY ?? "hermes-runtime-key";
const hermesDashboardToken =
  process.env.MOCK_HERMES_DASHBOARD_TOKEN ?? "hermes-dashboard-token";
const email = process.env.MMDASH_SMOKE_EMAIL ?? "admin@mmdash.local";
const password = process.env.MMDASH_SMOKE_PASSWORD ?? "mmdash-local-admin";
const runId = `${Date.now()}-${process.pid}`;

const allowedTools = ["project.get", "data.list", "data.read", "context.promote"];

let failures = 0;
function assert(condition, message) {
  if (!condition) {
    failures += 1;
    console.error(`FAIL: ${message}`);
  } else {
    console.log(`ok: ${message}`);
  }
}

function trim(value) {
  return typeof value === "string" ? value.trim() : value;
}

async function request(url, options = {}, token) {
  const headers = {
    "content-type": "application/json",
    ...(token ? { authorization: `Bearer ${token}` } : {}),
    ...(options.headers ?? {}),
  };
  const response = await fetch(url, { ...options, headers });
  const text = await response.text();
  let body = null;
  try {
    body = JSON.parse(text);
  } catch {
    body = text;
  }
  return { body, response, status: response.status };
}

async function core(path, options, token) {
  const result = await request(`${coreUrl}/v1${path}`, options, token);
  if (!result.response.ok) {
    throw new Error(
      `Core ${options.method ?? "GET"} ${path} -> ${result.status}: ${JSON.stringify(result.body)}`,
    );
  }
  return result.body;
}

async function mcpInitialize(token) {
  const result = await request(
    `${mcpUrl}/mcp`,
    {
      method: "POST",
      headers: {
        accept: "application/json, text/event-stream",
        authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({
        id: 1,
        jsonrpc: "2.0",
        method: "initialize",
        params: {
          clientInfo: { name: "mmdash-agent-smoke", version: "0.1.0" },
          capabilities: {},
          protocolVersion: "2025-06-18",
        },
      }),
    },
    null,
  );
  const sessionId = result.response.headers.get("x-mmdash-session-id");
  if (result.status !== 200 || !sessionId) {
    throw new Error(
      `MCP initialize -> ${result.status}: ${JSON.stringify(result.body)} (session ${sessionId})`,
    );
  }
  return sessionId;
}

async function mcpRequest(token, sessionId, id, method, params) {
  const result = await request(
    `${mcpUrl}/mcp`,
    {
      method: "POST",
      headers: {
        accept: "application/json, text/event-stream",
        authorization: `Bearer ${token}`,
        ...(sessionId ? { "x-mmdash-session-id": sessionId } : {}),
      },
      body: JSON.stringify({ id, jsonrpc: "2.0", method, ...(params ? { params } : {}) }),
    },
    null,
  );
  // Streamable HTTP may answer with an SSE frame; unwrap the data payload.
  if (typeof result.body === "string" && result.body.includes("\ndata: ")) {
    const match = result.body.match(/data: (\{.*\})\n/s);
    if (match) {
      try {
        result.body = JSON.parse(match[1]);
      } catch {
        // keep the raw text when the payload is not JSON
      }
    }
  }
  return result;
}

async function streamRunEvents(url, token, expected) {
  const response = await fetch(url, {
    headers: { authorization: `Bearer ${token}` },
  });
  if (!response.ok || !response.body) {
    throw new Error(`SSE stream -> ${response.status}`);
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  const seen = new Set();
  try {
    for (let index = 0; index < 200; index += 1) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const frames = buffer.split("\n\n");
      buffer = frames.pop() ?? "";
      for (const frame of frames) {
        for (const line of frame.split("\n")) {
          if (line.startsWith("event:")) {
            seen.add(line.slice(6).trim());
          }
        }
      }
      if (expected.every((name) => seen.has(name))) break;
    }
  } finally {
    reader.releaseLock();
  }
  return seen;
}

async function main() {
  console.log(`agent smoke ${runId}`);
  console.log(`core=${coreUrl} mcp=${mcpUrl} mock-hermes=${hermesRuntimeUrl}`);

  // Direct mock Hermes sanity: pinned health contract.
  const health = await request(`${hermesHealthUrl}/health`, {
    headers: { authorization: `Bearer ${hermesKey}` },
  });
  assert(
    health.status === 200 && health.body?.version === "2026.8.3",
    "mock Hermes health is pinned to v2026.8.3",
  );

  const login = await core("/auth/login", {
    body: JSON.stringify({ email, password }),
    method: "POST",
  });
  const token = login.access_token;
  assert(Boolean(token), "admin login issues a Core access token");

  const project = await core(
    "/projects",
    {
      body: JSON.stringify({
        name: `Agent smoke ${runId}`,
        problem_summary: "Stage 5 end-to-end acceptance",
        problem_title: "Agent sessions",
      }),
      method: "POST",
    },
    token,
  );
  const projectId = project.id;
  assert(Boolean(projectId), "project creation returns an ID");

  // 1. Manual instance provisioning returns the one-time Agent Token.
  const provisioned = await core(
    `/projects/${projectId}/agent-instances`,
    {
      body: JSON.stringify({
        adapter_type: "hermes",
        allowed_tools: allowedTools,
        display_name: "Smoke Hermes",
        hermes_api_key: hermesKey,
        management_mode: "manual",
        profile: "research",
        request_timeout_seconds: 10,
        runtime_url: hermesRuntimeUrl,
      }),
      method: "POST",
    },
    token,
  );
  const instance = provisioned.instance;
  const instanceId = instance.agent_instance_id;
  const agentToken = provisioned.one_time_credential?.token;
  const tokenId = provisioned.one_time_credential?.credential?.id;
  assert(
    Boolean(agentToken) && agentToken.startsWith("mmdash_"),
    "manual provisioning returns a one-time opaque Agent Token",
  );
  assert(
    instance.management_mode === "manual" && instance.status === "setup_pending",
    `instance starts setup_pending (${instance.status})`,
  );
  assert(
    instance.capabilities?.project_access?.verify === true,
    "instance exposes declared project-access capabilities",
  );

  // 2. Runtime check passes while project access is still unverified.
  const checked = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/checks`,
    { body: JSON.stringify({ scope: "all" }), method: "POST" },
    token,
  );
  const runtimeCheck = checked.instance?.runtime_check;
  assert(
    runtimeCheck?.status === "passed",
    `runtime check passes (${runtimeCheck?.code ?? runtimeCheck?.status ?? "?"})`,
  );

  const before = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/project-access/verify`,
    { method: "POST" },
    token,
  );
  assert(
    before.verified === false,
    "project access is not verified before real tools/list evidence",
  );

  // 3. MCP Gateway: initialize + exact tools/list create verification evidence.
  const gatewaySession = await mcpInitialize(agentToken);
  assert(Boolean(gatewaySession), "MCP initialize negotiates a gateway session");

  const listed = await mcpRequest(agentToken, gatewaySession, 2, "tools/list");
  const toolNames = listed.body?.result?.tools?.map((tool) => tool.name) ?? [];
  assert(
    listed.status === 200 &&
      toolNames.sort().join(",") === allowedTools.sort().join(","),
    `tools/list returns exactly the reviewed tools (${toolNames.join(",")})`,
  );

  const denied = await mcpRequest(agentToken, gatewaySession, 3, "tools/call", {
    arguments: { project_id: projectId },
    name: "project.member.list",
  });
  assert(denied.status === 403, "exact Tool scope denies an unlisted tool");

  const deniedBusiness = await mcpRequest(agentToken, gatewaySession, 4, "tools/call", {
    arguments: { project_id: projectId },
    name: "data.list",
  });
  assert(
    deniedBusiness.status === 403 &&
      deniedBusiness.body?.code === "AGENT_CREDENTIAL_PENDING",
    "pending Agent credential cannot call business tools",
  );

  // 4. Manual activation: evidence exists, so VerifyToken activates the
  // pending Token and the reverse connection is then verified.
  const activated = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/tokens/${tokenId}/verify`,
    { method: "POST" },
    token,
  );
  assert(
    activated.verified === true && activated.credential?.status === "active",
    "manual VerifyToken activates the pending Token after evidence",
  );

  const after = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/project-access/verify`,
    { method: "POST" },
    token,
  );
  assert(after.verified === true, "project access verifies after evidence");
  assert(
    after.instance.status === "active",
    `instance activates after verification (${after.instance.status})`,
  );

  // 5. The active Token can call authorized business tools.
  const dataCall = await mcpRequest(agentToken, gatewaySession, 5, "tools/call", {
    arguments: { project_id: projectId },
    name: "data.list",
  });
  assert(
    dataCall.status === 200 && dataCall.body?.result?.isError !== true,
    "authorized data.list call succeeds through Gateway",
  );

  // 5. Session lifecycle: create, default, rename, messages.
  const created = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions`,
    {
      body: JSON.stringify({ default: true, session_type: "main", title: "Main" }),
      method: "POST",
    },
    token,
  );
  const sessionId = created.session_id;
  assert(
    Boolean(sessionId) && Boolean(created.remote_session_id),
    "session create returns local and remote IDs",
  );
  assert(created.default === true, "created session becomes the default");

  const sessions = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions`,
    {},
    token,
  );
  assert(
    sessions.items?.some((item) => item.session_id === sessionId),
    "session list includes the created session",
  );

  const renamed = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}`,
    { body: JSON.stringify({ title: "Renamed" }), method: "PATCH" },
    token,
  );
  assert(renamed.title === "Renamed", "session rename applies");

  const messages = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/messages`,
    {},
    token,
  );
  assert(
    Array.isArray(messages.items) && messages.items.length > 0,
    "session message history streams from the mock runtime",
  );

  // 6. Run lifecycle: start, status, SSE stream, stop.
  const started = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs`,
    { body: JSON.stringify({ message: "analyze the project" }), method: "POST" },
    token,
  );
  const runIdValue = started.run?.run_id;
  assert(Boolean(runIdValue), "StartRun returns a Run ID");

  const runStatus = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runIdValue}`,
    {},
    token,
  );
  assert(
    ["queued", "running", "completed"].includes(runStatus.status),
    `run status readable (${runStatus.status})`,
  );

  const events = await streamRunEvents(
    `${coreUrl}/v1/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runIdValue}/events`,
    token,
    ["message.delta", "run.completed"],
  );
  assert(
    events.has("message.delta") && events.has("run.completed"),
    "SSE run stream emits message and completion events",
  );

  const stopped = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runIdValue}/stop`,
    { method: "POST" },
    token,
  );
  assert(
    ["stopping", "stopped"].includes(stopped.status),
    "run stop is accepted by the runtime",
  );

  // 7. Rerun and regenerate map onto ReplayRun. Regenerate forks the
  // session, which ends the parent, so rerun must run first.
  for (const mode of ["rerun", "regenerate"]) {
    const replayed = await core(
      `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runIdValue}/${mode}`,
      { method: "POST" },
      token,
    );
    assert(Boolean(replayed.run?.run_id), `${mode} starts a new Run`);
  }

  // 8. Fork and end.
  const fork = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/fork`,
    { body: JSON.stringify({ title: "Fork" }), method: "POST" },
    token,
  );
  assert(
    Boolean(fork.session_id) && fork.session_id !== sessionId,
    "fork creates a distinct child session",
  );
  const ended = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/end`,
    { method: "POST" },
    token,
  );
  assert(ended.status === "ended", "session end marks the local index");

  // 9. context.promote through MCP with paired provenance lands as a Proposal.
  const promote = await mcpRequest(agentToken, gatewaySession, 6, "tools/call", {
    arguments: {
      agent_run_id: runIdValue,
      agent_session_id: sessionId,
      content: "smoke conclusion",
      context_type: "finding",
      project_id: projectId,
      title: "Smoke finding",
    },
    name: "context.promote",
  });
  assert(
    promote.status === 200 && promote.body?.result?.isError !== true,
    "context.promote accepts paired Agent provenance",
  );

  // 10. Project Prompt: auto-generated, custom, reset.
  const prompt = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/prompt`,
    {},
    token,
  );
  assert(
    typeof prompt.effective_prompt === "string" &&
      prompt.effective_prompt.length > 0,
    "project Prompt is auto-generated",
  );
  const custom = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/prompt`,
    { body: JSON.stringify({ content: "custom smoke prompt" }), method: "PATCH" },
    token,
  );
  assert(
    custom.custom_prompt === "custom smoke prompt",
    "prompt override applies",
  );
  const reset = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/prompt/reset`,
    { method: "POST" },
    token,
  );
  assert(
    reset.custom === false && reset.effective_prompt.length > 0,
    "prompt reset restores the generated default",
  );

  // 11. Two-phase rotation keeps the old Token valid until verification.
  const rotated = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/tokens/rotate`,
    { body: JSON.stringify({ name: "rotated" }), method: "POST" },
    token,
  );
  const pendingToken = rotated.one_time_credential?.token;
  const pendingTokenId = rotated.one_time_credential?.credential?.id;
  assert(
    Boolean(pendingToken) && pendingToken !== agentToken,
    "rotation issues a distinct pending Token once",
  );
  assert(
    rotated.old_credential_remains_active === true,
    "rotation keeps the old active Token valid",
  );

  // 12. Aborting the pending rotation keeps the old Token active.
  const aborted = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/tokens/${pendingTokenId}/abort`,
    { method: "POST" },
    token,
  );
  assert(
    aborted.old_credential_remains_active === true,
    "abort revokes only the pending Token",
  );

  // 13. Explicit revocation blocks the Token immediately.
  const revoked = await request(
    `${coreUrl}/v1/projects/${projectId}/agent-instances/${instanceId}/tokens/${tokenId}`,
    { method: "DELETE" },
    token,
  );
  assert(revoked.status === 204, "explicit revoke takes effect immediately");

  // 14. Revoked Token can no longer list tools.
  const revokedList = await mcpRequest(agentToken, gatewaySession, 7, "tools/list");
  assert(
    revokedList.status === 401 || revokedList.status === 403,
    "revoked Agent Token is rejected by Gateway",
  );

  // 15. Automatic management configures Hermes directly, verifies the
  // product Token through the real Gateway, and never returns plaintext.
  const autoProvisioned = await core(
    `/projects/${projectId}/agent-instances`,
    {
      body: JSON.stringify({
        adapter_type: "hermes",
        allowed_tools: allowedTools,
        dashboard_session_token: hermesDashboardToken,
        display_name: "Auto Smoke Hermes",
        hermes_api_key: hermesKey,
        management_mode: "auto",
        management_url: hermesManagementUrl,
        profile: "research",
        request_timeout_seconds: 10,
        runtime_url: hermesRuntimeUrl,
      }),
      method: "POST",
    },
    token,
  );
  const autoInstance = autoProvisioned.instance;
  const autoInstanceId = autoInstance.agent_instance_id;
  assert(
    autoProvisioned.one_time_credential === undefined,
    "auto provisioning never returns the one-time Agent Token",
  );
  assert(
    autoInstance.management_mode === "auto" && autoInstance.status === "active",
    `auto-managed instance activates (${autoInstance.status})`,
  );
  assert(
    autoInstance.management_path === "direct" &&
      autoInstance.management_check?.status === "passed" &&
      autoInstance.project_access_check?.status === "passed",
    "auto management verifies the Dashboard and real Gateway tool access",
  );

  const autoVerified = await core(
    `/projects/${projectId}/agent-instances/${autoInstanceId}/project-access/verify`,
    { method: "POST" },
    token,
  );
  assert(autoVerified.verified === true, "auto project access remains verified");

  const autoRotated = await core(
    `/projects/${projectId}/agent-instances/${autoInstanceId}/tokens/rotate`,
    { body: JSON.stringify({ name: "auto-rotated" }), method: "POST" },
    token,
  );
  assert(
    autoRotated.rotation_status === "completed" &&
      autoRotated.credential?.status === "active" &&
      autoRotated.old_credential_remains_active === false &&
      autoRotated.one_time_credential === undefined,
    "auto rotation activates the replacement without returning plaintext",
  );

  console.log(
    failures === 0
      ? `\nagent smoke PASSED (${runId})`
      : `\nagent smoke FAILED: ${failures} assertion(s)`,
  );
  process.exitCode = failures === 0 ? 0 : 1;
}

main().catch((error) => {
  console.error(`agent smoke error: ${error.message}`);
  process.exitCode = 1;
});
