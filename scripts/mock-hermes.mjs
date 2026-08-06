#!/usr/bin/env node
// Pinned mock Hermes HTTP/SSE server for Stage 5 automated acceptance.
//
// Implements the exact v2026.8.3 contract pinned by
// backend/internal/agent/hermes/adapter_contract_test.go: runtime health,
// capabilities, Sessions CRUD/fork/messages/chat/stream, Runs
// start/get/stop/approval/events, Jobs CRUD, and the Dashboard management API
// (health, MCP servers CRUD + test, gateway restart). It never implements a
// real Hermes: run streams return canned event sequences and tool results are
// static. Real Hermes interoperability is a separate environment check.
//
// Runtime endpoints authenticate with Authorization: Bearer
// MOCK_HERMES_RUNTIME_KEY; Dashboard endpoints authenticate with
// X-Hermes-Session-Token: MOCK_HERMES_DASHBOARD_TOKEN and optionally require
// CF-Access-Client-Id/Secret when MOCK_HERMES_CLOUDFLARE_REQUIRE=1.

import { createServer } from "node:http";

const PORT = Number(process.env.MOCK_HERMES_PORT ?? 8642);
const RUNTIME_KEY = process.env.MOCK_HERMES_RUNTIME_KEY ?? "hermes-runtime-key";
const DASHBOARD_TOKEN =
  process.env.MOCK_HERMES_DASHBOARD_TOKEN ?? "hermes-dashboard-token";
const CLOUDFLARE_REQUIRE = process.env.MOCK_HERMES_CLOUDFLARE_REQUIRE === "1";
const CF_CLIENT_ID = process.env.MOCK_HERMES_CF_CLIENT_ID ?? "cf-client";
const CF_CLIENT_SECRET =
  process.env.MOCK_HERMES_CF_CLIENT_SECRET ?? "cf-secret";

const CAPABILITIES = {
  object: "hermes.api_server.capabilities",
  platform: "hermes-agent",
  model: "hermes-4",
  features: {
    session_resources: true,
    session_fork: true,
    session_chat: true,
    session_chat_streaming: true,
    run_submission: true,
    run_status: true,
    run_events_sse: true,
    run_stop: true,
    run_approval_response: true,
    tool_progress_events: true,
  },
  endpoints: {
    sessions: { method: "GET", path: "/api/sessions" },
    session_chat: { method: "POST", path: "/api/sessions/{session_id}/chat" },
    session_chat_stream: {
      method: "POST",
      path: "/api/sessions/{session_id}/chat/stream",
    },
    runs: { method: "POST", path: "/v1/runs" },
    run_status: { method: "GET", path: "/v1/runs/{run_id}" },
    run_events: { method: "GET", path: "/v1/runs/{run_id}/events" },
    run_stop: { method: "POST", path: "/v1/runs/{run_id}/stop" },
  },
};

const sessions = new Map();
const runs = new Map();
const jobs = new Map();
const mcpServers = new Map();

function sessionFixture(id, title = "Main") {
  return {
    id,
    source: "api_server",
    model: "hermes-4",
    title,
    started_at: Date.now() / 1000,
    message_count: 2,
    tool_call_count: 1,
  };
}

function runFixture(runId, sessionId, status = "completed") {
  return {
    object: "hermes.run",
    run_id: runId,
    session_id: sessionId,
    status,
    model: "hermes-4",
    output: "mock analysis summary",
    usage: { input_tokens: 2, output_tokens: 5, total_tokens: 7 },
    created_at: Date.now() / 1000,
    updated_at: Date.now() / 1000,
  };
}

function jobFixture(id) {
  return {
    object: "hermes.job",
    id,
    name: "progress",
    schedule: "0 * * * *",
    enabled: true,
    repeat_times: 3,
    repeat_completed: 1,
  };
}

function writeJSON(response, status, value) {
  response.writeHead(status, { "Content-Type": "application/json" });
  response.end(JSON.stringify(value));
}

function readBody(request) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    request.on("data", (chunk) => chunks.push(chunk));
    request.on("end", () => {
      try {
        resolve(chunks.length ? JSON.parse(Buffer.concat(chunks).toString()) : {});
      } catch (error) {
        reject(error);
      }
    });
    request.on("error", reject);
  });
}

function stripProfilePrefix(path) {
  const match = path.match(/^\/p\/[^/]+(\/.*)$/);
  return match ? match[1] : path;
}

function assertRuntimeAuth(request) {
  return request.headers.authorization === `Bearer ${RUNTIME_KEY}`;
}

function assertDashboardAuth(request) {
  if (request.headers["x-hermes-session-token"] !== DASHBOARD_TOKEN) {
    return false;
  }
  if (CLOUDFLARE_REQUIRE) {
    return (
      request.headers["cf-access-client-id"] === CF_CLIENT_ID &&
      request.headers["cf-access-client-secret"] === CF_CLIENT_SECRET
    );
  }
  return true;
}

function sseFrame(response, event, payload) {
  if (event) {
    response.write(`event: ${event}\n`);
  }
  response.write(`data: ${JSON.stringify(payload)}\n\n`);
}

function streamRunSequence(response, runId, sessionId) {
  response.writeHead(200, {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive",
  });
  response.write(": keepalive\n\n");
  const frames = [
    ["run.started", { run_id: runId, session_id: sessionId, seq: 1, ts: Date.now() / 1000 }],
    ["message.started", { run_id: runId, session_id: sessionId, message: { id: "message-1", role: "assistant" } }],
    ["assistant.delta", { run_id: runId, message_id: "message-1", delta: "hel" }],
    ["tool.started", { run_id: runId, message_id: "message-1", tool_name: "data.read", args: { path: "data" }, preview: "reading" }],
    ["tool.completed", { run_id: runId, tool_name: "data.read", preview: "rows=1" }],
    ["assistant.completed", { run_id: runId, message_id: "message-1", content: "hello from mock hermes" }],
    ["run.completed", { run_id: runId, messages: [{ tool_result: "ok" }], usage: { input_tokens: 2, output_tokens: 3, total_tokens: 5 } }],
    ["done", {}],
  ];
  for (const [event, payload] of frames) {
    sseFrame(response, event, payload);
  }
  response.end();
}

function streamRunEvents(response, runId) {
  response.writeHead(200, {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive",
  });
  const frames = [
    { event: "reasoning.available", run_id: runId, text: "hidden" },
    { event: "approval.request", run_id: runId, command: "data.read", choices: ["once", "session", "always", "deny"] },
    { event: "message.delta", run_id: runId, delta: "mock stream reply" },
    { event: "run.completed", run_id: runId },
  ];
  for (const frame of frames) {
    response.write(`data: ${JSON.stringify(frame)}\n\n`);
  }
  response.end();
}

const server = createServer(async (request, response) => {
  const rawPath = new URL(request.url, "http://localhost").pathname;
  const path = stripProfilePrefix(rawPath);

  // Dashboard management endpoints.
  if (path === "/api/health") {
    writeJSON(response, 200, { ok: true, version: "2026.8.3", auth_required: true });
    return;
  }
  if (
    path === "/api/mcp/servers" ||
    path === "/api/gateway/restart" ||
    (path.startsWith("/api/mcp/servers/") && path.endsWith("/test")) ||
    (path.startsWith("/api/mcp/servers/") && request.method === "DELETE")
  ) {
    if (!assertDashboardAuth(request)) {
      writeJSON(response, 401, { detail: "Unauthorized" });
      return;
    }
    if (path === "/api/mcp/servers" && request.method === "GET") {
      writeJSON(response, 200, { servers: [...mcpServers.values()] });
      return;
    }
    if (path === "/api/mcp/servers" && request.method === "POST") {
      const body = await readBody(request);
      const server = {
        name: body.name,
        transport: "http",
        url: body.url,
        auth: body.auth,
        enabled: true,
        bearer_token: body.bearer_token,
      };
      mcpServers.set(body.name, server);
      writeJSON(response, 200, server);
      return;
    }
    if (path.endsWith("/test")) {
      const name = path.slice("/api/mcp/servers/".length, -"/test".length);
      if (!mcpServers.has(name)) {
        writeJSON(response, 404, { detail: "not found" });
        return;
      }
      writeJSON(response, 200, {
        ok: true,
        tools: [
          { name: "project.get", description: "" },
          { name: "data.read", description: "" },
        ],
        prompts: 0,
        resources: 0,
      });
      return;
    }
    if (request.method === "DELETE") {
      const name = path.slice("/api/mcp/servers/".length);
      mcpServers.delete(name);
      writeJSON(response, 200, { ok: true });
      return;
    }
    if (path === "/api/gateway/restart") {
      writeJSON(response, 200, { ok: true, pid: 123, name: "gateway-restart" });
      return;
    }
  }

  // Runtime endpoints.
  if (!assertRuntimeAuth(request)) {
    writeJSON(response, 401, { detail: "Invalid API key" });
    return;
  }
  if (path === "/health") {
    writeJSON(response, 200, { status: "ok", platform: "hermes-agent", version: "2026.8.3" });
    return;
  }
  if (path === "/health/detailed") {
    writeJSON(response, 200, { status: "ready" });
    return;
  }
  if (path === "/v1/capabilities") {
    writeJSON(response, 200, CAPABILITIES);
    return;
  }
  if (path === "/api/sessions" && request.method === "GET") {
    writeJSON(response, 200, { data: [...sessions.values()], limit: 50, offset: 0, has_more: false });
    return;
  }
  if (path === "/api/sessions" && request.method === "POST") {
    const body = await readBody(request);
    const session = sessionFixture(body.id ?? `session-${Date.now()}`, body.title ?? "Main");
    sessions.set(session.id, session);
    writeJSON(response, 201, { object: "hermes.session", session });
    return;
  }
  const sessionMatch = path.match(/^\/api\/sessions\/([^/]+)(\/.*)?$/);
  if (sessionMatch) {
    const sessionId = sessionMatch[1];
    const rest = sessionMatch[2] ?? "";
    const session = sessions.get(sessionId);
    if (rest === "" && request.method === "GET") {
      writeJSON(response, session ? 200 : 404, session ?? { detail: "not found" });
      return;
    }
    if (rest === "" && request.method === "PATCH") {
      const body = await readBody(request);
      if (session) {
        if (body.title) session.title = body.title;
        if (body.end_reason) session.end_reason = body.end_reason;
      }
      writeJSON(response, 200, { session: session ?? sessionFixture(sessionId) });
      return;
    }
    if (rest === "" && request.method === "DELETE") {
      sessions.delete(sessionId);
      writeJSON(response, 200, { object: "hermes.session.deleted", id: sessionId, deleted: true });
      return;
    }
    if (rest === "/fork" && request.method === "POST") {
      const body = await readBody(request);
      const fork = sessionFixture(body.id ?? `${sessionId}-fork`, body.title ?? "Fork");
      sessions.set(fork.id, fork);
      writeJSON(response, 201, { session: fork });
      return;
    }
    if (rest === "/messages" && request.method === "GET") {
      writeJSON(response, 200, {
        data: [
          {
            id: "m0",
            session_id: sessionId,
            role: "user",
            content: "analyze the project",
            timestamp: Date.now() / 1000,
          },
          {
            id: "m1",
            session_id: sessionId,
            role: "assistant",
            content: "safe answer",
            tool_calls: [{ id: "call-1", function: { name: "data.read", arguments: "{}" } }],
            timestamp: Date.now() / 1000 + 1,
          },
        ],
      });
      return;
    }
    if (rest === "/chat" && request.method === "POST") {
      const body = await readBody(request);
      writeJSON(response, 200, {
        object: "hermes.session.chat.completion",
        session_id: sessionId,
        message: { role: "assistant", content: `reply to: ${body.message ?? ""}` },
        usage: { input_tokens: 3, output_tokens: 4, total_tokens: 7 },
        runtime: { provider: "nous", model: "hermes-4", route_source: "profile", model_lock: "confirmed", api_key: "never-copy" },
      });
      return;
    }
    if (rest === "/chat/stream" && request.method === "POST") {
      await readBody(request);
      const runId = `run-${Date.now()}`;
      runs.set(runId, runFixture(runId, sessionId, "running"));
      streamRunSequence(response, runId, sessionId);
      runs.set(runId, runFixture(runId, sessionId, "completed"));
      return;
    }
  }
  if (path === "/v1/runs" && request.method === "POST") {
    const body = await readBody(request);
    const runId = `run-${Date.now()}`;
    runs.set(runId, runFixture(runId, body.session_id ?? "session-main", "started"));
    writeJSON(response, 202, { run_id: runId, status: "started" });
    return;
  }
  const runMatch = path.match(/^\/v1\/runs\/([^/]+)(\/.*)?$/);
  if (runMatch) {
    const runId = runMatch[1];
    const rest = runMatch[2] ?? "";
    const run = runs.get(runId) ?? runFixture(runId, "session-main");
    if (rest === "" && request.method === "GET") {
      writeJSON(response, 200, run);
      return;
    }
    if (rest === "/stop" && request.method === "POST") {
      writeJSON(response, 200, { run_id: runId, status: "stopping" });
      return;
    }
    if (rest === "/approval" && request.method === "POST") {
      writeJSON(response, 200, { object: "hermes.run.approval_response", run_id: runId, choice: "session", resolved: 2 });
      return;
    }
    if (rest === "/events" && request.method === "GET") {
      streamRunEvents(response, runId);
      return;
    }
  }
  if (path === "/api/jobs" && request.method === "GET") {
    writeJSON(response, 200, { jobs: [...jobs.values()] });
    return;
  }
  if (path === "/api/jobs" && request.method === "POST") {
    const body = await readBody(request);
    const job = jobFixture(body.id ?? `job-${Date.now()}`);
    jobs.set(job.id, job);
    writeJSON(response, 200, { job });
    return;
  }
  const jobMatch = path.match(/^\/api\/jobs\/([^/]+)(\/.*)?$/);
  if (jobMatch) {
    const jobId = jobMatch[1];
    const rest = jobMatch[2] ?? "";
    const job = jobs.get(jobId) ?? jobFixture(jobId);
    if (rest === "" && request.method === "GET") {
      writeJSON(response, 200, { job });
      return;
    }
    if (rest === "" && request.method === "PATCH") {
      const body = await readBody(request);
      if (body.enabled !== undefined) job.enabled = body.enabled;
      if (body.name) job.name = body.name;
      writeJSON(response, 200, { job });
      return;
    }
    if (rest === "" && request.method === "DELETE") {
      jobs.delete(jobId);
      writeJSON(response, 200, { ok: true });
      return;
    }
    if (/^\/(pause|resume|run)$/.test(rest) && request.method === "POST") {
      writeJSON(response, 200, { job });
      return;
    }
  }
  writeJSON(response, 404, { detail: "not found" });
});

server.listen(PORT, "0.0.0.0", () => {
  process.stdout.write(`mock-hermes listening on ${PORT}\n`);
});
