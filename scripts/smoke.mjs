import { spawnSync } from "node:child_process";
import { resolve } from "node:path";

const webUrl = trim(process.env.MMDASH_SMOKE_URL ?? "http://localhost:3000");
const coreUrl = trim(
  process.env.MMDASH_SMOKE_CORE_URL ?? "http://localhost:8080",
);
const mcpUrl = trim(
  process.env.MMDASH_SMOKE_MCP_URL ?? "http://localhost:3002",
);
const email =
  process.env.MMDASH_SMOKE_EMAIL ?? "admin@mmdash.local";
const password =
  process.env.MMDASH_SMOKE_PASSWORD ?? "mmdash-local-admin";
const runId = `${Date.now()}-${process.pid}`;

const web = await fetchChecked(`${webUrl}/projects`);
const html = await web.text();
assert(
  html.includes("团队项目") || html.includes("创建团队项目"),
  "Web smoke check did not find the current project-list shell.",
);

const example = await jsonChecked(`${webUrl}/api/example`);
assert(
  example.body.status === "ok" && example.body.storage === "postgres",
  `Unexpected BFF example response: ${JSON.stringify(example.body)}`,
);
assert(example.response.headers.get("x-request-id"), "BFF omitted x-request-id.");

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
      ? spawnSync(
          "docker",
          [
            "compose",
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
        )
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
              UV_CACHE_DIR:
                process.env.UV_CACHE_DIR ?? resolve(".uv-cache"),
            },
          },
        );
  assert(
    worker.status === 0,
    `Worker smoke failed:\n${worker.stdout}\n${worker.stderr}`,
  );
}
const completedJob = await poll(
  async () => (await jsonChecked(`${coreUrl}/v1/jobs/${jobId}`, {
    headers: sessionHeaders,
  })).body,
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
  async () => (await jsonChecked(`${coreUrl}/v1/events/${eventId}`, {
    headers: sessionHeaders,
  })).body,
  (state) =>
    state.record?.status === "published" &&
    state.deliveries?.every((delivery) => delivery.status === "succeeded"),
  "Outbox test event was not published and consumed.",
);

const objects = await poll(
  async () => (await jsonChecked(
    `${coreUrl}/v1/data/projects/${projectId}/objects?type=project`,
    { headers: sessionHeaders },
  )).body,
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
  async () => (await jsonChecked(
    `${coreUrl}/v1/audit/events?request_id=${encodeURIComponent(projectRequestId)}`,
    { headers: sessionHeaders },
  )).body,
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
  mcpHealth.body.status === "ok" &&
    mcpHealth.body.service === "mcp-gateway",
  "MCP Gateway health check failed.",
);

const cli = spawnSync(
  process.execPath,
  ["clients/cli/dist/main.js", "--version"],
  { encoding: "utf8" },
);
assert(
  cli.status === 0 && cli.stdout.trim().length > 0,
  `CLI shell failed:\n${cli.stdout}\n${cli.stderr}`,
);

console.log(
  JSON.stringify({
    audit_events: audits.items.length,
    event_id: eventId,
    job_id: jobId,
    project_id: projectId,
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
    throw new Error(`${options.method ?? "GET"} ${url}: HTTP ${response.status} ${text}`);
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

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function trim(value) {
  return value.replace(/\/$/, "");
}
