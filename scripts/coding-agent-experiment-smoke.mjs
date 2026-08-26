// Simulate a Coding Agent calling the Experiment MCP tools and completing one
// Local Docker run.  Start the local development stack first:
//
//   node .localscripts\dev.mjs
//
// Then run this script from a second terminal.  The script creates a temporary
// local Git remote and a project, prefers an already-online account-owned Box,
// and removes the temporary Box assignment and working files when it exits.

import {
  existsSync,
  mkdtempSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { spawn, spawnSync } from "node:child_process";
import { join, resolve } from "node:path";

const root = resolve(process.cwd());
const runId = `${Date.now()}-${process.pid}`;
const bffUrl = trim(
  process.env.MMDASH_ACCEPTANCE_BFF_URL ?? "http://127.0.0.1:3001",
);
const coreUrl = trim(
  process.env.MMDASH_ACCEPTANCE_CORE_URL ?? "http://127.0.0.1:8080",
);
const mcpUrl = trim(
  process.env.MMDASH_ACCEPTANCE_MCP_URL ?? "http://127.0.0.1:3002/mcp",
);
const email = process.env.AUTH_BOOTSTRAP_EMAIL ?? "admin@mmdash.local";
const password = process.env.AUTH_BOOTSTRAP_PASSWORD ?? "mmdash-local-admin";
const suppliedAccessToken = trim(
  process.env.MMDASH_ACCEPTANCE_ACCESS_TOKEN ?? "",
);
const reuseProjectId = trim(
  process.env.MMDASH_ACCEPTANCE_REUSE_PROJECT_ID ?? "",
);
const suppliedBoxId = trim(process.env.MMDASH_ACCEPTANCE_BOX_ID ?? "");
const dependencyInput =
  process.env.MMDASH_ACCEPTANCE_PYTHON_DEPENDENCIES ??
  "numpy>=2,<3\nmatplotlib>=3,<4\n";
const localImage =
  process.env.MMDASH_ACCEPTANCE_LOCAL_IMAGE ?? "python:3.12-slim";
const boxBinary =
  process.env.MMDASH_ACCEPTANCE_BOX_BINARY ??
  join(
    root,
    ".tmp",
    "dev-tools",
    process.platform === "win32" ? "mmdash-box.exe" : "mmdash-box",
  );
const protocolVersion =
  process.env.MMDASH_ACCEPTANCE_MCP_PROTOCOL ?? "2026-07-28";
const timeoutSeconds = Number(
  process.env.MMDASH_ACCEPTANCE_TIMEOUT_SECONDS ?? "180",
);
const requireExistingBox =
  process.env.MMDASH_ACCEPTANCE_REQUIRE_EXISTING_BOX === "true";
const forceTemporaryBox =
  process.env.MMDASH_ACCEPTANCE_FORCE_TEMPORARY_BOX === "true";

let cookie;
let coreToken;
let projectId;
let boxId;
let boxProcess;
let temporaryBox = false;
let temporaryRoot;
let mcpSession;
let negotiatedProtocol;
let mcpRequestId = 1;

try {
  assert(
    Number.isInteger(timeoutSeconds) && timeoutSeconds >= 30,
    "MMDASH_ACCEPTANCE_TIMEOUT_SECONDS must be at least 30.",
  );
  assert(
    existsSync(boxBinary),
    `Box binary not found at ${boxBinary}. Start .localscripts\\dev.mjs first or set MMDASH_ACCEPTANCE_BOX_BINARY.`,
  );
  await checkHealth(`${coreUrl}/health/ready`, "Core");
  await checkHealth(`${bffUrl}/health/live`, "Web BFF");
  await checkHealth(
    `${mcpUrl.replace(/\/mcp$/u, "")}/health/live`,
    "MCP Gateway",
  );

  if (suppliedAccessToken) {
    coreToken = suppliedAccessToken;
  } else {
    const login = await jsonRequest(`${bffUrl}/api/auth/login`, {
      body: { email, password },
      method: "POST",
    });
    const cookieHeader =
      login.response.headers.getSetCookie?.()[0] ??
      login.response.headers.get("set-cookie");
    assert(cookieHeader, "Bootstrap login did not set a BFF session cookie.");
    cookie = cookieHeader.split(";", 1)[0];

    const coreLogin = await jsonRequest(`${coreUrl}/v1/auth/login`, {
      body: { email, password },
      method: "POST",
    });
    coreToken = coreLogin.body.access_token;
    assert(coreToken, "Core login did not return an access token.");
  }

  let fixture;
  if (reuseProjectId) {
    projectId = reuseProjectId;
    const repository = await coreJson(
      `/projects/${encodeURIComponent(projectId)}/repository`,
    );
    const codeWorkspace = repository.body.workspaces?.find(
      (workspace) =>
        workspace.workspace === "code" && workspace.status === "ready",
    );
    assert(
      codeWorkspace?.head_commit_sha,
      "Reused Project has no ready code workspace Commit.",
    );
    fixture = { sourceCommit: codeWorkspace.head_commit_sha };
  } else {
    const project = await coreJson("/projects", {
      body: {
        name: `Coding Agent experiment ${runId}`,
        problem_summary: "Local Docker environment preparation acceptance",
        problem_title: "Environment preparation",
      },
      method: "POST",
    });
    projectId = project.body.id;
    assert(projectId, "Project creation did not return an ID.");

    const scratchRoot = join(root, ".tmp");
    mkdirSync(scratchRoot, { recursive: true });
    temporaryRoot = mkdtempSync(join(scratchRoot, "mmdash-coding-agent-"));
    fixture = createFixtureRepository(temporaryRoot, dependencyInput);

    const setting = await bffJson(
      `/api/projects/${encodeURIComponent(projectId)}/settings/repo.connection`,
      {
        body: {
          values: {
            article_branch: "article",
            code_branch: "main",
            provider: "local",
            remote_url: fixture.remote,
            result_branch: "result",
          },
        },
        method: "PATCH",
      },
    );
    const tested = await bffJson(
      `/api/projects/${encodeURIComponent(projectId)}/repository/test`,
      { method: "POST" },
    );
    assert(
      tested.body.status === "passed",
      `Repository connection test failed: ${JSON.stringify(tested.body)}`,
    );
    const connected = await bffJson(
      `/api/projects/${encodeURIComponent(projectId)}/repository`,
      {
        body: { settings_version: setting.body.version },
        method: "PUT",
      },
    );
    assert(
      connected.body.repository_id,
      "Repository connection did not return a repository ID.",
    );
    const repository = await poll(
      async () =>
        (
          await bffJson(
            `/api/projects/${encodeURIComponent(projectId)}/repository`,
          )
        ).body,
      (item) =>
        item.status === "ready" &&
        item.workspaces?.some(
          (workspace) =>
            workspace.workspace === "code" &&
            workspace.status === "ready" &&
            workspace.head_commit_sha === fixture.sourceCommit,
        ),
      "Repository did not synchronize the fixture Commit",
    );
    assert(repository.status === "ready", "Repository did not become ready.");
  }

  const existingBox = suppliedBoxId
    ? { box_id: suppliedBoxId }
    : forceTemporaryBox
      ? undefined
      : await findOnlineLocalDockerBox();
  if (existingBox) {
    boxId = existingBox.box_id;
  } else {
    assert(
      !requireExistingBox,
      "The account has no online Box advertising the local-docker runtime.",
    );
    boxProcess = await startBox({ cookie, projectId });
    boxId = boxProcess.boxId;
    temporaryBox = true;
  }
  await assignBox(boxId, projectId);

  mcpSession = await mcpInitialize();
  const listed = await mcpRequest("tools/list", {});
  const tools = listed.tools?.map((tool) => tool.name) ?? [];
  for (const tool of [
    "experiment.create",
    "experiment.run",
    "experiment.status",
    "result.get",
  ]) {
    assert(tools.includes(tool), `MCP tools/list omitted ${tool}.`);
  }

  const created = await mcpTool("experiment.create", {
    project_id: projectId,
    name: `Coding Agent Local Docker ${runId}`,
    experiment_type: "box",
    source_commit: fixture.sourceCommit,
    entrypoint: "python:run.py",
    parameters: {},
    environment: {},
    inputs: {},
    runtime_policy: "local-docker",
    limits_override: {
      cpu_millis: 1_000,
      memory_bytes: 1_073_741_824,
      timeout_seconds: timeoutSeconds,
      disk_bytes: 2_147_483_648,
      pids: 128,
      network: "disabled",
    },
    idempotency_key: `coding-agent-create-${runId}`,
  });
  const experimentId = created.experiment_id;
  assert(experimentId, "experiment.create did not return an experiment ID.");
  assert(
    (created.execution_status ?? created.status) === "created",
    `Experiment was not created: ${JSON.stringify(created)}`,
  );

  const queued = await mcpTool("experiment.run", {
    project_id: projectId,
    experiment_id: experimentId,
    idempotency_key: `coding-agent-run-${runId}`,
  });
  assert(
    ["queued", "preparing", "running"].includes(
      queued.execution_status ?? queued.status,
    ),
    `experiment.run did not queue the experiment: ${JSON.stringify(queued)}`,
  );

  const terminal = await poll(
    async () =>
      mcpTool("experiment.status", {
        project_id: projectId,
        experiment_id: experimentId,
        log_tail: 500,
      }),
    (item) =>
      ["succeeded", "failed", "canceled", "timed_out", "archived"].includes(
        item.execution_status ?? item.status,
      ),
    "Experiment did not reach a terminal state",
    timeoutSeconds + 60,
  );
  const terminalStatus = terminal.execution_status ?? terminal.status;
  assert(
    terminalStatus === "succeeded" || terminalStatus === "archived",
    `Experiment failed: ${JSON.stringify({
      status: terminalStatus,
      failure: terminal.failure,
      failure_code: terminal.failure_code,
    })}`,
  );
  assert(
    terminal.actual_runtime === undefined ||
      terminal.actual_runtime === "local-docker",
    `Unexpected actual runtime: ${JSON.stringify(terminal.actual_runtime)}`,
  );

  const logs = terminal.logs ?? [];
  const logText = logs.map((entry) => entry.message ?? "").join("\n");
  assert(
    logText.includes("MMDASH_CODING_AGENT_STDOUT"),
    `Console output was not returned through Experiment logs: ${logText}`,
  );
  assert(
    logs.some(
      (entry) =>
        entry.stream === "system" &&
        /environment|dependency|pip|镜像|环境|依赖/iu.test(entry.message ?? ""),
    ),
    "Environment preparation did not emit a persisted system log.",
  );

  const result = await mcpTool("result.get", {
    project_id: projectId,
    experiment_id: experimentId,
  });
  const resultStatus = result.execution_status ?? result.status;
  assert(
    resultStatus === "succeeded" || resultStatus === "archived",
    `result.get did not report a successful result: ${JSON.stringify(result)}`,
  );
  const resultFiles = result.files ?? result.manifest?.files ?? [];
  assert(
    resultFiles.some((file) => file.path === "summary.md"),
    `Result did not include summary.md: ${JSON.stringify(result)}`,
  );
  assert(
    resultFiles.some((file) => file.path === "figures/dependency-plot.png"),
    `Result did not include the NumPy/Matplotlib figure: ${JSON.stringify(result)}`,
  );
  assert(
    result.execution_bundle || result.result_commit_sha || result.result_commit,
    `Result did not expose an immutable bundle or Commit: ${JSON.stringify(result)}`,
  );

  console.log(
    JSON.stringify({
      dependencies: fixture.dependencies,
      experiment_id: experimentId,
      project_id: projectId,
      source_commit: fixture.sourceCommit,
      status: "passed",
    }),
  );
} catch (error) {
  console.error(`coding-agent experiment smoke failed: ${error.message}`);
  process.exitCode = 1;
} finally {
  await cleanup();
}

async function startBox({ cookie: sessionCookie, projectId: targetProjectId }) {
  const boxRoot = join(temporaryRoot, "box");
  const statePath = join(boxRoot, "state.json");
  const child = spawn(boxBinary, ["--gateway", "--root", boxRoot], {
    cwd: root,
    env: {
      ...process.env,
      MMDASH_BOX_CONTROL_URL: coreUrl,
      MMDASH_BOX_CORE_URL: coreUrl,
      MMDASH_BOX_LOCAL_IMAGE: localImage,
      MMDASH_BOX_NAME: `coding-agent-box-${runId}`,
      MMDASH_BOX_VERSION: "coding-agent-acceptance",
      MMDASH_BOX_HEARTBEAT_INTERVAL: "1s",
      MMDASH_BOX_CLAIM_WAIT: "2s",
      MMDASH_BOX_RETRY_DELAY: "500ms",
      MMDASH_BOX_CPU_MILLIS: "1000",
      MMDASH_BOX_MEMORY_BYTES: "1073741824",
      MMDASH_BOX_TIMEOUT_SECONDS: String(timeoutSeconds),
      MMDASH_BOX_DISK_BYTES: "2147483648",
      MMDASH_BOX_PIDS: "128",
      MMDASH_BOX_NETWORK: "disabled",
    },
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });
  let output = "";
  let approved = false;
  child.stdout.setEncoding("utf8");
  child.stderr.setEncoding("utf8");
  const collect = (chunk) => {
    output = `${output}${chunk}`.slice(-200_000);
    if (!approved) {
      const code = output.match(
        /(?:验证码|user code)[：:]?\s*([A-Za-z0-9]{4}(?:-[A-Za-z0-9]{4}|[A-Za-z0-9]{4}))/iu,
      )?.[1];
      if (code) {
        approved = true;
        void bffJson("/api/auth/device/verify", {
          body: { approve: true, user_code: code },
          headers: { cookie: sessionCookie },
          method: "POST",
        }).catch((error) => {
          child.kill();
          console.error(`Box device approval failed: ${error.message}`);
        });
      }
    }
  };
  child.stdout.on("data", collect);
  child.stderr.on("data", collect);
  const state = await poll(
    async () => {
      if (child.exitCode !== null || child.signalCode !== null) {
        throw new Error(`Box exited before registration:\n${output}`);
      }
      const current = readState(statePath);
      if (current?.box_id) return current;
      return undefined;
    },
    Boolean,
    "Box did not complete device registration",
    180,
  );
  assert(state.box_id, "Box state did not contain box_id.");
  await poll(
    async () => {
      const detail = await tryCoreJson(
        `/users/me/boxes/${encodeURIComponent(state.box_id)}`,
      );
      if (detail?.body) return detail.body;
      const legacy = await tryCoreJson(
        `/boxes?project_id=${encodeURIComponent(targetProjectId)}`,
      );
      return legacy?.body?.items?.find((item) => item.box_id === state.box_id);
    },
    (detail) =>
      detail?.status === "online" || detail?.connectivity === "online",
    "Box did not become online",
    60,
  );
  return { boxId: state.box_id, child };
}

async function findOnlineLocalDockerBox() {
  const inventory = await coreJson("/users/me/boxes");
  const items = inventory.body.items ?? [];
  return items.find(
    (item) =>
      item.status === "online" &&
      item.runtimes?.some((runtime) => runtime.name === "local-docker"),
  );
}

async function assignBox(targetBoxId, targetProjectId) {
  const target = await coreRequest(
    `/projects/${encodeURIComponent(targetProjectId)}/boxes/${encodeURIComponent(targetBoxId)}`,
    { method: "PUT" },
  );
  if (target.response.ok) return;
  const legacy = await coreRequest(
    `/projects/${encodeURIComponent(targetProjectId)}/box`,
    { body: { box_id: targetBoxId }, method: "PUT" },
  );
  assert(
    legacy.response.ok,
    `Could not assign Box: ${target.response.status} ${target.text} / ${legacy.response.status} ${legacy.text}`,
  );
}

async function mcpInitialize() {
  const response = await mcpHttp({
    body: {
      id: mcpRequestId++,
      jsonrpc: "2.0",
      method: "initialize",
      params: {
        capabilities: {},
        clientInfo: {
          name: "mmdash-coding-agent-acceptance",
          version: "0.1.0",
        },
        protocolVersion,
      },
    },
  });
  assert(
    response.response.ok,
    `MCP initialize failed: ${JSON.stringify(response.body)}`,
  );
  negotiatedProtocol =
    response.body?.result?.protocolVersion ?? protocolVersion;
  const session = response.response.headers.get("x-mmdash-session-id");
  assert(session, "MCP initialize did not return x-mmdash-session-id.");
  return session;
}

async function mcpRequest(method, params) {
  const response = await mcpHttp({
    body: { id: mcpRequestId++, jsonrpc: "2.0", method, params },
    session: mcpSession,
  });
  assert(
    response.response.ok,
    `MCP ${method} failed: ${JSON.stringify(response.body)}`,
  );
  const payload = response.body?.result ?? response.body;
  if (response.body?.error) {
    throw new Error(
      `MCP ${method} JSON-RPC error: ${JSON.stringify(response.body.error)}`,
    );
  }
  return payload;
}

async function mcpTool(name, args) {
  const result = await mcpRequest("tools/call", {
    arguments: args,
    name,
  });
  if (result?.isError) {
    throw new Error(
      `MCP tool ${name} returned an error: ${JSON.stringify(result)}`,
    );
  }
  if (result?.structuredContent) return result.structuredContent;
  const text = result?.content?.find((entry) => entry.type === "text")?.text;
  assert(text, `MCP tool ${name} returned no structured result.`);
  return JSON.parse(text);
}

async function mcpHttp({ body, session }) {
  const headers = {
    accept: "application/json, text/event-stream",
    authorization: `Bearer ${coreToken}`,
    "content-type": "application/json",
    ...(session
      ? {
          "mcp-protocol-version": negotiatedProtocol ?? protocolVersion,
          "x-mmdash-session-id": session,
        }
      : {}),
  };
  const response = await fetch(mcpUrl, {
    body: JSON.stringify(body),
    headers,
    method: "POST",
    signal: AbortSignal.timeout(30_000),
  });
  const text = await response.text();
  return { body: parseMcpBody(text), response };
}

function parseMcpBody(text) {
  if (!text.trimStart().startsWith("event:") && !text.includes("data: ")) {
    try {
      return JSON.parse(text);
    } catch {
      return text;
    }
  }
  const frames = text
    .split(/\r?\n\r?\n/u)
    .flatMap((frame) => frame.split(/\r?\n/u))
    .filter((line) => line.startsWith("data:"))
    .map((line) => line.slice(5).trim())
    .filter(Boolean);
  const last = frames.at(-1) ?? "";
  try {
    return JSON.parse(last);
  } catch {
    return text;
  }
}

function createFixtureRepository(directory, packageRequirements) {
  const worktree = join(directory, "worktree");
  const bare = join(directory, "remote.git");
  runGit(["init", "--bare", bare]);
  runGit(["init", worktree]);
  runGit(["-C", worktree, "config", "user.name", "mmdash Acceptance"]);
  runGit(["-C", worktree, "config", "user.email", "acceptance@mmdash.local"]);
  runGit(["-C", worktree, "checkout", "-b", "main"]);
  writeFixtureFile(
    worktree,
    "README.md",
    "Coding Agent environment acceptance\n",
  );
  const requirements = packageRequirements.trim();
  assert(requirements, "Acceptance dependencies must not be empty.");
  writeFixtureFile(worktree, "requirements.in", `${requirements}\n`);
  runCommand(
    "uv",
    [
      "pip",
      "compile",
      "--generate-hashes",
      "--python-version",
      "3.12",
      "--python-platform",
      "x86_64-manylinux_2_28",
      "--output-file",
      "requirements.lock",
      "requirements.in",
    ],
    worktree,
  );
  const lockContents = readFileSync(
    join(worktree, "requirements.lock"),
    "utf8",
  );
  assert(
    /numpy==[^\s]+[\s\S]*--hash=sha256:[0-9a-f]{64}/iu.test(lockContents) &&
      /matplotlib==[^\s]+[\s\S]*--hash=sha256:[0-9a-f]{64}/iu.test(
        lockContents,
      ),
    "uv did not produce hash-pinned NumPy and Matplotlib requirements.",
  );
  writeFixtureFile(
    worktree,
    "run.py",
    `from pathlib import Path\n\nimport matplotlib\nmatplotlib.use("Agg")\nimport matplotlib.pyplot as plt\nimport numpy as np\n\nvalues = np.arange(6, dtype=float) ** 2\nfigures = Path("/output/figures")\nfigures.mkdir(parents=True, exist_ok=True)\nplt.plot(values)\nplt.title("mmdash dependency preparation")\nplt.savefig(figures / "dependency-plot.png")\nplt.close()\nmessage = f"numpy={np.__version__} matplotlib={matplotlib.__version__} sum={values.sum():.1f}"\nprint(f"MMDASH_CODING_AGENT_STDOUT {message}", flush=True)\nPath("/output/summary.md").write_text(f"Coding Agent environment preparation passed: {message}\\n", encoding="utf-8")\n`,
  );
  runGit(["-C", worktree, "add", "."]);
  runGit(["-C", worktree, "commit", "-m", "Coding Agent environment fixture"]);
  const sourceCommit = runGit([
    "-C",
    worktree,
    "rev-parse",
    "HEAD",
  ]).stdout.trim();
  runGit(["-C", worktree, "remote", "add", "origin", bare]);
  runGit(["-C", worktree, "push", "origin", "HEAD:refs/heads/main"]);
  runGit(["-C", worktree, "branch", "article", "main"]);
  runGit(["-C", worktree, "branch", "result", "main"]);
  runGit(["-C", worktree, "push", "origin", "article", "result"]);
  return {
    dependencies: requirements.split(/\r?\n/u).filter(Boolean),
    remote: bare,
    sourceCommit,
  };
}

function writeFixtureFile(directory, filename, contents) {
  const path = join(directory, filename);
  writeFileSync(path, contents, "utf8");
}

function runGit(args) {
  const result = spawnSync("git", args, { cwd: root, encoding: "utf8" });
  assert(
    result.status === 0,
    `git ${args.join(" ")} failed:\n${result.stdout}\n${result.stderr}`,
  );
  return result;
}

function runCommand(command, args, cwd = root) {
  const result = spawnSync(command, args, { cwd, encoding: "utf8" });
  assert(
    result.status === 0,
    `${command} ${args.join(" ")} failed:\n${result.stdout}\n${result.stderr}`,
  );
  return result;
}

async function cleanup() {
  if (boxId && projectId && coreToken) {
    await tryCoreRequest(
      `/projects/${encodeURIComponent(projectId)}/boxes/${encodeURIComponent(boxId)}`,
      { method: "DELETE" },
    );
  }
  if (temporaryBox && boxId && coreToken) {
    await tryCoreRequest(
      `/users/me/boxes/${encodeURIComponent(boxId)}/revoke`,
      {
        body: { mode: "force" },
        method: "POST",
      },
    );
    await tryCoreRequest(`/boxes/${encodeURIComponent(boxId)}`, {
      method: "DELETE",
    });
  }
  if (boxProcess?.child) await stopChild(boxProcess.child);
  if (temporaryRoot) rmSync(temporaryRoot, { force: true, recursive: true });
}

async function bffJson(path, options = {}) {
  return jsonRequest(`${bffUrl}${path}`, {
    ...options,
    headers: { ...(options.headers ?? {}), cookie },
  });
}

async function coreJson(path, options = {}) {
  return jsonRequest(`${coreUrl}/v1${path}`, {
    ...options,
    headers: {
      authorization: `Bearer ${coreToken}`,
      ...(options.headers ?? {}),
    },
  });
}

async function coreRequest(path, options = {}) {
  return request(`${coreUrl}/v1${path}`, {
    ...options,
    headers: {
      authorization: `Bearer ${coreToken}`,
      ...(options.headers ?? {}),
    },
  });
}

async function tryCoreJson(path) {
  const result = await tryCoreRequest(path);
  if (!result.response.ok) return undefined;
  try {
    return { body: result.text ? JSON.parse(result.text) : {} };
  } catch {
    return undefined;
  }
}

async function tryCoreRequest(path, options = {}) {
  try {
    return await coreRequest(path, options);
  } catch {
    return {
      response: new Response(null, { status: 599 }),
      text: async () => "",
    };
  }
}

async function jsonRequest(url, options = {}) {
  const result = await request(url, options);
  assert(
    result.response.ok,
    `${options.method ?? "GET"} ${url} -> ${result.response.status}: ${result.text}`,
  );
  let body = {};
  if (result.text) body = JSON.parse(result.text);
  return { body, response: result.response };
}

async function request(url, options = {}) {
  const headers = new Headers(options.headers);
  let body = options.body;
  if (body !== undefined && typeof body !== "string") {
    headers.set("content-type", "application/json");
    body = JSON.stringify(body);
  }
  const response = await fetch(url, {
    ...options,
    body,
    headers,
    signal: AbortSignal.timeout(30_000),
  });
  return { response, text: await response.text() };
}

async function checkHealth(url, name) {
  const response = await fetch(url, { signal: AbortSignal.timeout(5_000) });
  assert(response.ok, `${name} health check failed: HTTP ${response.status}`);
}

async function poll(load, ready, message, attempts = 120) {
  let last;
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    last = await load();
    if (ready(last)) return last;
    await delay(1_000);
  }
  throw new Error(`${message}. Last state: ${JSON.stringify(last)}`);
}

function readState(path) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch {
    return undefined;
  }
}

async function stopChild(child) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  child.kill();
  await Promise.race([
    new Promise((resolveStopped) => child.once("close", resolveStopped)),
    delay(5_000),
  ]);
}

function trim(value) {
  return String(value).replace(/\/$/u, "");
}

function delay(milliseconds) {
  return new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds));
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}
