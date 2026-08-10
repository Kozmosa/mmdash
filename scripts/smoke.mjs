import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { resolveComposeCommand } from "./compose-command.mjs";
import { runRepoSmoke } from "./repo-smoke.mjs";

const webUrl = trim(process.env.MMDASH_SMOKE_URL ?? "http://localhost:3000");
const coreUrl = trim(
  process.env.MMDASH_SMOKE_CORE_URL ?? "http://localhost:8080",
);
const mcpUrl = trim(
  process.env.MMDASH_SMOKE_MCP_URL ?? "http://localhost:3002",
);
const email = process.env.MMDASH_SMOKE_EMAIL ?? "admin@mmdash.local";
const password = process.env.MMDASH_SMOKE_PASSWORD ?? "mmdash-local-admin";
const runId = `${Date.now()}-${process.pid}`;

const web = await fetchChecked(`${webUrl}/projects`);
const html = await web.text();
assert(
  new URL(web.url).pathname === "/login" ||
    html.includes("团队项目") ||
    html.includes("创建团队项目") ||
    html.includes("登录 mmdash"),
  "Web smoke check did not find the authenticated project shell or login guard.",
);

const example = await jsonChecked(`${webUrl}/api/example`);
assert(
  example.body.status === "ok" && example.body.storage === "postgres",
  `Unexpected BFF example response: ${JSON.stringify(example.body)}`,
);
assert(
  example.response.headers.get("x-request-id"),
  "BFF omitted x-request-id.",
);

const browserLogin = await jsonChecked(`${webUrl}/api/auth/login`, {
  body: { email, password },
  method: "POST",
});
const cookieHeader =
  browserLogin.response.headers.getSetCookie?.()[0] ??
  browserLogin.response.headers.get("set-cookie");
assert(cookieHeader, "BFF login did not set the browser session cookie.");
const cookie = cookieHeader.split(";", 1)[0];

const projectRequestId = `smoke-project-${runId}`;
const projectResult = await jsonChecked(`${webUrl}/api/projects`, {
  body: {
    name: `Foundation smoke ${runId}`,
    problem_summary: "Stage 3.15 end-to-end verification",
    problem_title: "Technical foundation",
  },
  headers: { cookie, "x-request-id": projectRequestId },
  method: "POST",
});
assert(
  projectResult.response.headers.get("x-request-id") === projectRequestId,
  "BFF did not preserve the supplied project request ID.",
);
const projectId = projectResult.body.id;
assert(projectId, "BFF project creation did not return a project ID.");

const home = await jsonChecked(
  `${webUrl}/api/projects/${encodeURIComponent(projectId)}/pages/home`,
  { headers: { cookie } },
);
assert(
  home.body.fragments?.home?.project_id === projectId,
  "BFF home aggregation did not reach Core Data Hub.",
);

const coreLogin = await jsonChecked(`${coreUrl}/v1/auth/login`, {
  body: { email, password },
  headers: { "x-request-id": `smoke-core-login-${runId}` },
  method: "POST",
});
const accessToken = coreLogin.body.access_token;
assert(accessToken, "Core login did not return an access token.");
const sessionHeaders = {
  authorization: `Bearer ${accessToken}`,
};

const issued = await jsonChecked(`${coreUrl}/v1/auth/tokens`, {
  body: {
    kind: "api",
    name: `stage-3.15-worker-${runId}`,
    project_id: projectId,
  },
  headers: sessionHeaders,
  method: "POST",
});
const workerToken = issued.body.token;
assert(workerToken, "Core did not issue the Worker API token.");

const jobResult = await jsonChecked(`${coreUrl}/v1/jobs`, {
  body: {
    idempotency_key: `stage-3.15-${runId}`,
    job_type: "system.test",
    payload: { message: "worker-smoke" },
    project_id: projectId,
    timeout_seconds: 120,
  },
  headers: sessionHeaders,
  method: "POST",
});
const jobId = jobResult.body.id;
assert(jobId, "Core did not create the test Job.");

if (process.env.MMDASH_SMOKE_SKIP_WORKER !== "1") {
  const worker =
    process.env.MMDASH_SMOKE_WORKER_MODE === "docker"
      ? (() => {
          const compose = resolveComposeCommand();
          return spawnSync(
            compose.command,
            [
              ...compose.args,
              "-f",
              "deploy/compose/compose.yaml",
              "run",
              "--rm",
              "-e",
              `MMDASH_WORKER_API_TOKEN=${workerToken}`,
              "-e",
              `MMDASH_WORKER_ID=stage-3.15-${runId}`,
              "worker",
              "--once",
            ],
            { encoding: "utf8", env: process.env },
          );
        })()
      : spawnSync(
          process.platform === "win32" ? "uv.exe" : "uv",
          [
            "run",
            "--offline",
            "--package",
            "mmdash-worker",
            "mmdash-worker",
            "--once",
          ],
          {
            encoding: "utf8",
            env: {
              ...process.env,
              MMDASH_CORE_URL: coreUrl,
              MMDASH_WORKER_API_TOKEN: workerToken,
              MMDASH_WORKER_ID: `stage-3.15-${runId}`,
              MMDASH_WORKER_LEASE_SECONDS: "60",
              MMDASH_WORKER_POLL_SECONDS: "1",
              UV_CACHE_DIR: process.env.UV_CACHE_DIR ?? resolve(".uv-cache"),
            },
          },
        );
  assert(
    worker.status === 0,
    `Worker smoke failed:\n${worker.stdout}\n${worker.stderr}`,
  );
}
const completedJob = await poll(
  async () =>
    (
      await jsonChecked(`${coreUrl}/v1/jobs/${jobId}`, {
        headers: sessionHeaders,
      })
    ).body,
  (job) => job.status === "succeeded",
  "Worker did not complete the system.test Job.",
);
assert(
  completedJob.result?.handler === "system.test",
  "Worker result did not identify the baseline handler.",
);

const eventResult = await jsonChecked(`${coreUrl}/v1/events/test`, {
  body: { message: "stage-3.15", payload: { run_id: runId } },
  headers: sessionHeaders,
  method: "POST",
});
const eventId = eventResult.body.event_id;
assert(eventId, "Core did not enqueue the Outbox test event.");
await poll(
  async () =>
    (
      await jsonChecked(`${coreUrl}/v1/events/${eventId}`, {
        headers: sessionHeaders,
      })
    ).body,
  (state) =>
    state.record?.status === "published" &&
    state.deliveries?.length > 0 &&
    state.deliveries?.every((delivery) => delivery.status === "succeeded"),
  "Outbox test event was not published and consumed.",
);

const objects = await poll(
  async () =>
    (
      await jsonChecked(
        `${coreUrl}/v1/data/projects/${projectId}/objects?type=project`,
        { headers: sessionHeaders },
      )
    ).body,
  (page) => page.items?.some((item) => item.source_id === projectId),
  "Data Hub did not project the created project object.",
);
const object = objects.items.find((item) => item.source_id === projectId);
const objectRead = await jsonChecked(
  `${coreUrl}/v1/data/projects/${projectId}/objects/${object.object_id}`,
  { headers: sessionHeaders },
);
assert(
  objectRead.body.content?.id === projectId,
  "data.read did not route to authoritative Project content.",
);

const audits = await poll(
  async () =>
    (
      await jsonChecked(
        `${coreUrl}/v1/audit/events?request_id=${encodeURIComponent(projectRequestId)}`,
        { headers: sessionHeaders },
      )
    ).body,
  (page) =>
    page.items?.some(
      (item) =>
        item.request_id === projectRequestId &&
        item.project_id === projectId &&
        item.action === "http.request.completed",
    ),
  "The cross-service request ID was not queryable in Audit.",
);
assert(audits.items.length > 0, "Audit query returned no items.");

const metrics = await (await fetchChecked(`${coreUrl}/metrics`)).text();
assert(
  metrics.includes("mmdash_http_requests_total") &&
    metrics.includes("mmdash_build_info"),
  "Core metrics endpoint is missing baseline series.",
);

const mcpHealth = await jsonChecked(`${mcpUrl}/health/live`);
assert(
  mcpHealth.body.status === "ok" && mcpHealth.body.service === "mcp-gateway",
  "MCP Gateway health check failed.",
);

if (process.env.MMDASH_SMOKE_SKIP_CLI === "1") {
  console.log("skipping native CLI MCP smoke (MMDASH_SMOKE_SKIP_CLI=1)");
} else {
  await runCliMcpSmoke({
    cookie,
    coreUrl,
    dataObjectId: object.object_id,
    mcpUrl,
    projectId,
    runId,
    webUrl,
  });
}

const repo =
  process.env.MMDASH_SMOKE_REPO_MODE === "docker"
    ? await runRepoSmoke({
        coreUrl,
        email,
        password,
        runId,
        webUrl,
      })
    : null;

console.log(
  JSON.stringify({
    audit_events: audits.items.length,
    event_id: eventId,
    job_id: jobId,
    project_id: projectId,
    repo,
    status: "passed",
  }),
);

async function jsonChecked(url, options = {}) {
  const response = await fetchChecked(url, options);
  const body = await response.json();
  return { body, response };
}

async function fetchChecked(url, options = {}) {
  const headers = new Headers(options.headers);
  let body = options.body;
  if (body !== undefined) {
    headers.set("content-type", "application/json");
    body = JSON.stringify(body);
  }
  const response = await fetch(url, {
    ...options,
    body,
    headers,
    signal: AbortSignal.timeout(20_000),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(
      `${options.method ?? "GET"} ${url}: HTTP ${response.status} ${text}`,
    );
  }
  return response;
}

async function poll(load, ready, message) {
  let last;
  for (let attempt = 0; attempt < 30; attempt += 1) {
    last = await load();
    if (ready(last)) return last;
    await new Promise((resolve) => setTimeout(resolve, 1_000));
  }
  throw new Error(`${message} Last state: ${JSON.stringify(last)}`);
}

async function runCliMcpSmoke({
  cookie,
  coreUrl,
  dataObjectId,
  mcpUrl,
  projectId,
  runId,
  webUrl,
}) {
  const configDirectory = mkdtempSync(join(tmpdir(), "mmdash-cli-smoke-"));
  const environment = {
    ...process.env,
    MMDASH_CONFIG_DIR: configDirectory,
    MMDASH_CORE_URL: coreUrl,
    MMDASH_MCP_URL: `${mcpUrl}/mcp`,
    // The profile URL is deliberately unique so this smoke never replaces a
    // developer's normal localhost credential in the platform secret store.
    MMDASH_URL: `${coreUrl}/cli-smoke-${runId}`,
  };
  const cliBinary = join(
    configDirectory,
    process.platform === "win32" ? "mmdash.exe" : "mmdash",
  );
  const build = spawnSync(
    "go",
    ["build", "-trimpath", "-o", cliBinary, "./clients/cli/cmd/mmdash"],
    { encoding: "utf8", env: environment, timeout: 120_000 },
  );
  assert(
    build.status === 0,
    `Native CLI build failed:\n${build.stdout}\n${build.stderr}`,
  );
  let authenticated = false;
  let loginProcess;
  try {
    const version = runCli(cliBinary, ["--version"], environment);
    assert(version.stdout.trim().length > 0, "Native CLI returned no version.");

    let resolveUserCode;
    let rejectUserCode;
    let timeout;
    const userCode = new Promise((resolveCode, rejectCode) => {
      resolveUserCode = (code) => {
        clearTimeout(timeout);
        resolveCode(code);
      };
      rejectUserCode = rejectCode;
    });
    const login = startCli(
      cliBinary,
      ["login", "--no-browser"],
      environment,
      (text) => {
        const match = text.match(/enter code ([A-Za-z0-9]{4}-[A-Za-z0-9]{4})/);
        if (match) resolveUserCode(match[1]);
      },
    );
    loginProcess = login.child;
    timeout = setTimeout(
      () =>
        rejectUserCode(new Error("CLI login did not present a device code.")),
      20_000,
    );
    let code;
    try {
      code = await Promise.race([
        userCode,
        login.finished.then((result) => {
          throw new Error(
            `CLI login exited before presenting a device code:\n${result.stdout}\n${result.stderr}`,
          );
        }),
      ]);
    } finally {
      clearTimeout(timeout);
    }
    await fetchChecked(`${webUrl}/api/auth/device/verify`, {
      body: { approve: true, user_code: code },
      headers: { cookie },
      method: "POST",
    });
    const loginResult = await withTimeout(
      login.finished,
      30_000,
      "CLI login did not finish after browser approval.",
    );
    loginProcess = undefined;
    authenticated = loginResult.code === 0;
    assert(
      authenticated,
      `Native CLI login failed:\n${loginResult.stdout}\n${loginResult.stderr}`,
    );

    runCli(cliBinary, ["--json", "project", "use", projectId], environment);
    const requests = [
      {
        id: 1,
        jsonrpc: "2.0",
        method: "initialize",
        params: {
          capabilities: {},
          clientInfo: { name: "mmdash-cli-smoke", version: "0.1.0" },
          protocolVersion: "2026-07-28",
        },
      },
      { jsonrpc: "2.0", method: "notifications/initialized" },
      { id: 2, jsonrpc: "2.0", method: "tools/list" },
      {
        id: 3,
        jsonrpc: "2.0",
        method: "tools/call",
        params: { arguments: {}, name: "project.list" },
      },
      {
        id: 4,
        jsonrpc: "2.0",
        method: "tools/call",
        params: { arguments: {}, name: "project.get" },
      },
      {
        id: 5,
        jsonrpc: "2.0",
        method: "tools/call",
        params: { arguments: { type: "project" }, name: "data.list" },
      },
      {
        id: 6,
        jsonrpc: "2.0",
        method: "tools/call",
        params: {
          arguments: { object_id: dataObjectId },
          name: "data.read",
        },
      },
    ];
    const mcp = runCli(cliBinary, ["mcp"], environment, {
      input: `${requests.map((request) => JSON.stringify(request)).join("\n")}\n`,
      timeout: 120_000,
    });
    const responses = mcp.stdout
      .trim()
      .split("\n")
      .filter(Boolean)
      .map((line) => JSON.parse(line));
    const byId = new Map(responses.map((response) => [response.id, response]));
    for (const id of [1, 2, 3, 4, 5, 6]) {
      assert(byId.has(id), `CLI MCP omitted JSON-RPC response ${id}.`);
      assert(
        !byId.get(id).error,
        `CLI MCP request ${id} failed: ${JSON.stringify(byId.get(id).error)}`,
      );
    }
    const tools = byId.get(2).result.tools.map((tool) => tool.name);
    for (const tool of [
      "project.list",
      "project.get",
      "data.list",
      "data.read",
    ]) {
      assert(tools.includes(tool), `CLI MCP discovery omitted ${tool}.`);
    }
    assert(
      byId.get(4).result.structuredContent?.id === projectId,
      "CLI project.get did not use the explicitly selected Project.",
    );
    assert(
      byId.get(6).result.structuredContent?.object?.object_id === dataObjectId,
      "CLI data.read did not return the selected Data Hub object.",
    );
  } finally {
    await stopChild(loginProcess);
    if (authenticated) {
      const logout = spawnSync(cliBinary, ["logout"], {
        encoding: "utf8",
        env: environment,
        timeout: 60_000,
      });
      if (logout.status !== 0) {
        process.stderr.write(
          `CLI smoke cleanup could not remove its credential:\n${logout.stdout}\n${logout.stderr}\n`,
        );
      }
    }
    rmSync(configDirectory, { force: true, recursive: true });
  }
}

function runCli(cliBinary, arguments_, environment, options = {}) {
  const result = spawnSync(cliBinary, arguments_, {
    encoding: "utf8",
    env: environment,
    timeout: 60_000,
    ...options,
  });
  assert(
    result.status === 0,
    `Native CLI failed (${arguments_.join(" ")}):\n${result.stdout}\n${result.stderr}`,
  );
  return result;
}

function startCli(cliBinary, arguments_, environment, onStderr) {
  const child = spawn(cliBinary, arguments_, {
    env: environment,
    stdio: ["ignore", "pipe", "pipe"],
  });
  let stdout = "";
  let stderr = "";
  child.stdout.setEncoding("utf8");
  child.stderr.setEncoding("utf8");
  child.stdout.on("data", (text) => {
    stdout += text;
  });
  child.stderr.on("data", (text) => {
    stderr += text;
    onStderr(stderr);
  });
  const finished = new Promise((resolveFinished, reject) => {
    child.once("error", reject);
    child.once("close", (code) => resolveFinished({ code, stderr, stdout }));
  });
  return { child, finished };
}

async function stopChild(child) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  child.kill();
  await new Promise((resolveStopped) => {
    const timeout = setTimeout(resolveStopped, 5_000);
    child.once("close", () => {
      clearTimeout(timeout);
      resolveStopped();
    });
  });
}

async function withTimeout(promise, timeoutMs, message) {
  let timeout;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => {
        timeout = setTimeout(() => reject(new Error(message)), timeoutMs);
      }),
    ]);
  } finally {
    clearTimeout(timeout);
  }
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function trim(value) {
  return value.replace(/\/$/, "");
}
