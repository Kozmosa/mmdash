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
// MOCK_HERMES_RUNTIME_URL (default http://localhost:18642), and
// MOCK_HERMES_RUNTIME_KEY (default hermes-runtime-key).
//
// Covers: instance setup, runtime checks, manual verification evidence,
// exact Tool scope, sessions CRUD/fork/default, message Run lifecycle with
// SSE stream and stop/regenerate/rerun, context.promote provenance, prompt
// override and reset, two-phase rotation, abort, and revocation.

const coreUrl = trim(process.env.CORE_URL ?? "http://localhost:18080");
const mcpUrl = trim(process.env.MCP_URL ?? "http://localhost:19002");
const hermesUrl = trim(
  process.env.MOCK_HERMES_RUNTIME_URL ?? "http://localhost:18642",
);
const hermesKey = process.env.MOCK_HERMES_RUNTIME_KEY ?? "hermes-runtime-key";
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

async function core(path, options = {}, token) {
  const result = await request(`${coreUrl}/v1${path}`, options, token);
  if (!result.response.ok) {
    throw new Error(
      `Core ${options.method ?? "GET"} ${path} -> ${result.status}: ${JSON.stringify(result.body)}`,
    );
  }
  return { ...result, body: result.body };
}

async function mcpInitialize(token) {
  const result = await request(`${mcpUrl}/mcp`, {
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
  });
  const sessionId = result.response.headers.get("mcp-session-id");
  if (result.status !== 200 || !sessionId) {
    throw new Error(
      `MCP initialize -> ${result.status}: ${JSON.stringify(result.body)} (session ${sessionId})`,
    );
  }
  return sessionId;
}

async function mcpRequest(token, sessionId, id, method, params) {
  return request(
    `${mcpUrl}/mcp`,
    {
      method: "POST",
      headers: {
        accept: "application/json, text/event-stream",
        authorization: `Bearer ${token}`,
        ...(sessionId ? { "mcp-session-id": sessionId } : {}),
      },
      body: JSON.stringify({ id, jsonrpc: "2.0", method, ...(params ? { params } : {}) }),
    },
    null,
  );
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
  console.log(`core=${coreUrl} mcp=${mcpUrl} mock-hermes=${hermesUrl}`);

  // Direct mock Hermes sanity: pinned health and capabilities contract.
  const health = await request(`${hermesUrl}/health`, {
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
  const token = login.body.access_token;
  assert(Boolean(token), "admin login issues a Core access token");

  const project = await core("/projects", {
    body: JSON.stringify({
      name: `Agent smoke ${runId}`,
      problem_summary: "Stage 5 end-to-end acceptance",
      problem_title: "Agent sessions",
    }),
    method: "POST",
  });
  const projectId = project.body.id;
  assert(Boolean(projectId), "project creation returns an ID");

  // 1. Manual instance provisioning returns the one-time Agent Token.
  const provisioned = await core(`/projects/${projectId}/agent-instances`, {
    body: JSON.stringify({
      adapter_type: "hermes",
      allowed_tools: allowedTools,
      display_name: "Smoke Hermes",
      hermes_api_key: hermesKey,
      management_mode: "manual",
      profile: "research",
      request_timeout_seconds: 10,
      runtime_url: hermesUrl,
    }),
    method: "POST",
  });
  const instance = provisioned.body.instance;
  const instanceId = instance.agent_instance_id;
  const agentToken = provisioned.body.one_time_credential?.token;
  const tokenId = provisioned.body.one_time_credential?.credential?.id;
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
  );
  assert(
    checked.body.runtime_check?.status === "passed",
    `runtime check passes (${checked.body.runtime_check?.code ?? "?"})`,
  );

  const before = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/project-access/verify`,
    { method: "POST" },
  );
  assert(
    before.body.verified === false,
    "project access is not verified before real tools/list evidence",
  );

  // 3. MCP Gateway: initialize + exact tools/list create verification evidence.
  const gatewaySession = await mcpInitialize(agentToken);
  assert(Boolean(gatewaySession), "MCP initialize negotiates a gateway session");

  const listed = await mcpRequest(agentToken, gatewaySession, 2, "tools/list");
  const toolNames = listed.body?.tools?.map((tool) => tool.name) ?? [];
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

  const dataCall = await mcpRequest(agentToken, gatewaySession, 4, "tools/call", {
    arguments: { project_id: projectId },
    name: "data.list",
  });
  assert(
    dataCall.status === 200 && dataCall.body?.isError !== true,
    "authorized data.list call succeeds through Gateway",
  );

  // 4. After evidence, manual verification passes and the instance activates.
  const after = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/project-access/verify`,
    { method: "POST" },
  );
  assert(after.body.verified === true, "project access verifies after evidence");
  assert(
    after.body.instance.status === "active",
    `instance activates after verification (${after.body.instance.status})`,
  );

  // 5. Session lifecycle: create, default, rename, messages.
  const created = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions`,
    {
      body: JSON.stringify({ default: true, session_type: "main", title: "Main" }),
      method: "POST",
    },
  );
  const sessionId = created.body.session_id;
  assert(
    Boolean(sessionId) && Boolean(created.body.remote_session_id),
    "session create returns local and remote IDs",
  );
  assert(created.body.default === true, "created session becomes the default");

  const sessions = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions`,
  );
  assert(
    sessions.body.items?.some((item) => item.session_id === sessionId),
    "session list includes the created session",
  );

  const renamed = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}`,
    { body: JSON.stringify({ title: "Renamed" }), method: "PATCH" },
  );
  assert(renamed.body.title === "Renamed", "session rename applies");

  const messages = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/messages`,
  );
  assert(
    Array.isArray(messages.body.items) && messages.body.items.length > 0,
    "session message history streams from the mock runtime",
  );

  // 6. Run lifecycle: start, status, SSE stream, stop.
  const started = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs`,
    { body: JSON.stringify({ message: "analyze the project" }), method: "POST" },
  );
  const runIdValue = started.body.run?.run_id;
  assert(Boolean(runIdValue), "StartRun returns a Run ID");

  const runStatus = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runIdValue}`,
  );
  assert(
    ["queued", "running", "completed"].includes(runStatus.body.status),
    `run status readable (${runStatus.body.status})`,
  );

  const events = await streamRunEvents(
    `${coreUrl}/v1/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runIdValue}/events`,
    token,
    ["run.started", "message.delta", "run.completed"],
  );
  assert(
    events.has("run.started") && events.has("run.completed"),
    "SSE run stream emits start and completion events",
  );

  const stopped = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runIdValue}/stop`,
    { method: "POST" },
  );
  assert(
    ["stopping", "stopped"].includes(stopped.body.status),
    "run stop is accepted by the runtime",
  );

  // 7. Regenerate and rerun map onto ReplayRun.
  for (const mode of ["regenerate", "rerun"]) {
    const replayed = await core(
      `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runIdValue}/${mode}`,
      { method: "POST" },
    );
    assert(Boolean(replayed.body.run?.run_id), `${mode} starts a new Run`);
  }

  // 8. Fork and end.
  const fork = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/fork`,
    { body: JSON.stringify({ title: "Fork" }), method: "POST" },
  );
  assert(
    Boolean(fork.body.session_id) && fork.body.session_id !== sessionId,
    "fork creates a distinct child session",
  );
  const ended = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/end`,
    { method: "POST" },
  );
  assert(ended.body.status === "ended", "session end marks the local index");

  // 9. context.promote through MCP with paired provenance lands as a Proposal.
  const promote = await mcpRequest(agentToken, gatewaySession, 5, "tools/call", {
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
    promote.status === 200 && promote.body?.isError !== true,
    "context.promote accepts paired Agent provenance",
  );

  // 10. Project Prompt: auto-generated, custom, reset.
  const prompt = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/prompt`,
  );
  assert(
    typeof prompt.body.effective_prompt === "string" &&
      prompt.body.effective_prompt.length > 0,
    "project Prompt is auto-generated",
  );
  const custom = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/prompt`,
    { body: JSON.stringify({ content: "custom smoke prompt" }), method: "PATCH" },
  );
  assert(custom.body.custom_prompt === "custom smoke prompt", "prompt override applies");
  const reset = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/prompt/reset`,
    { method: "POST" },
  );
  assert(
    reset.body.custom === false && reset.body.effective_prompt.length > 0,
    "prompt reset restores the generated default",
  );

  // 11. Two-phase rotation keeps the old Token valid until verification.
  const rotated = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/tokens/rotate`,
    { body: JSON.stringify({ name: "rotated" }), method: "POST" },
  );
  const pendingToken = rotated.body.one_time_credential?.token;
  const pendingTokenId = rotated.body.one_time_credential?.credential?.id;
  assert(
    Boolean(pendingToken) && pendingToken !== agentToken,
    "rotation issues a distinct pending Token once",
  );
  assert(
    rotated.body.old_credential_remains_active === true,
    "rotation keeps the old active Token valid",
  );

  // 12. Aborting the pending rotation keeps the old Token active.
  const aborted = await core(
    `/projects/${projectId}/agent-instances/${instanceId}/tokens/${pendingTokenId}/abort`,
    { method: "POST" },
  );
  assert(
    aborted.body.old_credential_remains_active === true,
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
  const revokedList = await mcpRequest(agentToken, gatewaySession, 6, "tools/list");
  assert(
    revokedList.status === 401 || revokedList.status === 403,
    "revoked Agent Token is rejected by Gateway",
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
