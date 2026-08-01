import { spawn } from "node:child_process";
import { createWriteStream, existsSync } from "node:fs";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import net from "node:net";
import path from "node:path";
import process from "node:process";

const repositoryRoot = path.resolve(import.meta.dirname, "..");
const host = "127.0.0.1";
const pnpmVersion = "11.9.0";

export const serviceOrder = [
  "postgres",
  "minio",
  "core",
  "web-bff",
  "mcp-gateway",
  "web",
];

export const portDefinitions = {
  bff: ["MMDASH_TESTENV_BFF_PORT", 13_001],
  core: ["MMDASH_TESTENV_CORE_PORT", 18_080],
  mcp: ["MMDASH_TESTENV_MCP_PORT", 13_002],
  minio: ["MMDASH_TESTENV_MINIO_PORT", 19_000],
  minioConsole: ["MMDASH_TESTENV_MINIO_CONSOLE_PORT", 19_001],
  postgres: ["MMDASH_TESTENV_POSTGRES_PORT", 15_432],
  web: ["MMDASH_TESTENV_WEB_PORT", 13_000],
};

export function createLayout(root = repositoryRoot) {
  const testenvRoot = path.join(root, ".testenv");
  const runtimeRoot = path.join(testenvRoot, "runtime");
  return {
    cacheRoot: path.join(testenvRoot, "cache"),
    minioCerts: path.join(runtimeRoot, "config", "minio", "certs"),
    minioData: path.join(runtimeRoot, "data", "minio"),
    pnpmStore: path.join(testenvRoot, "cache", "pnpm-store"),
    postgresData: path.join(runtimeRoot, "data", "postgres"),
    postgresSocket: path.join(runtimeRoot, "run", "postgres"),
    pythonEnvironment: path.join(testenvRoot, "python"),
    repositoryRoot: root,
    runtimeRoot,
    serviceLogs: path.join(runtimeRoot, "logs"),
    stopRequest: path.join(runtimeRoot, "run", "stop.request"),
    supervisorLock: path.join(runtimeRoot, "run", "supervisor.json"),
    testenvRoot,
    temporaryRoot: path.join(runtimeRoot, "tmp"),
  };
}

export function assertPathWithin(parent, candidate) {
  const relative = path.relative(path.resolve(parent), path.resolve(candidate));
  if (
    relative === "" ||
    (!relative.startsWith("..") && !path.isAbsolute(relative))
  ) {
    return;
  }
  throw new Error(`${candidate} is outside the isolated environment ${parent}`);
}

export function resolvePorts(environment = process.env) {
  const ports = Object.fromEntries(
    Object.entries(portDefinitions).map(([name, [variable, fallback]]) => {
      const raw = environment[variable] ?? String(fallback);
      const value = Number(raw);
      if (!Number.isInteger(value) || value < 1 || value > 65_535) {
        throw new Error(`${variable} must be an integer between 1 and 65535`);
      }
      return [name, value];
    }),
  );
  const values = Object.values(ports);
  if (new Set(values).size !== values.length) {
    throw new Error("MMDASH_TESTENV_*_PORT values must be unique");
  }
  return ports;
}

export function createIsolatedEnvironment(
  layout = createLayout(),
  environment = process.env,
) {
  const isolated = { ...environment };
  const assignments = {
    APPDATA: path.join(layout.runtimeRoot, "user", "appdata"),
    COREPACK_ENABLE_DOWNLOAD_PROMPT: "0",
    COREPACK_HOME: path.join(layout.cacheRoot, "corepack"),
    GOCACHE: path.join(layout.cacheRoot, "go-build"),
    GOMODCACHE: path.join(layout.cacheRoot, "go-mod"),
    GOPATH: path.join(layout.testenvRoot, "go"),
    GOTOOLCHAIN: "local",
    LOCALAPPDATA: path.join(layout.runtimeRoot, "user", "localappdata"),
    NPM_CONFIG_CACHE: path.join(layout.cacheRoot, "npm"),
    NPM_CONFIG_USERCONFIG: path.join(layout.runtimeRoot, "config", "npmrc"),
    PIXI_CACHE_DIR: path.join(layout.cacheRoot, "pixi"),
    PIXI_HOME: path.join(layout.testenvRoot, "pixi-home"),
    PIXI_NO_CONFIG: "1",
    PNPM_HOME: path.join(layout.testenvRoot, "pnpm-home"),
    RATTLER_CACHE_DIR: path.join(layout.cacheRoot, "rattler"),
    TEMP: layout.temporaryRoot,
    TMP: layout.temporaryRoot,
    TMPDIR: layout.temporaryRoot,
    UV_CACHE_DIR: path.join(layout.cacheRoot, "uv"),
    UV_PROJECT_ENVIRONMENT: layout.pythonEnvironment,
    XDG_CACHE_HOME: path.join(layout.cacheRoot, "xdg"),
    XDG_CONFIG_HOME: path.join(layout.runtimeRoot, "config", "xdg"),
    XDG_DATA_HOME: path.join(layout.runtimeRoot, "user", "xdg-data"),
    XDG_STATE_HOME: path.join(layout.runtimeRoot, "user", "xdg-state"),
  };
  Object.assign(isolated, assignments);
  for (const key of Object.keys(isolated)) {
    if (key !== "PATH" && key.toUpperCase() === "PATH") {
      delete isolated[key];
    }
  }
  const nodePrefix = path.dirname(process.execPath);
  const corepackShims = path.join(
    nodePrefix,
    "node_modules",
    "corepack",
    "shims",
  );
  const pnpmBin =
    process.platform === "win32"
      ? assignments.PNPM_HOME
      : path.join(assignments.PNPM_HOME, "bin");
  isolated.PATH = [
    corepackShims,
    pnpmBin,
    environment.PATH ?? environment.Path ?? "",
  ]
    .filter(Boolean)
    .join(path.delimiter);
  return isolated;
}

export function createServiceConfiguration(
  ports = resolvePorts(),
  layout = createLayout(),
) {
  const databaseUrl = `postgres://mmdash@${host}:${ports.postgres}/mmdash?sslmode=disable`;
  const coreUrl = `http://${host}:${ports.core}`;
  const minioUrl = `http://${host}:${ports.minio}`;
  const webUrl = `http://${host}:${ports.web}`;
  return {
    databaseUrl,
    coreUrl,
    minioUrl,
    ports,
    webUrl,
    environments: {
      core: {
        CORE_ADDR: `${host}:${ports.core}`,
        CORE_OPENAPI_PATH: path.join(
          layout.repositoryRoot,
          "contracts",
          "openapi",
          "core.yaml",
        ),
        DATABASE_URL: databaseUrl,
        OBJECT_STORAGE_ACCESS_KEY: "mmdash",
        OBJECT_STORAGE_BUCKET: "mmdash",
        OBJECT_STORAGE_ENDPOINT: minioUrl,
        OBJECT_STORAGE_SECRET_KEY: "local-minio-secret",
      },
      mcp: {
        CORE_BASE_URL: coreUrl,
        MCP_AGENT_PROJECTS: "*",
        MCP_AGENT_TOKEN: "local-agent-token-change-before-production",
        MCP_AGENT_TOOLS: "*",
        MCP_ALLOWED_HOSTS: `localhost,${host}`,
        MCP_ALLOWED_ORIGINS: `${webUrl},http://${host}:${ports.mcp}`,
        MCP_CLI_PROJECTS: "*",
        MCP_CLI_TOKEN: "local-cli-token-change-before-production",
        MCP_CLI_TOOLS: "*",
        MCP_GATEWAY_HOST: host,
        MCP_GATEWAY_PORT: String(ports.mcp),
      },
      minio: {
        MINIO_ROOT_PASSWORD: "local-minio-secret",
        MINIO_ROOT_USER: "mmdash",
      },
      web: {
        BFF_INTERNAL_URL: `http://${host}:${ports.bff}`,
        CORE_INTERNAL_URL: coreUrl,
        MCP_INTERNAL_URL: `http://${host}:${ports.mcp}`,
        MMDASH_LOCAL_UNIFIED_PROXY: "true",
      },
      webBff: {
        BFF_COOKIE_SECRET: "local-pixi-cookie-secret-change-before-production",
        BFF_HOST: host,
        BFF_PORT: String(ports.bff),
        CORE_BASE_URL: coreUrl,
      },
    },
  };
}

async function ensureDirectories(layout) {
  const directories = [
    layout.cacheRoot,
    layout.minioCerts,
    layout.minioData,
    layout.postgresSocket,
    layout.runtimeRoot,
    layout.serviceLogs,
    layout.temporaryRoot,
  ];
  for (const directory of directories) {
    assertPathWithin(layout.testenvRoot, directory);
    await mkdir(directory, { recursive: true });
  }
}

function commandInvocation(command, arguments_) {
  if (!["corepack", "pnpm"].includes(command)) {
    return { arguments_, command };
  }
  const corepackCli = path.join(
    path.dirname(process.execPath),
    "node_modules",
    "corepack",
    "dist",
    "corepack.js",
  );
  return {
    arguments_:
      command === "pnpm"
        ? [corepackCli, "pnpm", ...arguments_]
        : [corepackCli, ...arguments_],
    command: process.execPath,
  };
}

async function execute(
  command,
  arguments_ = [],
  {
    allowFailure = false,
    capture = false,
    cwd = repositoryRoot,
    environment = process.env,
  } = {},
) {
  const invocation = commandInvocation(command, arguments_);
  return await new Promise((resolve, reject) => {
    const child = spawn(invocation.command, invocation.arguments_, {
      cwd,
      env: environment,
      stdio: capture ? ["ignore", "pipe", "pipe"] : "inherit",
      windowsHide: true,
    });
    let stdout = "";
    let stderr = "";
    child.stdout?.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr?.on("data", (chunk) => {
      stderr += chunk;
    });
    child.once("error", reject);
    child.once("close", (code, signal) => {
      const result = { code: code ?? 1, signal, stderr, stdout };
      if (result.code === 0 || allowFailure) {
        resolve(result);
        return;
      }
      const detail = stderr.trim() || stdout.trim();
      reject(
        new Error(
          `${command} exited with code ${result.code}${detail ? `: ${detail}` : ""}`,
        ),
      );
    });
  });
}

async function installDependencies(layout, environment) {
  await ensureDirectories(layout);
  await execute("corepack", ["install", "--global", `pnpm@${pnpmVersion}`], {
    environment,
  });
  await execute(
    "pnpm",
    ["install", "--frozen-lockfile", "--store-dir", layout.pnpmStore],
    { environment },
  );
  await execute("uv", ["sync", "--all-packages", "--frozen"], {
    environment,
  });
}

async function isPortAvailable(port) {
  return await new Promise((resolve) => {
    const server = net.createServer();
    server.unref();
    server.once("error", () => resolve(false));
    server.listen({ exclusive: true, host, port }, () => {
      server.close(() => resolve(true));
    });
  });
}

async function assertPortsAvailable(ports) {
  const occupied = [];
  for (const [name, port] of Object.entries(ports)) {
    if (!(await isPortAvailable(port))) {
      occupied.push(`${name}=${port}`);
    }
  }
  if (occupied.length > 0) {
    throw new Error(
      `Isolated development ports are already in use: ${occupied.join(", ")}`,
    );
  }
}

function pipeServiceOutput(name, stream, destination, log) {
  let pending = "";
  stream?.on("data", (chunk) => {
    const text = chunk.toString();
    log.write(text);
    pending += text;
    const lines = pending.split(/\r?\n/);
    pending = lines.pop() ?? "";
    for (const line of lines) {
      destination.write(`[${name}] ${line}\n`);
    }
  });
  stream?.once("end", () => {
    if (pending) {
      destination.write(`[${name}] ${pending}\n`);
    }
  });
}

function startManagedProcess(
  name,
  command,
  arguments_,
  { cwd = repositoryRoot, environment = process.env, layout },
) {
  const invocation = commandInvocation(command, arguments_);
  const log = createWriteStream(path.join(layout.serviceLogs, `${name}.log`), {
    flags: "a",
  });
  const child = spawn(invocation.command, invocation.arguments_, {
    cwd,
    detached: process.platform !== "win32",
    env: environment,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });
  pipeServiceOutput(name, child.stdout, process.stdout, log);
  pipeServiceOutput(name, child.stderr, process.stderr, log);
  const exited = new Promise((resolve) => {
    child.once("exit", (code, signal) => {
      log.end();
      resolve({ code, signal });
    });
  });
  return { child, exited, name };
}

async function waitForCondition(
  description,
  check,
  service,
  shutdownRequested,
  timeoutMilliseconds = 45_000,
) {
  const deadline = Date.now() + timeoutMilliseconds;
  while (Date.now() < deadline) {
    if (shutdownRequested()) {
      throw new Error(`Interrupted while waiting for ${description}`);
    }
    if (service.child.exitCode !== null || service.child.signalCode !== null) {
      throw new Error(
        `${service.name} exited before ${description} became ready`,
      );
    }
    try {
      if (await check()) {
        return;
      }
    } catch {
      // Readiness failures are expected while a service is starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`Timed out waiting for ${description}`);
}

async function waitForHttp(url, service, shutdownRequested) {
  await waitForCondition(
    url,
    async () => {
      const response = await fetch(url, { signal: AbortSignal.timeout(1_000) });
      return response.ok;
    },
    service,
    shutdownRequested,
  );
}

async function waitForPostgres(port, service, shutdownRequested, environment) {
  await waitForCondition(
    "PostgreSQL",
    async () => {
      const result = await execute(
        "pg_isready",
        ["-h", host, "-p", String(port), "-U", "mmdash", "-d", "postgres"],
        { allowFailure: true, capture: true, environment },
      );
      return result.code === 0;
    },
    service,
    shutdownRequested,
  );
}

async function initializePostgres(layout, environment) {
  if (existsSync(path.join(layout.postgresData, "PG_VERSION"))) {
    return;
  }
  await mkdir(layout.postgresData, { recursive: true });
  await execute(
    "initdb",
    [
      "--pgdata",
      layout.postgresData,
      "--username",
      "mmdash",
      "--auth-local",
      "trust",
      "--auth-host",
      "trust",
      "--encoding",
      "UTF8",
      "--no-locale",
    ],
    { environment },
  );
}

async function ensureDatabase(port, environment) {
  const query = await execute(
    "psql",
    [
      "-h",
      host,
      "-p",
      String(port),
      "-U",
      "mmdash",
      "-d",
      "postgres",
      "-tAc",
      "SELECT 1 FROM pg_database WHERE datname = 'mmdash'",
    ],
    { capture: true, environment },
  );
  if (query.stdout.trim() === "1") {
    return;
  }
  await execute(
    "createdb",
    ["-h", host, "-p", String(port), "-U", "mmdash", "mmdash"],
    { environment },
  );
}

function isProcessAlive(pid) {
  if (!Number.isInteger(pid) || pid <= 0) {
    return false;
  }
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

async function readSupervisorLock(layout) {
  try {
    return JSON.parse(await readFile(layout.supervisorLock, "utf8"));
  } catch (error) {
    if (error && typeof error === "object" && error.code === "ENOENT") {
      return undefined;
    }
    throw error;
  }
}

async function acquireSupervisorLock(layout) {
  await mkdir(path.dirname(layout.supervisorLock), { recursive: true });
  const existing = await readSupervisorLock(layout);
  if (existing && isProcessAlive(existing.pid)) {
    throw new Error(
      `The isolated development environment is already running as PID ${existing.pid}`,
    );
  }
  if (existing) {
    await rm(layout.supervisorLock, { force: true });
  }
  await rm(layout.stopRequest, { force: true });
  await writeFile(
    layout.supervisorLock,
    `${JSON.stringify({ pid: process.pid, startedAt: new Date().toISOString() })}\n`,
    { encoding: "utf8", flag: "wx" },
  );
}

async function releaseSupervisorLock(layout) {
  const lock = await readSupervisorLock(layout);
  if (!lock || lock.pid === process.pid) {
    await rm(layout.supervisorLock, { force: true });
  }
  await rm(layout.stopRequest, { force: true });
}

async function waitForExit(service, timeoutMilliseconds) {
  if (service.child.exitCode !== null || service.child.signalCode !== null) {
    return true;
  }
  return await Promise.race([
    service.exited.then(() => true),
    new Promise((resolve) =>
      setTimeout(() => resolve(false), timeoutMilliseconds),
    ),
  ]);
}

async function stopProcessTree(service, environment) {
  if (service.child.exitCode !== null || service.child.signalCode !== null) {
    return;
  }
  if (process.platform === "win32") {
    await execute("taskkill", ["/PID", String(service.child.pid), "/T", "/F"], {
      allowFailure: true,
      capture: true,
      environment,
    });
    await waitForExit(service, 5_000);
    return;
  }
  try {
    process.kill(-service.child.pid, "SIGTERM");
  } catch {
    return;
  }
  if (!(await waitForExit(service, 5_000))) {
    try {
      process.kill(-service.child.pid, "SIGKILL");
    } catch {
      return;
    }
    await waitForExit(service, 5_000);
  }
}

async function stopServices(services, layout, environment) {
  for (const service of [...services].reverse()) {
    if (service.name === "postgres") {
      await execute(
        "pg_ctl",
        ["stop", "-D", layout.postgresData, "-m", "fast", "-w", "-t", "10"],
        { allowFailure: true, capture: true, environment },
      );
      await waitForExit(service, 10_000);
      continue;
    }
    await stopProcessTree(service, environment);
  }
}

async function startDevelopmentEnvironment(layout, environment, ports) {
  await ensureDirectories(layout);
  await assertPortsAvailable(ports);
  await acquireSupervisorLock(layout);

  const configuration = createServiceConfiguration(ports, layout);
  const services = [];
  let interrupted = false;
  const shutdownRequested = () => interrupted || existsSync(layout.stopRequest);
  const requestShutdown = () => {
    interrupted = true;
  };
  process.once("SIGINT", requestShutdown);
  process.once("SIGTERM", requestShutdown);

  try {
    await initializePostgres(layout, environment);
    const postgresArguments = [
      "-D",
      layout.postgresData,
      "-h",
      host,
      "-p",
      String(ports.postgres),
    ];
    if (process.platform !== "win32") {
      postgresArguments.push("-k", layout.postgresSocket);
    }
    const postgres = startManagedProcess(
      "postgres",
      "postgres",
      postgresArguments,
      {
        environment,
        layout,
      },
    );
    services.push(postgres);
    await waitForPostgres(
      ports.postgres,
      postgres,
      shutdownRequested,
      environment,
    );
    await ensureDatabase(ports.postgres, environment);

    const minio = startManagedProcess(
      "minio",
      "minio",
      [
        "server",
        layout.minioData,
        "--address",
        `${host}:${ports.minio}`,
        "--console-address",
        `${host}:${ports.minioConsole}`,
        "--certs-dir",
        layout.minioCerts,
      ],
      {
        environment: { ...environment, ...configuration.environments.minio },
        layout,
      },
    );
    services.push(minio);
    await waitForHttp(
      `${configuration.minioUrl}/minio/health/ready`,
      minio,
      shutdownRequested,
    );

    await execute("pnpm", ["migrate"], {
      environment: {
        ...environment,
        DATABASE_URL: configuration.databaseUrl,
        MIGRATIONS_DIR: path.join(
          layout.repositoryRoot,
          "backend",
          "migrations",
        ),
      },
    });

    const core = startManagedProcess(
      "core",
      "go",
      ["run", "./backend/cmd/core-server"],
      {
        environment: { ...environment, ...configuration.environments.core },
        layout,
      },
    );
    services.push(core);
    await waitForHttp(
      `${configuration.coreUrl}/health/ready`,
      core,
      shutdownRequested,
    );

    const webBff = startManagedProcess(
      "web-bff",
      "pnpm",
      ["--filter", "@mmdash/web-bff", "dev"],
      {
        environment: { ...environment, ...configuration.environments.webBff },
        layout,
      },
    );
    services.push(webBff);
    await waitForHttp(
      `http://${host}:${ports.bff}/health/live`,
      webBff,
      shutdownRequested,
    );

    const mcp = startManagedProcess(
      "mcp-gateway",
      "pnpm",
      ["--filter", "@mmdash/mcp-gateway", "dev"],
      {
        environment: { ...environment, ...configuration.environments.mcp },
        layout,
      },
    );
    services.push(mcp);
    await waitForHttp(
      `http://${host}:${ports.mcp}/health/live`,
      mcp,
      shutdownRequested,
    );

    const web = startManagedProcess(
      "web",
      "pnpm",
      [
        "--filter",
        "@mmdash/web",
        "exec",
        "next",
        "dev",
        "--hostname",
        host,
        "--port",
        String(ports.web),
      ],
      {
        environment: { ...environment, ...configuration.environments.web },
        layout,
      },
    );
    services.push(web);
    await waitForHttp(
      `${configuration.webUrl}/projects`,
      web,
      shutdownRequested,
    );

    console.log(
      `Isolated development environment is ready at ${configuration.webUrl}`,
    );
    console.log(
      `Run the smoke check in another terminal with ${
        process.platform === "win32"
          ? ".\\scripts\\testenv.ps1 smoke"
          : "./scripts/testenv.sh smoke"
      }`,
    );

    const signal = new Promise((resolve) => {
      const poll = setInterval(() => {
        if (shutdownRequested()) {
          clearInterval(poll);
          resolve();
        }
      }, 100);
    });
    const unexpectedExit = Promise.race(
      services.map((service) =>
        service.exited.then(({ code, signal: childSignal }) => {
          if (shutdownRequested()) {
            return;
          }
          throw new Error(
            `${service.name} exited unexpectedly (${childSignal ?? code ?? "unknown"})`,
          );
        }),
      ),
    );
    await Promise.race([signal, unexpectedExit]);
  } finally {
    process.removeListener("SIGINT", requestShutdown);
    process.removeListener("SIGTERM", requestShutdown);
    await stopServices(services, layout, environment);
    await releaseSupervisorLock(layout);
  }
}

async function doctor(layout, environment, ports) {
  await ensureDirectories(layout);
  for (const writablePath of [
    layout.cacheRoot,
    layout.pythonEnvironment,
    layout.runtimeRoot,
    environment.COREPACK_HOME,
    environment.GOCACHE,
    environment.GOMODCACHE,
    environment.UV_CACHE_DIR,
  ]) {
    assertPathWithin(layout.testenvRoot, writablePath);
  }
  const versions = [
    ["node", ["--version"]],
    ["corepack", ["--version"]],
    ["go", ["version"]],
    ["python", ["--version"]],
    ["uv", ["--version"]],
    ["postgres", ["--version"]],
    ["minio", ["--version"]],
  ];
  console.log(`Repository: ${layout.repositoryRoot}`);
  console.log(`Isolated environment: ${layout.testenvRoot}`);
  for (const [command, arguments_] of versions) {
    const result = await execute(command, arguments_, {
      capture: true,
      environment,
    });
    console.log(
      `${command}: ${(result.stdout || result.stderr).trim().split("\n")[0]}`,
    );
  }
  for (const [name, port] of Object.entries(ports)) {
    console.log(
      `${name}: ${host}:${port} (${(await isPortAvailable(port)) ? "available" : "in use"})`,
    );
  }
  const lock = await readSupervisorLock(layout);
  console.log(
    lock && isProcessAlive(lock.pid)
      ? `supervisor: running as PID ${lock.pid}`
      : "supervisor: stopped",
  );
}

async function resetEnvironment(layout, ports) {
  assertPathWithin(layout.testenvRoot, layout.runtimeRoot);
  const lock = await readSupervisorLock(layout);
  if (lock && isProcessAlive(lock.pid)) {
    throw new Error(
      `Cannot reset while the environment is running as PID ${lock.pid}`,
    );
  }
  const occupied = [];
  for (const [name, port] of Object.entries(ports)) {
    if (!(await isPortAvailable(port))) {
      occupied.push(`${name}=${port}`);
    }
  }
  if (occupied.length > 0) {
    throw new Error(
      `Cannot reset while isolated development ports are in use: ${occupied.join(", ")}`,
    );
  }
  await rm(layout.runtimeRoot, { force: true, recursive: true });
  console.log(`Removed isolated runtime data at ${layout.runtimeRoot}`);
}

async function stopEnvironment(layout) {
  const lock = await readSupervisorLock(layout);
  if (!lock || !isProcessAlive(lock.pid)) {
    if (lock) {
      await rm(layout.supervisorLock, { force: true });
    }
    console.log("The isolated development environment is already stopped.");
    return;
  }
  await writeFile(
    layout.stopRequest,
    `${JSON.stringify({ requestedAt: new Date().toISOString() })}\n`,
    "utf8",
  );
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    const currentLock = await readSupervisorLock(layout);
    if (
      !currentLock ||
      currentLock.pid !== lock.pid ||
      !isProcessAlive(lock.pid)
    ) {
      console.log("The isolated development environment stopped cleanly.");
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(
    `Timed out waiting for supervisor PID ${lock.pid}; inspect it before forcing termination.`,
  );
}

async function main() {
  const command = process.argv[2] ?? "doctor";
  const layout = createLayout();
  const environment = createIsolatedEnvironment(layout);
  const ports = resolvePorts(environment);
  switch (command) {
    case "install":
      await installDependencies(layout, environment);
      break;
    case "test":
      await execute("pnpm", ["test"], { environment });
      break;
    case "check":
      await execute("pnpm", ["check"], { environment });
      break;
    case "doctor":
      await doctor(layout, environment, ports);
      break;
    case "dev":
      await startDevelopmentEnvironment(layout, environment, ports);
      break;
    case "smoke":
      await execute("pnpm", ["smoke"], {
        environment: {
          ...environment,
          MMDASH_SMOKE_CORE_URL: `http://${host}:${ports.core}`,
          MMDASH_SMOKE_MCP_URL: `http://${host}:${ports.mcp}`,
          MMDASH_SMOKE_URL: `http://${host}:${ports.web}`,
        },
      });
      break;
    case "reset":
      await resetEnvironment(layout, ports);
      break;
    case "stop":
      await stopEnvironment(layout);
      break;
    default:
      throw new Error(
        `Unknown test environment command '${command}'. Expected install, test, check, doctor, dev, smoke, stop, or reset.`,
      );
  }
}

const invokedDirectly =
  process.argv[1] &&
  path.resolve(process.argv[1]) === path.resolve(import.meta.filename);

if (invokedDirectly) {
  try {
    await main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : error);
    process.exitCode = 1;
  }
}
