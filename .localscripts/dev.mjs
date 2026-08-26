import { spawn } from "node:child_process";
import {
  chmodSync,
  cpSync,
  copyFileSync,
  closeSync,
  existsSync,
  mkdirSync,
  openSync,
  readFileSync,
  rmSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { createConnection, createServer } from "node:net";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import path from "node:path";

const repositoryRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const composeFile = path.join(
  repositoryRoot,
  "deploy",
  "compose",
  "compose.yaml",
);
const isWindows = process.platform === "win32";
const developmentToolsDirectory = path.join(
  repositoryRoot,
  ".tmp",
  "dev-tools",
);
const developmentDownloadsDirectory = path.join(
  repositoryRoot,
  "apps",
  "web",
  "public",
  "downloads",
  "dev",
);
const developmentLockPath = path.join(
  developmentToolsDirectory,
  "dev.lock",
);
const repoAskPassPath = path.join(
  developmentToolsDirectory,
  `mmdash-git-askpass${isWindows ? ".exe" : ""}`,
);
const developmentCliPath = path.join(
  developmentToolsDirectory,
  `mmdash${isWindows ? ".exe" : ""}`,
);
const developmentBoxPath = path.join(
  developmentToolsDirectory,
  `mmdash-box${isWindows ? ".exe" : ""}`,
);
const developmentMboxPath = path.join(
  developmentToolsDirectory,
  `mbox${isWindows ? ".exe" : ""}`,
);
const developmentCliConfigDirectory = path.join(
  developmentToolsDirectory,
  "cli-config",
);
const developmentCliLauncherPath = path.join(
  developmentToolsDirectory,
  `mmdash-local${isWindows ? ".cmd" : ".sh"}`,
);
const managedProcesses = [];
const fallbackProxyUrl = "http://127.0.0.1:22334";
const connectionFailurePattern =
  /\b(?:connectex|ECONNREFUSED|ECONNRESET|ETIMEDOUT)\b|dial (?:tcp|udp)|connection (?:attempt failed|refused|reset|timed out)|connect fail(?:ed|ure)?/i;
let issuedWorkerCredential;
let developmentLock;
let developmentWorkerContainerName;
let shuttingDown = false;

const commands = {
  docker: isWindows ? "docker.exe" : "docker",
  go: isWindows ? "go.exe" : "go",
  pnpm: isWindows ? "pnpm.cmd" : "pnpm",
  uv: isWindows ? "uv.exe" : "uv",
};

async function main() {
  const options = parseArguments(process.argv.slice(2));
  if (options.help) {
    printHelp();
    return;
  }

  const environment = buildDevelopmentEnvironment();
  await verifyCommand(
    commands.docker,
    ["compose", "version"],
    "Docker Compose",
  );

  if (options.down) {
    await runForeground(
      commands.docker,
      ["compose", "-f", composeFile, "down"],
      environment,
      "docker",
    );
    return;
  }

  acquireDevelopmentLock();
  await verifyToolchain();
  await assertDevelopmentPortsAvailable(environment);

  const workerMode = options.skipWorker
    ? "disabled"
    : await resolveDevelopmentWorkerMode(environment);
  if (!options.skipInstall) {
    await runForeground(
      commands.pnpm,
      ["install", "--frozen-lockfile"],
      environment,
      "pnpm",
    );
    await runForeground(
      commands.uv,
      ["sync", "--all-packages", "--frozen"],
      environment,
      "uv",
    );
  }

  // Both TypeScript gateways consume the generated Core client. Build it once
  // before starting their file watchers to avoid concurrent predev builds.
  await runForeground(
    commands.pnpm,
    ["--filter", "@mmdash/core-client", "build"],
    environment,
    "core-client",
  );

  if (workerMode === "docker") {
    const pythonIndexUrl = new URL(
      environment.MMDASH_DEV_PYPI_INDEX_URL ||
        "https://mirrors.aliyun.com/pypi/simple/",
    );
    if (
      !["http:", "https:"].includes(pythonIndexUrl.protocol) ||
      pythonIndexUrl.username ||
      pythonIndexUrl.password
    ) {
      throw new Error(
        "MMDASH_DEV_PYPI_INDEX_URL must be an HTTP(S) URL without embedded credentials",
      );
    }
    const configuredProxyValue = environment.MMDASH_DEV_DOCKER_PROXY_URL?.trim();
    const proxyDisabled = configuredProxyValue?.toLowerCase() === "none";
    const localFallbackAvailable =
      !configuredProxyValue && (await hasListeningServer(22_334));
    const dockerProxyUrl = proxyDisabled
      ? undefined
      : configuredProxyValue
        ? dockerAccessibleUrl(configuredProxyValue)
      : localFallbackAvailable
        ? dockerAccessibleUrl(fallbackProxyUrl)
        : undefined;
    if (dockerProxyUrl) {
      const parsedProxyUrl = new URL(dockerProxyUrl);
      if (
        !["http:", "https:"].includes(parsedProxyUrl.protocol) ||
        parsedProxyUrl.username ||
        parsedProxyUrl.password
      ) {
        throw new Error(
          "MMDASH_DEV_DOCKER_PROXY_URL must be an HTTP(S) URL without embedded credentials",
        );
      }
    }
    console.log(
      `[dev] Native Pandoc/LaTeX toolchain is unavailable; preparing the containerized Worker (Python index: ${pythonIndexUrl.href}${dockerProxyUrl ? `, proxy: ${dockerProxyUrl}` : ""})...`,
    );
    const workerBuildContext = prepareWorkerBuildContext();
    try {
      const workerBuildArguments = ["build"];
      if (dockerProxyUrl) {
        workerBuildArguments.push(
          "--build-arg",
          `HTTP_PROXY=${dockerProxyUrl}`,
          "--build-arg",
          `HTTPS_PROXY=${dockerProxyUrl}`,
        );
      }
      workerBuildArguments.push(
        "--build-arg",
        `PYPI_INDEX_URL=${pythonIndexUrl.href}`,
        "--tag",
        "mmdash-worker:dev",
        "--file",
        path.join(
          workerBuildContext,
          "workers",
          "mmdash-worker",
          "Dockerfile",
        ),
        workerBuildContext,
      );
      await runForeground(
        commands.docker,
        workerBuildArguments,
        environment,
        "worker-image",
      );
    } finally {
      removeWorkerBuildContext(workerBuildContext);
    }
  }

  mkdirSync(developmentToolsDirectory, { recursive: true });
  await runForeground(
    commands.go,
    ["build", "-o", repoAskPassPath, "./cmd/mmdash-git-askpass"],
    environment,
    "git-askpass",
    path.join(repositoryRoot, "backend"),
  );
  await runForeground(
    commands.go,
    [
      "build",
      "-trimpath",
      "-ldflags",
      "-X main.version=dev",
      "-o",
      developmentCliPath,
      "./clients/cli/cmd/mmdash",
    ],
    environment,
    "cli",
  );
  await runForeground(
    commands.go,
    [
      "build",
      "-trimpath",
      "-o",
      developmentBoxPath,
      "./box/cmd/mmdash-box",
    ],
    environment,
    "box",
  );
  mkdirSync(developmentDownloadsDirectory, { recursive: true });
  const downloadSuffix = `${process.platform}-${process.arch}${isWindows ? ".exe" : ""}`;
  copyFileSync(
    developmentCliPath,
    path.join(developmentDownloadsDirectory, `mmdash-cli-${downloadSuffix}`),
  );
  copyFileSync(
    developmentBoxPath,
    path.join(developmentDownloadsDirectory, `mmdash-box-${downloadSuffix}`),
  );
  copyFileSync(developmentBoxPath, developmentMboxPath);
  writeDevelopmentCliLauncher(environment);

  console.log("[dev] Starting PostgreSQL and MinIO...");
  await runForeground(
    commands.docker,
    ["compose", "-f", composeFile, "up", "-d", "--wait", "postgres", "minio"],
    environment,
    "docker",
  );

  if (environment.ARTIFACT_STORAGE_BACKEND !== "local") {
    console.log("[dev] Initializing artifact object storage...");
    await runForeground(
      commands.go,
      ["run", "./cmd/artifact-storage-init"],
      environment,
      "artifact-storage-init",
      path.join(repositoryRoot, "backend"),
    );
  }

  console.log("[dev] Applying database migrations...");
  await runForeground(
    commands.go,
    ["run", "./cmd/migrate"],
    environment,
    "migrate",
    path.join(repositoryRoot, "backend"),
  );

  startManaged(
    "core",
    commands.go,
    ["run", "./cmd/core-server"],
    environment,
    path.join(repositoryRoot, "backend"),
  );
  await waitForHttp(
    "Core",
    localServiceUrl(environment.MMDASH_DEV_CORE_PORT, "/health/ready"),
    90_000,
  );

  let workerToken = environment.MMDASH_WORKER_API_TOKEN?.trim();
  if (!options.skipWorker && !workerToken) {
    issuedWorkerCredential = await issueDevelopmentWorkerToken(environment);
    workerToken = issuedWorkerCredential.token;
  }

  startManaged(
    "web-bff",
    commands.pnpm,
    ["--filter", "@mmdash/web-bff", "exec", "tsx", "watch", "src/main.ts"],
    environment,
  );
  startManaged(
    "web",
    commands.pnpm,
    [
      "--filter",
      "@mmdash/web",
      "exec",
      "next",
      "dev",
      "--hostname",
      "0.0.0.0",
      "--port",
      environment.MMDASH_DEV_WEB_PORT,
    ],
    environment,
  );
  startManaged(
    "mcp",
    commands.pnpm,
    ["--filter", "@mmdash/mcp-gateway", "exec", "tsx", "watch", "src/main.ts"],
    environment,
  );

  if (workerMode === "native") {
    startManaged(
      "worker",
      commands.uv,
      ["run", "--package", "mmdash-worker", "mmdash-worker"],
      {
        ...environment,
        MMDASH_WORKER_API_TOKEN: workerToken,
      },
    );
  } else if (workerMode === "docker") {
    developmentWorkerContainerName = `mmdash-development-worker-${process.pid}`;
    const dockerWorkerEnvironment = {
      ...environment,
      MMDASH_CORE_URL: dockerAccessibleUrl(environment.MMDASH_CORE_URL),
      MMDASH_WORKER_API_TOKEN: workerToken,
      MMDASH_WORKER_ID: developmentWorkerContainerName,
      MMDASH_WORKER_TRANSFER_ORIGIN_OVERRIDE: dockerAccessibleUrl(
        environment.OBJECT_STORAGE_PUBLIC_ENDPOINT,
      ),
    };
    startManaged(
      "worker",
      commands.docker,
      [
        "run",
        "--rm",
        "--name",
        developmentWorkerContainerName,
        "--mount",
        `type=bind,source=${path.join(repositoryRoot, "workers", "mmdash-worker", "src")},target=/app/workers/mmdash-worker/src,readonly`,
        "--env",
        "MMDASH_CORE_URL",
        "--env",
        "MMDASH_WORKER_API_TOKEN",
        "--env",
        "MMDASH_WORKER_ID",
        "--env",
        "MMDASH_WORKER_TRANSFER_ORIGIN_OVERRIDE",
        "--env",
        "MMDASH_WORKER_LEASE_SECONDS",
        "--env",
        "MMDASH_WORKER_POLL_SECONDS",
        "--env",
        "MMDASH_PROGRESS_EVALUATOR_MODE",
        "--env",
        "SOURCE_DATE_EPOCH=0",
        "--env",
        "TZ=UTC",
        "--env",
        "TEXMFVAR=/tmp/texmf-var",
        "--env",
        "TEXMFCONFIG=/tmp/texmf-config",
        "--env",
        "TEXMFHOME=/tmp/texmf-home",
        "mmdash-worker:dev",
      ],
      dockerWorkerEnvironment,
    );
  }

  await Promise.all([
    waitForHttp(
      "Web BFF",
      localServiceUrl(environment.MMDASH_DEV_BFF_PORT, "/health/live"),
      90_000,
    ),
    waitForHttp(
      "Web",
      localServiceUrl(environment.MMDASH_DEV_WEB_PORT, "/health/live"),
      120_000,
    ),
    waitForHttp(
      "MCP Gateway",
      localServiceUrl(environment.MMDASH_DEV_MCP_PORT, "/health/live"),
      90_000,
    ),
  ]);

  console.log("");
  console.log("[dev] Development environment is ready:");
  console.log(`[dev]   Web:           ${environment.MMDASH_PUBLIC_URL}`);
  console.log(`[dev]   Web BFF:       ${environment.BFF_INTERNAL_URL}`);
  console.log(`[dev]   Core:          ${environment.MMDASH_CORE_URL}`);
  console.log(
    `[dev]   MCP Gateway:   ${new URL(environment.MMDASH_MCP_URL).origin}`,
  );
  console.log(
    `[dev]   MinIO API:     ${environment.OBJECT_STORAGE_PUBLIC_ENDPOINT}`,
  );
  console.log("[dev]   MinIO Console: http://localhost:9001");
  console.log(`[dev]   CLI binary:     ${developmentCliPath}`);
  console.log(`[dev]   Box binary:     ${developmentBoxPath}`);
  console.log(`[dev]   mbox command:   ${developmentMboxPath}`);
  console.log(`[dev]   Downloads:      ${developmentDownloadsDirectory}`);
  console.log(`[dev]   CLI launcher:   ${developmentCliLauncherPath}`);
  if (
    environment.AUTH_BOOTSTRAP_EMAIL ||
    environment.AUTH_BOOTSTRAP_PASSWORD
  ) {
    console.log(
      "[dev] Bootstrap login uses AUTH_BOOTSTRAP_EMAIL/AUTH_BOOTSTRAP_PASSWORD overrides; development defaults do not fully apply.",
    );
  } else {
    console.log("[dev] Bootstrap login: admin@mmdash.local / mmdash-local-admin");
  }
  console.log("[dev] Press Ctrl+C to stop local application processes.");
  console.log(
    "[dev] PostgreSQL and MinIO stay running; use --down to stop them.",
  );

  if (options.check) {
    console.log("[dev] Startup check passed; stopping application processes.");
    await shutdown(0);
    return;
  }

  // The managed child-process exit handlers and signal handlers own shutdown.
  await new Promise(() => {});
}

function parseArguments(arguments_) {
  const options = {
    check: false,
    down: false,
    help: false,
    skipInstall: false,
    skipWorker: false,
  };
  for (const argument of arguments_) {
    switch (argument) {
      case "--check":
        options.check = true;
        break;
      case "--down":
        options.down = true;
        break;
      case "--help":
      case "-h":
        options.help = true;
        break;
      case "--skip-install":
        options.skipInstall = true;
        break;
      case "--skip-worker":
        options.skipWorker = true;
        break;
      default:
        throw new Error(`Unknown argument: ${argument}`);
    }
  }
  return options;
}

function printHelp() {
  console.log(`Usage: node .localscripts/dev.mjs [options]

Options:
  --check         Start, health-check, and stop application processes
  --skip-install  Do not run pnpm install and uv sync
  --skip-worker   Start the environment without the Python Worker
  --down          Stop and remove the Compose development infrastructure
  -h, --help      Show this help

Optional local port overrides:
  MMDASH_DEV_WEB_PORT  MMDASH_DEV_BFF_PORT
  MMDASH_DEV_MCP_PORT  MMDASH_DEV_CORE_PORT

Optional containerized Worker downloads:
  MMDASH_DEV_PYPI_INDEX_URL     Python package index (default: Aliyun)
  MMDASH_DEV_DOCKER_PROXY_URL   HTTP(S) proxy; auto-detects 127.0.0.1:22334;
                                set to none to disable proxy auto-detection

Optional Local Git development access:
  REPO_LOCAL_ALLOWED_ROOTS      Local provider allowlist (default: repository .tmp)`);
}

function buildDevelopmentEnvironment() {
  const dotEnvPath = path.join(repositoryRoot, ".env");
  const fileEnvironment = existsSync(dotEnvPath)
    ? parseDotEnv(readFileSync(dotEnvPath, "utf8"))
    : {};
  const environment = {
    ...fileEnvironment,
    ...process.env,
  };

  const database = environment.POSTGRES_DB || "mmdash";
  const databaseUser = environment.POSTGRES_USER || "mmdash";
  const databasePassword = environment.POSTGRES_PASSWORD || "mmdash";
  const webPort = developmentPort(environment, "MMDASH_DEV_WEB_PORT", 3000);
  const bffPort = developmentPort(environment, "MMDASH_DEV_BFF_PORT", 3001);
  const mcpPort = developmentPort(environment, "MMDASH_DEV_MCP_PORT", 3002);
  const corePort = developmentPort(environment, "MMDASH_DEV_CORE_PORT", 8080);
  const webUrl =
    environment.MMDASH_DEV_PUBLIC_URL || `http://localhost:${webPort}`;
  const bffUrl = `http://127.0.0.1:${bffPort}`;
  const coreUrl = `http://127.0.0.1:${corePort}`;
  const mcpUrl = `http://127.0.0.1:${mcpPort}/mcp`;

  environment.POSTGRES_DB = database;
  environment.POSTGRES_USER = databaseUser;
  environment.POSTGRES_PASSWORD = databasePassword;
  environment.MINIO_ROOT_USER = environment.MINIO_ROOT_USER || "mmdash";
  environment.MINIO_ROOT_PASSWORD =
    environment.MINIO_ROOT_PASSWORD || "change-me";

  // Compose service names are valid only inside the Compose network. Native
  // application processes always connect through the published host ports.
  environment.DATABASE_URL =
    environment.MMDASH_DEV_DATABASE_URL ||
    `postgres://${encodeURIComponent(databaseUser)}:${encodeURIComponent(
      databasePassword,
    )}@127.0.0.1:5432/${encodeURIComponent(database)}?sslmode=disable`;
  environment.OBJECT_STORAGE_ENDPOINT =
    environment.MMDASH_DEV_OBJECT_STORAGE_ENDPOINT || "http://127.0.0.1:9000";
  environment.OBJECT_STORAGE_PUBLIC_ENDPOINT =
    environment.MMDASH_DEV_OBJECT_STORAGE_PUBLIC_ENDPOINT ||
    "http://localhost:9000";
  environment.OBJECT_STORAGE_ACCESS_KEY =
    environment.OBJECT_STORAGE_ACCESS_KEY || environment.MINIO_ROOT_USER;
  environment.OBJECT_STORAGE_SECRET_KEY =
    environment.OBJECT_STORAGE_SECRET_KEY || environment.MINIO_ROOT_PASSWORD;
  environment.OBJECT_STORAGE_BUCKET =
    environment.OBJECT_STORAGE_BUCKET || "mmdash";
  environment.OBJECT_STORAGE_REGION =
    environment.OBJECT_STORAGE_REGION || "us-east-1";
  environment.ARTIFACT_STORAGE_BACKEND =
    environment.ARTIFACT_STORAGE_BACKEND || "minio";
  environment.ARTIFACT_WEB_ORIGIN =
    environment.MMDASH_DEV_ARTIFACT_WEB_ORIGIN || webUrl;
  environment.CORE_OPENAPI_PATH = path.join(
    repositoryRoot,
    "contracts",
    "openapi",
    "core.yaml",
  );
  environment.MIGRATIONS_DIR = path.join(
    repositoryRoot,
    "backend",
    "migrations",
  );
  environment.REPO_ASKPASS_PATH = repoAskPassPath;
  environment.REPO_LOCAL_ALLOWED_ROOTS =
    environment.REPO_LOCAL_ALLOWED_ROOTS || path.join(repositoryRoot, ".tmp");
  environment.MMDASH_PUBLIC_URL = webUrl;
  environment.MMDASH_URL =
    environment.MMDASH_DEV_CLI_SERVER_URL || webUrl;
  environment.CORE_BASE_URL = coreUrl;
  environment.CORE_INTERNAL_URL = coreUrl;
  environment.MMDASH_CORE_URL =
    environment.MMDASH_DEV_CLI_CORE_URL || coreUrl;
  environment.MMDASH_MCP_URL =
    environment.MMDASH_DEV_CLI_MCP_URL || mcpUrl;
  environment.MMDASH_LOCAL_UNIFIED_PROXY = "true";
  environment.MCP_INTERNAL_URL = new URL(mcpUrl).origin;
  environment.BFF_INTERNAL_URL = bffUrl;
  environment.CORE_ADDR = `:${corePort}`;
  environment.BFF_HOST = "0.0.0.0";
  environment.BFF_PORT = String(bffPort);
  environment.MCP_GATEWAY_HOST = "0.0.0.0";
  environment.MCP_GATEWAY_PORT = String(mcpPort);
  environment.MCP_ALLOWED_HOSTS =
    environment.MMDASH_DEV_MCP_ALLOWED_HOSTS ||
    [...new Set(["localhost", "127.0.0.1", new URL(webUrl).hostname])].join(
      ",",
    );
  environment.MCP_ALLOWED_ORIGINS =
    environment.MMDASH_DEV_MCP_ALLOWED_ORIGINS ||
    [...new Set([webUrl, new URL(mcpUrl).origin])].join(",");
  environment.PORT = String(webPort);
  environment.NODE_ENV = "development";

  return environment;
}

function developmentPort(environment, name, fallback) {
  const value = Number(environment[name] || fallback);
  if (!Number.isInteger(value) || value < 1 || value > 65_535) {
    throw new Error(`${name} must be an integer between 1 and 65535`);
  }
  environment[name] = String(value);
  return value;
}

async function resolveDevelopmentWorkerMode(environment) {
  const configured = (environment.MMDASH_DEV_WORKER_MODE || "auto")
    .trim()
    .toLowerCase();
  if (!["auto", "native", "docker"].includes(configured)) {
    throw new Error(
      "MMDASH_DEV_WORKER_MODE must be auto, native, or docker",
    );
  }
  if (configured !== "auto") {
    return configured;
  }
  for (const command of ["pandoc", "latexmk", "xelatex"]) {
    try {
      await captureCommand(command, ["--version"]);
    } catch {
      return "docker";
    }
  }
  return "native";
}

function dockerAccessibleUrl(value) {
  const url = new URL(value);
  if (url.hostname === "localhost" || url.hostname === "127.0.0.1") {
    url.hostname = "host.docker.internal";
  }
  return url.toString().replace(/\/$/u, "");
}

function prepareWorkerBuildContext() {
  const contextRoot = path.join(
    developmentToolsDirectory,
    `worker-build-${process.pid}`,
  );
  const workerSource = path.join(repositoryRoot, "workers", "mmdash-worker");
  const workerTarget = path.join(contextRoot, "workers", "mmdash-worker");
  mkdirSync(workerTarget, { recursive: true });
  copyFileSync(
    path.join(repositoryRoot, "pyproject.toml"),
    path.join(contextRoot, "pyproject.toml"),
  );
  copyFileSync(
    path.join(repositoryRoot, "uv.lock"),
    path.join(contextRoot, "uv.lock"),
  );
  for (const filename of ["Dockerfile", "pyproject.toml", "README.md"]) {
    copyFileSync(
      path.join(workerSource, filename),
      path.join(workerTarget, filename),
    );
  }
  cpSync(path.join(workerSource, "src"), path.join(workerTarget, "src"), {
    recursive: true,
  });
  return contextRoot;
}

function removeWorkerBuildContext(contextRoot) {
  const expectedParent = `${path.resolve(developmentToolsDirectory)}${path.sep}`;
  const resolved = path.resolve(contextRoot);
  if (!resolved.startsWith(expectedParent)) {
    throw new Error(`Refusing to remove unexpected Worker context: ${resolved}`);
  }
  rmSync(resolved, { force: true, recursive: true });
}

function localServiceUrl(port, pathname = "") {
  return `http://127.0.0.1:${port}${pathname}`;
}

function writeDevelopmentCliLauncher(environment) {
  mkdirSync(developmentCliConfigDirectory, { recursive: true });
  const cliEnvironment = {
    MMDASH_CONFIG_DIR: developmentCliConfigDirectory,
    MMDASH_CORE_URL: environment.MMDASH_CORE_URL,
    MMDASH_MCP_URL: environment.MMDASH_MCP_URL,
    MMDASH_URL: environment.MMDASH_URL,
  };
  if (isWindows) {
    const lines = ["@echo off", "setlocal"];
    for (const [name, value] of Object.entries(cliEnvironment)) {
      lines.push(`set "${name}=${value}"`);
    }
    lines.push(`"${developmentCliPath}" %*`, "exit /b %ERRORLEVEL%", "");
    writeFileSync(developmentCliLauncherPath, lines.join("\r\n"), "utf8");
    return;
  }
  const quote = (value) => `'${value.replaceAll("'", `'"'"'`)}'`;
  const lines = ["#!/usr/bin/env sh"];
  for (const [name, value] of Object.entries(cliEnvironment)) {
    lines.push(`export ${name}=${quote(value)}`);
  }
  lines.push(`exec ${quote(developmentCliPath)} "$@"`, "");
  writeFileSync(developmentCliLauncherPath, lines.join("\n"), "utf8");
  chmodSync(developmentCliLauncherPath, 0o755);
}

function parseDotEnv(contents) {
  const environment = {};
  for (const [index, originalLine] of contents.split(/\r?\n/u).entries()) {
    const line = originalLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }
    const normalized = line.startsWith("export ") ? line.slice(7).trim() : line;
    const separator = normalized.indexOf("=");
    if (separator < 1) {
      throw new Error(`Invalid .env entry on line ${index + 1}`);
    }
    const key = normalized.slice(0, separator).trim();
    let value = normalized.slice(separator + 1).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/u.test(key)) {
      throw new Error(`Invalid .env key on line ${index + 1}: ${key}`);
    }
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    environment[key] = value;
  }
  return environment;
}

async function verifyToolchain() {
  const nodeMajor = Number.parseInt(process.versions.node.split(".", 1)[0], 10);
  if (nodeMajor < 24) {
    throw new Error(
      `Node.js 24 or newer is required; found ${process.versions.node}`,
    );
  }

  const [pnpmVersion, goVersion] = await Promise.all([
    captureCommand(commands.pnpm, ["--version"]),
    captureCommand(commands.go, ["version"]),
    verifyCommand(commands.uv, ["--version"], "uv"),
  ]);
  if (Number.parseInt(pnpmVersion.trim().split(".", 1)[0], 10) < 11) {
    throw new Error(
      `pnpm 11 or newer is required; found ${pnpmVersion.trim()}`,
    );
  }
  const goMatch = /\bgo(\d+)\.(\d+)/u.exec(goVersion);
  if (
    !goMatch ||
    Number(goMatch[1]) < 1 ||
    (Number(goMatch[1]) === 1 && Number(goMatch[2]) < 26)
  ) {
    throw new Error(`Go 1.26 or newer is required; found ${goVersion.trim()}`);
  }
}

async function verifyCommand(command, arguments_, displayName) {
  try {
    await captureCommand(command, arguments_);
  } catch (error) {
    throw new Error(
      `${displayName} is required but is not available: ${error.message}`,
    );
  }
}

function captureCommand(command, arguments_) {
  return new Promise((resolve, reject) => {
    const child = spawnCommand(command, arguments_, {
      cwd: repositoryRoot,
      env: process.env,
      shell: false,
      windowsHide: true,
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.once("error", reject);
    child.once("exit", (code) => {
      if (code === 0) {
        resolve(stdout.trim() || stderr.trim());
      } else {
        reject(
          new Error(
            `${command} ${arguments_.join(" ")} exited with code ${code}: ${
              stderr.trim() || stdout.trim()
            }`,
          ),
        );
      }
    });
  });
}

function runForeground(
  command,
  arguments_,
  environment,
  label,
  workingDirectory = repositoryRoot,
) {
  return runForegroundAttempt(
    command,
    arguments_,
    environment,
    label,
    workingDirectory,
  ).then(async (result) => {
    if (result.code === 0) {
      return;
    }

    if (!connectionFailurePattern.test(result.output)) {
      throw foregroundExitError(label, result.code, result.signal);
    }

    console.warn(
      `[dev] ${label} encountered a connection failure; retrying once via ${fallbackProxyUrl}...`,
    );
    const retryResult = await runForegroundAttempt(
      command,
      arguments_,
      withFallbackProxy(environment),
      label,
      workingDirectory,
    );
    if (retryResult.code !== 0) {
      throw foregroundExitError(label, retryResult.code, retryResult.signal);
    }
  });
}

function runForegroundAttempt(
  command,
  arguments_,
  environment,
  label,
  workingDirectory,
) {
  console.log(`[${label}] ${command} ${arguments_.join(" ")}`);
  return new Promise((resolve, reject) => {
    const child = spawnCommand(command, arguments_, {
      cwd: workingDirectory,
      env: environment,
      shell: false,
      stdio: ["inherit", "pipe", "pipe"],
      windowsHide: false,
    });
    let output = "";
    const forwardOutput = (chunk, destination) => {
      destination.write(chunk);
      output = `${output}${chunk}`.slice(-65_536);
    };
    child.stdout.on("data", (chunk) => forwardOutput(chunk, process.stdout));
    child.stderr.on("data", (chunk) => forwardOutput(chunk, process.stderr));
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      resolve({ code, signal, output });
    });
  });
}

function withFallbackProxy(environment) {
  return {
    ...environment,
    HTTP_PROXY: fallbackProxyUrl,
    HTTPS_PROXY: fallbackProxyUrl,
    ALL_PROXY: fallbackProxyUrl,
    http_proxy: fallbackProxyUrl,
    https_proxy: fallbackProxyUrl,
    all_proxy: fallbackProxyUrl,
  };
}

function foregroundExitError(label, code, signal) {
  return new Error(
    `${label} exited with ${signal ? `signal ${signal}` : `code ${code}`}`,
  );
}

function startManaged(
  label,
  command,
  arguments_,
  environment,
  workingDirectory = repositoryRoot,
) {
  const processEntry = { child: undefined, label };
  managedProcesses.push(processEntry);

  const launch = (launchEnvironment, usingFallbackProxy) => {
    console.log(`[dev] Starting ${label}...`);
    let output = "";
    let spawnFailed = false;
    const child = spawnCommand(command, arguments_, {
      cwd: workingDirectory,
      detached: !isWindows,
      env: launchEnvironment,
      shell: false,
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    });
    processEntry.child = child;
    const captureLine = (line) => {
      output = `${output}${line}\n`.slice(-65_536);
    };
    prefixOutput(child.stdout, label, process.stdout, captureLine);
    prefixOutput(child.stderr, label, process.stderr, captureLine);
    child.once("error", (error) => {
      spawnFailed = true;
      if (!shuttingDown) {
        void fail(`${label} failed to start: ${error.message}`);
      }
    });
    child.once("close", (code, signal) => {
      if (shuttingDown || spawnFailed) {
        return;
      }
      if (
        !usingFallbackProxy &&
        code !== 0 &&
        connectionFailurePattern.test(output)
      ) {
        console.warn(
          `[dev] ${label} encountered a connection failure; retrying once via ${fallbackProxyUrl}...`,
        );
        launch(withFallbackProxy(environment), true);
        return;
      }
      const result = signal ? `signal ${signal}` : `code ${code}`;
      void fail(`${label} stopped unexpectedly with ${result}`);
    });
  };

  launch(environment, false);
}

function spawnCommand(command, arguments_, options) {
  if (isWindows && command.toLowerCase().endsWith(".cmd")) {
    return spawn(
      process.env.ComSpec || "cmd.exe",
      ["/d", "/s", "/c", command, ...arguments_],
      options,
    );
  }
  return spawn(command, arguments_, options);
}

function prefixOutput(stream, label, output, onLine = () => {}) {
  const lines = createInterface({ input: stream });
  lines.on("line", (line) => {
    onLine(line);
    output.write(`[${label}] ${line}\n`);
  });
}

async function waitForHttp(name, url, timeoutMilliseconds) {
  const deadline = Date.now() + timeoutMilliseconds;
  let lastError = "no response";
  while (Date.now() < deadline && !shuttingDown) {
    try {
      const response = await fetch(url, {
        redirect: "manual",
        signal: AbortSignal.timeout(3_000),
      });
      if (response.status >= 200 && response.status < 400) {
        console.log(`[dev] ${name} is ready.`);
        return;
      }
      lastError = `HTTP ${response.status}`;
    } catch (error) {
      lastError = error.message;
    }
    await delay(1_000);
  }
  throw new Error(`${name} did not become ready at ${url}: ${lastError}`);
}

async function assertDevelopmentPortsAvailable(environment) {
  const services = [
    ["Web", Number(environment.MMDASH_DEV_WEB_PORT)],
    ["Web BFF", Number(environment.MMDASH_DEV_BFF_PORT)],
    ["MCP Gateway", Number(environment.MMDASH_DEV_MCP_PORT)],
    ["Core", Number(environment.MMDASH_DEV_CORE_PORT)],
  ];
  const conflicts = [];
  for (const [name, port] of services) {
    try {
      await assertPortAvailable(port);
    } catch (error) {
      if (error.code !== "EADDRINUSE" && error.code !== "EACCES") {
        throw error;
      }
      conflicts.push(`${name} (${port})`);
    }
  }
  if (conflicts.length === 0) {
    return;
  }

  const portList = services.map(([, port]) => port).join(",");
  const inspectionHint = isWindows
    ? `Inspect them with: Get-NetTCPConnection -State Listen -LocalPort ${portList} | Select-Object LocalPort,OwningProcess`
    : `Inspect them with: ${services.map(([, port]) => `lsof -nP -iTCP:${port}`).join(" ")} -sTCP:LISTEN`;
  throw new Error(
    `Development ports are already in use: ${conflicts.join(
      ", ",
    )}. Stop the existing application processes before starting another dev environment.\n${inspectionHint}`,
  );
}

function assertPortAvailable(port) {
  return hasListeningServer(port).then((isListening) => {
    if (isListening) {
      const error = new Error(`Port ${port} already has a listening server`);
      error.code = "EADDRINUSE";
      throw error;
    }
    return new Promise((resolve, reject) => {
      const server = createServer();
      server.unref();
      server.once("error", reject);
      server.listen({ exclusive: true, host: "0.0.0.0", port }, () => {
        server.close(resolve);
      });
    });
  });
}

function hasListeningServer(port) {
  return new Promise((resolve, reject) => {
    let settled = false;
    const finish = (callback, value) => {
      if (settled) {
        return;
      }
      settled = true;
      socket.destroy();
      callback(value);
    };
    const socket = createConnection({ host: "127.0.0.1", port });
    socket.setTimeout(750);
    socket.once("connect", () => finish(resolve, true));
    socket.once("timeout", () => finish(resolve, false));
    socket.once("error", (error) => {
      if (
        error.code === "ECONNREFUSED" ||
        error.code === "EHOSTUNREACH" ||
        error.code === "ETIMEDOUT"
      ) {
        finish(resolve, false);
        return;
      }
      finish(reject, error);
    });
  });
}

function acquireDevelopmentLock() {
  mkdirSync(developmentToolsDirectory, { recursive: true });
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      const descriptor = openSync(developmentLockPath, "wx");
      const token = `${process.pid}:${Date.now()}`;
      writeFileSync(descriptor, token, "utf8");
      developmentLock = { descriptor, token };
      return;
    } catch (error) {
      if (error.code !== "EEXIST") {
        throw error;
      }
      const existingPid = readDevelopmentLockPid();
      if (existingPid && isProcessRunning(existingPid)) {
        throw new Error(
          `Another mmdash development environment is already starting or running (PID ${existingPid}). Stop it before starting a second instance.`,
        );
      }
      try {
        unlinkSync(developmentLockPath);
      } catch (unlinkError) {
        if (unlinkError.code !== "ENOENT") {
          throw unlinkError;
        }
      }
    }
  }
  throw new Error("Could not acquire the mmdash development environment lock");
}

function readDevelopmentLockPid() {
  try {
    const [pidText] = readFileSync(developmentLockPath, "utf8").split(":", 1);
    const pid = Number.parseInt(pidText, 10);
    return Number.isSafeInteger(pid) && pid > 0 ? pid : undefined;
  } catch {
    return undefined;
  }
}

function isProcessRunning(processId) {
  try {
    process.kill(processId, 0);
    return true;
  } catch (error) {
    return error.code === "EPERM";
  }
}

function releaseDevelopmentLock() {
  if (!developmentLock) {
    return;
  }
  const { descriptor, token } = developmentLock;
  developmentLock = undefined;
  try {
    closeSync(descriptor);
  } catch {
    // The descriptor may already have been closed during process teardown.
  }
  try {
    if (readFileSync(developmentLockPath, "utf8") === token) {
      unlinkSync(developmentLockPath);
    }
  } catch {
    // A stale lock is safely recovered by the next invocation.
  }
}

async function issueDevelopmentWorkerToken(environment) {
  const coreUrl = environment.MMDASH_CORE_URL.replace(/\/$/u, "");
  const email = environment.AUTH_BOOTSTRAP_EMAIL || "admin@mmdash.local";
  const configuredPassword =
    environment.AUTH_BOOTSTRAP_PASSWORD || "mmdash-local-admin";
  const passwordCandidates = [
    ...new Set([configuredPassword, "mmdash-local-admin"]),
  ];
  let accessToken;
  let loginError;

  for (const password of passwordCandidates) {
    try {
      const login = await fetchJson(`${coreUrl}/v1/auth/login`, {
        body: { email, password },
        method: "POST",
      });
      accessToken = login.access_token;
      if (accessToken) {
        break;
      }
    } catch (error) {
      loginError = error;
    }
  }
  if (!accessToken) {
    throw new Error(
      `Could not issue a Python Worker token for ${email}: ${
        loginError?.message || "login returned no access token"
      }. Set MMDASH_WORKER_API_TOKEN in .env or use --skip-worker.`,
    );
  }

  const authorization = { Authorization: `Bearer ${accessToken}` };
  const tokenName = "mmdash-local-development-worker";
  const existing = await fetchJson(`${coreUrl}/v1/auth/tokens`, {
    headers: authorization,
    method: "GET",
  });
  for (const credential of existing.items || []) {
    if (
      credential.kind === "api" &&
      credential.name === tokenName &&
      !credential.revoked_at
    ) {
      await fetchJson(
        `${coreUrl}/v1/auth/tokens/${encodeURIComponent(credential.id)}`,
        {
          headers: authorization,
          ignoreNotFound: true,
          method: "DELETE",
        },
      );
    }
  }

  const issued = await fetchJson(`${coreUrl}/v1/auth/tokens`, {
    body: { kind: "api", name: tokenName },
    headers: authorization,
    method: "POST",
  });
  if (!issued.token || !issued.credential?.id) {
    throw new Error("Core returned an invalid Python Worker credential");
  }
  console.log(
    "[dev] Issued a temporary local API token for the Python Worker.",
  );
  return {
    accessToken,
    coreUrl,
    credentialId: issued.credential.id,
    token: issued.token,
  };
}

async function fetchJson(url, options) {
  const headers = {
    Accept: "application/json",
    ...options.headers,
  };
  let body;
  if (options.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(options.body);
  }
  const response = await fetch(url, {
    body,
    headers,
    method: options.method,
    signal: AbortSignal.timeout(10_000),
  });
  const text = await response.text();
  let payload = {};
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      throw new Error(`${options.method} ${url} returned invalid JSON`);
    }
  }
  if (!response.ok) {
    if (response.status === 404 && options.ignoreNotFound) {
      return {};
    }
    throw new Error(
      `${options.method} ${url} returned HTTP ${response.status}: ${
        payload.message || payload.code || response.statusText
      }`,
    );
  }
  return payload;
}

async function revokeIssuedWorkerToken() {
  if (!issuedWorkerCredential) {
    return;
  }
  try {
    await fetchJson(
      `${issuedWorkerCredential.coreUrl}/v1/auth/tokens/${encodeURIComponent(
        issuedWorkerCredential.credentialId,
      )}`,
      {
        headers: {
          Authorization: `Bearer ${issuedWorkerCredential.accessToken}`,
        },
        ignoreNotFound: true,
        method: "DELETE",
      },
    );
    console.log("[dev] Revoked the temporary Python Worker token.");
  } catch (error) {
    console.error(
      `[dev] Could not revoke the temporary Python Worker token: ${error.message}`,
    );
  } finally {
    issuedWorkerCredential = undefined;
  }
}

async function stopManagedProcess({ child, label }) {
  if (!child?.pid) {
    return;
  }
  if (isWindows) {
    console.log(`[dev] Stopping ${label}...`);
    await stopWindowsProcessTree(child.pid);
    return;
  }
  if (child.exitCode !== null) {
    return;
  }
  console.log(`[dev] Stopping ${label}...`);

  try {
    process.kill(-child.pid, "SIGTERM");
  } catch {
    return;
  }
  await Promise.race([
    new Promise((resolve) => child.once("exit", resolve)),
    delay(5_000),
  ]);
  if (child.exitCode === null) {
    try {
      process.kill(-child.pid, "SIGKILL");
    } catch {
      // The process group exited between the check and the signal.
    }
  }
}

async function stopWindowsProcessTree(rootProcessId) {
  // npm/pnpm, `go run`, and framework CLIs may let their launcher exit before
  // the real server process. taskkill cannot find that tree once the launcher
  // is gone, but Windows retains each live child's original parent PID. Take
  // one process snapshot and stop descendants deepest-first, even if the root
  // process has already exited.
  const script = [
    "$ErrorActionPreference='SilentlyContinue'",
    `$rootProcessId=${Number(rootProcessId)}`,
    "$processes=Get-CimInstance Win32_Process",
    "$children=@{}",
    "foreach($process in $processes){$parent=[int]$process.ParentProcessId;if(-not $children.ContainsKey($parent)){$children[$parent]=[System.Collections.Generic.List[int]]::new()};$children[$parent].Add([int]$process.ProcessId)}",
    "$targets=[System.Collections.Generic.List[int]]::new()",
    "$visit={param([int]$parent)if($children.ContainsKey($parent)){foreach($childId in $children[$parent]){& $visit $childId;$targets.Add($childId)}}}",
    "& $visit $rootProcessId",
    "$targets.Add($rootProcessId)",
    "foreach($processId in $targets){Stop-Process -Id $processId -Force -ErrorAction SilentlyContinue}",
  ].join(";");
  await new Promise((resolve) => {
    const killer = spawn(
      "powershell.exe",
      ["-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script],
      {
        stdio: "ignore",
        windowsHide: true,
      },
    );
    killer.once("error", resolve);
    killer.once("exit", resolve);
  });
}

async function shutdown(exitCode = 0) {
  if (shuttingDown) {
    return;
  }
  shuttingDown = true;
  const processesInStopOrder = managedProcesses.toReversed();
  for (const processEntry of processesInStopOrder) {
    if (processEntry.label === "worker") {
      await stopManagedProcess(processEntry);
    }
  }
  await removeDevelopmentWorkerContainer();
  await revokeIssuedWorkerToken();
  for (const processEntry of processesInStopOrder) {
    if (processEntry.label === "worker") {
      continue;
    }
    await stopManagedProcess(processEntry);
  }
  releaseDevelopmentLock();
  process.exit(exitCode);
}

async function removeDevelopmentWorkerContainer() {
  if (!developmentWorkerContainerName) {
    return;
  }
  const name = developmentWorkerContainerName;
  developmentWorkerContainerName = undefined;
  try {
    await captureCommand(commands.docker, ["rm", "--force", name]);
  } catch {
    // `docker run --rm` may already have removed the exact managed container.
  }
}

async function fail(message) {
  console.error(`[dev] ${message}`);
  await shutdown(1);
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

process.once("SIGINT", () => void shutdown(0));
process.once("SIGTERM", () => void shutdown(0));
process.once("exit", releaseDevelopmentLock);

main().catch((error) => void fail(error.stack || error.message));
