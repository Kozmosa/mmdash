import { spawn } from "node:child_process";
import { createWriteStream, existsSync } from "node:fs";
import {
  chmod,
  copyFile,
  mkdir,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import net from "node:net";
import path from "node:path";
import process from "node:process";

const repositoryRoot = path.resolve(import.meta.dirname, "..");
const host = "127.0.0.1";
const pnpmVersion = "11.9.0";
const workerBaseImage = "python:3.12.11-slim-bookworm";
const workerBaseImageMirror =
  "docker.1ms.run/library/python:3.12.11-slim-bookworm";
const workerImage = "mmdash-worker:testenv";
const workerTokenName = "mmdash-pixi-development-worker";
const cloudflareTunnelImage = "cloudflare/cloudflared:latest";

export const serviceOrder = [
  "postgres",
  "minio",
  "core",
  "worker",
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
    localRepositoryRoot: path.join(testenvRoot, "repositories"),
    repositoryRoot: root,
    runtimeBin: path.join(runtimeRoot, "bin"),
    runtimeRoot,
    serviceLogs: path.join(runtimeRoot, "logs"),
    stopRequest: path.join(runtimeRoot, "run", "stop.request"),
    supervisorLock: path.join(runtimeRoot, "run", "supervisor.json"),
    testenvRoot,
    temporaryRoot: path.join(runtimeRoot, "tmp"),
    webDownloads: path.join(root, "apps", "web", "public", "downloads", "dev"),
  };
}

export function parseDotEnv(contents) {
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

export async function loadRepositoryEnvironment(
  root = repositoryRoot,
  environment = process.env,
) {
  const dotEnvPath = path.join(root, ".env");
  const fileEnvironment = existsSync(dotEnvPath)
    ? parseDotEnv(await readFile(dotEnvPath, "utf8"))
    : {};
  return { ...fileEnvironment, ...environment };
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
    GOPROXY: environment.GOPROXY ?? "https://goproxy.cn,direct",
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
  environment = process.env,
  workerMode = "native",
) {
  const databaseUrl = `postgres://mmdash@${host}:${ports.postgres}/mmdash?sslmode=disable`;
  const coreUrl = `http://${host}:${ports.core}`;
  const minioAccessKey = "mmdash";
  const minioSecretKey = "local-minio-secret";
  const minioUrl = `http://${host}:${ports.minio}`;
  const webUrl = `http://${host}:${ports.web}`;
  const publicUrl =
    environment.MMDASH_TESTENV_PUBLIC_URL ??
    environment.MMDASH_PUBLIC_URL ??
    webUrl;
  const mcpUrl = `http://${host}:${ports.mcp}/mcp`;
  const containerAccessRequired = workerMode === "docker";
  const coreBindHost = containerAccessRequired ? "0.0.0.0" : host;
  const minioBindHost = containerAccessRequired ? "0.0.0.0" : host;
  const askPassBinary = `mmdash-git-askpass${
    process.platform === "win32" ? ".exe" : ""
  }`;
  const cliBinary = `mmdash${process.platform === "win32" ? ".exe" : ""}`;
  const boxBinary = `mmdash-box${process.platform === "win32" ? ".exe" : ""}`;
  const mboxBinary = `mbox${process.platform === "win32" ? ".exe" : ""}`;
  return {
    boxPath: path.join(layout.runtimeBin, boxBinary),
    cliConfigDirectory: path.join(layout.runtimeRoot, "cli-config"),
    cliLauncherPath: path.join(
      layout.runtimeBin,
      `mmdash-local${process.platform === "win32" ? ".cmd" : ".sh"}`,
    ),
    cliPath: path.join(layout.runtimeBin, cliBinary),
    coreBindHost,
    databaseUrl,
    coreUrl,
    mboxPath: path.join(layout.runtimeBin, mboxBinary),
    mcpUrl,
    minioBindHost,
    minioUrl,
    ports,
    publicUrl,
    webUrl,
    environments: {
      core: {
        ARTIFACT_STORAGE_BACKEND: "minio",
        AGENT_MCP_GATEWAY_URL: environment.AGENT_MCP_GATEWAY_URL ?? mcpUrl,
        ARTIFACT_WEB_ORIGIN:
          environment.MMDASH_TESTENV_ARTIFACT_WEB_ORIGIN ?? publicUrl,
        CORE_ADDR: `${coreBindHost}:${ports.core}`,
        CORE_INTERNAL_URL: coreUrl,
        CORE_OPENAPI_PATH: path.join(
          layout.repositoryRoot,
          "contracts",
          "openapi",
          "core.yaml",
        ),
        DATABASE_URL: databaseUrl,
        MMDASH_PUBLIC_URL: publicUrl,
        NOTION_OAUTH_REDIRECT_URI:
          environment.NOTION_OAUTH_REDIRECT_URI ??
          `${publicUrl.replace(/\/$/u, "")}/api/integrations/notion/oauth/callback`,
        NOTIFICATION_WEBHOOK_ALLOW_HTTP_LOOPBACK: "true",
        OBJECT_STORAGE_ACCESS_KEY: minioAccessKey,
        OBJECT_STORAGE_BUCKET: "mmdash",
        OBJECT_STORAGE_ENDPOINT: minioUrl,
        OBJECT_STORAGE_PUBLIC_ENDPOINT: minioUrl,
        OBJECT_STORAGE_REGION: "us-east-1",
        OBJECT_STORAGE_SECRET_KEY: minioSecretKey,
        REPO_ASKPASS_PATH: path.join(layout.runtimeBin, askPassBinary),
        REPO_LOCAL_ALLOWED_ROOTS:
          environment.REPO_LOCAL_ALLOWED_ROOTS ?? layout.localRepositoryRoot,
        REPO_GITHUB_NO_PROXY:
          environment.REPO_GITHUB_NO_PROXY ?? "localhost,127.0.0.1,::1",
        REPO_GITHUB_PROXY_URL: environment.REPO_GITHUB_PROXY_URL ?? "",
      },
      mcp: {
        CORE_BASE_URL: coreUrl,
        MCP_AGENT_PROJECTS: "*",
        MCP_AGENT_TOKEN: "local-agent-token-change-before-production",
        MCP_AGENT_TOOLS: "*",
        MCP_ALLOWED_HOSTS: `localhost,${host}`,
        MCP_ALLOWED_ORIGINS: [
          ...new Set([webUrl, publicUrl, `http://${host}:${ports.mcp}`]),
        ].join(","),
        MCP_CLI_PROJECTS: "*",
        MCP_CLI_TOKEN: "local-cli-token-change-before-production",
        MCP_CLI_TOOLS: "*",
        MCP_GATEWAY_HOST: host,
        MCP_GATEWAY_PORT: String(ports.mcp),
      },
      minio: {
        MINIO_ROOT_PASSWORD: minioSecretKey,
        MINIO_ROOT_USER: minioAccessKey,
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
      worker: {
        MMDASH_CORE_URL: coreUrl,
        MMDASH_PROGRESS_EVALUATOR_MODE:
          environment.MMDASH_PROGRESS_EVALUATOR_MODE ?? "core_agent",
        MMDASH_WORKER_ID: `mmdash-pixi-worker-${process.pid}`,
        MMDASH_WORKER_LEASE_SECONDS:
          environment.MMDASH_WORKER_LEASE_SECONDS ?? "60",
        MMDASH_WORKER_POLL_SECONDS:
          environment.MMDASH_WORKER_POLL_SECONDS ?? "2",
      },
    },
  };
}

async function ensureDirectories(layout) {
  const directories = [
    layout.cacheRoot,
    layout.minioCerts,
    layout.minioData,
    layout.localRepositoryRoot,
    layout.postgresSocket,
    layout.runtimeRoot,
    layout.runtimeBin,
    layout.serviceLogs,
    layout.temporaryRoot,
  ];
  for (const directory of directories) {
    assertPathWithin(layout.testenvRoot, directory);
    await mkdir(directory, { recursive: true });
  }
  assertPathWithin(layout.repositoryRoot, layout.webDownloads);
  await mkdir(layout.webDownloads, { recursive: true });
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

async function commandAvailable(command, arguments_, environment) {
  try {
    const result = await execute(command, arguments_, {
      allowFailure: true,
      capture: true,
      environment,
    });
    return result.code === 0;
  } catch {
    return false;
  }
}

export async function resolveWorkerMode(
  environment = process.env,
  probe = async (command, arguments_) =>
    await commandAvailable(command, arguments_, environment),
) {
  const configured = (
    environment.MMDASH_TESTENV_WORKER_MODE ??
    environment.MMDASH_DEV_WORKER_MODE ??
    "auto"
  )
    .trim()
    .toLowerCase();
  if (!["auto", "native", "docker", "disabled"].includes(configured)) {
    throw new Error(
      "MMDASH_TESTENV_WORKER_MODE must be auto, native, docker, or disabled",
    );
  }
  if (configured !== "auto") {
    return configured;
  }
  for (const command of ["pandoc", "latexmk", "xelatex"]) {
    if (!(await probe(command, ["--version"]))) {
      return "docker";
    }
  }
  return "native";
}

export function dockerAccessibleUrl(value) {
  const url = new URL(value);
  if (url.hostname === "localhost" || url.hostname === host) {
    url.hostname = "host.docker.internal";
  }
  return url.toString().replace(/\/$/u, "");
}

export function parseDevelopmentArguments(arguments_ = []) {
  const unknown = arguments_.filter((argument) => argument !== "--cf");
  if (unknown.length > 0) {
    throw new Error(
      `Unknown development option '${unknown[0]}'. Expected --cf when starting a Cloudflare Quick Tunnel.`,
    );
  }
  return { cloudflareTunnel: arguments_.includes("--cf") };
}

export function cloudflareTunnelArguments(containerName, webUrl) {
  return [
    "run",
    "--rm",
    "--name",
    containerName,
    "--add-host",
    "host.docker.internal:host-gateway",
    cloudflareTunnelImage,
    "tunnel",
    "--no-autoupdate",
    "--url",
    dockerAccessibleUrl(webUrl),
  ];
}

function validatedHttpUrl(value, name) {
  const url = new URL(value);
  if (
    !["http:", "https:"].includes(url.protocol) ||
    url.username ||
    url.password
  ) {
    throw new Error(`${name} must be an HTTP(S) URL without credentials`);
  }
  return url;
}

async function prepareDockerWorkerImage(layout, environment) {
  const dockerReady = await commandAvailable(
    "docker",
    ["version", "--format", "{{.Server.Version}}"],
    environment,
  );
  if (!dockerReady) {
    throw new Error(
      "The complete Pixi environment needs either native pandoc/latexmk/xelatex or a running Docker daemon. Start Docker Desktop, set MMDASH_TESTENV_WORKER_MODE=native after installing the native toolchain, or explicitly use disabled for a base-only environment.",
    );
  }

  const configuredBaseImage =
    environment.MMDASH_TESTENV_WORKER_BASE_IMAGE ?? workerBaseImage;
  const localBaseImage = await commandAvailable(
    "docker",
    ["image", "inspect", configuredBaseImage],
    environment,
  );
  if (!localBaseImage) {
    const officialPull = await execute(
      "docker",
      ["pull", configuredBaseImage],
      {
        allowFailure: true,
        capture: true,
        environment,
      },
    );
    if (officialPull.code !== 0) {
      if (configuredBaseImage !== workerBaseImage) {
        throw new Error(
          `Could not pull configured Worker base image ${configuredBaseImage}: ${officialPull.stderr.trim() || officialPull.stdout.trim()}`,
        );
      }
      const mirror =
        environment.MMDASH_TESTENV_WORKER_BASE_IMAGE_MIRROR ??
        workerBaseImageMirror;
      console.warn(
        `Could not pull ${workerBaseImage}; retrying the pinned image through ${mirror}.`,
      );
      await execute("docker", ["pull", mirror], { environment });
      await execute("docker", ["tag", mirror, workerBaseImage], {
        environment,
      });
    }
  }

  const pythonIndexUrl = validatedHttpUrl(
    environment.MMDASH_TESTENV_PYPI_INDEX_URL ??
      environment.MMDASH_DEV_PYPI_INDEX_URL ??
      "https://mirrors.aliyun.com/pypi/simple/",
    "MMDASH_TESTENV_PYPI_INDEX_URL",
  );
  const debianMirror = validatedHttpUrl(
    environment.MMDASH_TESTENV_DEBIAN_MIRROR ??
      "https://mirrors.aliyun.com/debian",
    "MMDASH_TESTENV_DEBIAN_MIRROR",
  );
  const debianSecurityMirror = validatedHttpUrl(
    environment.MMDASH_TESTENV_DEBIAN_SECURITY_MIRROR ??
      "https://mirrors.aliyun.com/debian-security",
    "MMDASH_TESTENV_DEBIAN_SECURITY_MIRROR",
  );
  const configuredProxy = (
    environment.MMDASH_TESTENV_DOCKER_PROXY_URL ?? ""
  ).trim();
  const proxyDisabled = configuredProxy.toLowerCase() === "none";
  const proxyUrl = proxyDisabled
    ? undefined
    : configuredProxy
      ? dockerAccessibleUrl(configuredProxy)
      : undefined;
  if (proxyUrl) {
    validatedHttpUrl(proxyUrl, "MMDASH_TESTENV_DOCKER_PROXY_URL");
  }

  console.log(
    `Preparing Docker Worker ${workerImage} (Debian mirror: ${debianMirror.href}, Python index: ${pythonIndexUrl.href}${proxyUrl ? `, proxy: ${proxyUrl}` : ""})`,
  );
  const arguments_ = ["build"];
  if (proxyUrl) {
    arguments_.push(
      "--build-arg",
      `HTTP_PROXY=${proxyUrl}`,
      "--build-arg",
      `HTTPS_PROXY=${proxyUrl}`,
    );
  }
  arguments_.push(
    "--build-arg",
    `PYTHON_BASE_IMAGE=${configuredBaseImage}`,
    "--build-arg",
    `DEBIAN_MIRROR=${debianMirror.href.replace(/\/$/, "")}`,
    "--build-arg",
    `DEBIAN_SECURITY_MIRROR=${debianSecurityMirror.href.replace(/\/$/, "")}`,
    "--build-arg",
    `PYPI_INDEX_URL=${pythonIndexUrl.href}`,
    "--tag",
    workerImage,
    "--file",
    path.join(layout.repositoryRoot, "workers", "mmdash-worker", "Dockerfile"),
    layout.repositoryRoot,
  );
  await execute("docker", arguments_, { environment });
}

async function ensureCloudflareTunnelDocker(environment) {
  const dockerReady = await commandAvailable(
    "docker",
    ["version", "--format", "{{.Server.Version}}"],
    environment,
  );
  if (!dockerReady) {
    throw new Error(
      "The --cf development option requires a running Docker daemon. Start Docker Desktop and rerun '.\\scripts\\testenv.ps1 dev --cf'.",
    );
  }
}

async function writeDevelopmentCliLauncher(configuration) {
  await mkdir(configuration.cliConfigDirectory, { recursive: true });
  const cliEnvironment = {
    MMDASH_CONFIG_DIR: configuration.cliConfigDirectory,
    MMDASH_CORE_URL: configuration.coreUrl,
    MMDASH_MCP_URL: configuration.mcpUrl,
    MMDASH_URL: configuration.publicUrl,
  };
  if (process.platform === "win32") {
    const lines = ["@echo off", "setlocal"];
    for (const [name, value] of Object.entries(cliEnvironment)) {
      lines.push(`set "${name}=${value}"`);
    }
    lines.push(`"${configuration.cliPath}" %*`, "exit /b %ERRORLEVEL%", "");
    await writeFile(configuration.cliLauncherPath, lines.join("\r\n"), "utf8");
    return;
  }
  const quote = (value) => `'${value.replaceAll("'", `'"'"'`)}'`;
  const lines = ["#!/usr/bin/env sh"];
  for (const [name, value] of Object.entries(cliEnvironment)) {
    lines.push(`export ${name}=${quote(value)}`);
  }
  lines.push(`exec ${quote(configuration.cliPath)} "$@"`, "");
  await writeFile(configuration.cliLauncherPath, lines.join("\n"), "utf8");
  await chmod(configuration.cliLauncherPath, 0o755);
}

async function buildDevelopmentArtifacts(layout, configuration, environment) {
  for (const target of [
    configuration.cliPath,
    configuration.boxPath,
    configuration.mboxPath,
    configuration.cliLauncherPath,
  ]) {
    assertPathWithin(layout.testenvRoot, target);
  }
  await execute(
    "go",
    [
      "build",
      "-trimpath",
      "-ldflags",
      "-X main.version=dev",
      "-o",
      configuration.cliPath,
      "./clients/cli/cmd/mmdash",
    ],
    { environment },
  );
  await execute(
    "go",
    ["build", "-trimpath", "-o", configuration.boxPath, "./box/cmd/mmdash-box"],
    { environment },
  );
  await copyFile(configuration.boxPath, configuration.mboxPath);
  const suffix = `${process.platform}-${process.arch}${
    process.platform === "win32" ? ".exe" : ""
  }`;
  await copyFile(
    configuration.cliPath,
    path.join(layout.webDownloads, `mmdash-cli-${suffix}`),
  );
  await copyFile(
    configuration.boxPath,
    path.join(layout.webDownloads, `mmdash-box-${suffix}`),
  );
  await writeDevelopmentCliLauncher(configuration);
}

async function fetchJson(url, options) {
  const headers = { Accept: "application/json", ...options.headers };
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

async function issueDevelopmentWorkerToken(configuration, environment) {
  const email = environment.AUTH_BOOTSTRAP_EMAIL ?? "admin@mmdash.local";
  const configuredPassword =
    environment.AUTH_BOOTSTRAP_PASSWORD ?? "mmdash-local-admin";
  const passwordCandidates = [
    ...new Set([configuredPassword, "mmdash-local-admin"]),
  ];
  let accessToken;
  let loginError;
  for (const password of passwordCandidates) {
    try {
      const login = await fetchJson(`${configuration.coreUrl}/v1/auth/login`, {
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
      `Could not issue a Worker token for ${email}: ${loginError?.message ?? "login returned no access token"}. Set MMDASH_WORKER_API_TOKEN or fix the bootstrap credentials.`,
    );
  }
  const headers = { Authorization: `Bearer ${accessToken}` };
  const existing = await fetchJson(`${configuration.coreUrl}/v1/auth/tokens`, {
    headers,
    method: "GET",
  });
  for (const credential of existing.items ?? []) {
    if (
      credential.kind === "api" &&
      credential.name === workerTokenName &&
      !credential.revoked_at
    ) {
      await fetchJson(
        `${configuration.coreUrl}/v1/auth/tokens/${encodeURIComponent(credential.id)}`,
        { headers, ignoreNotFound: true, method: "DELETE" },
      );
    }
  }
  const issued = await fetchJson(`${configuration.coreUrl}/v1/auth/tokens`, {
    body: { kind: "api", name: workerTokenName },
    headers,
    method: "POST",
  });
  if (!issued.token || !issued.credential?.id) {
    throw new Error("Core returned an invalid Worker credential");
  }
  console.log("Issued a temporary API token for the Pixi Worker.");
  return {
    accessToken,
    coreUrl: configuration.coreUrl,
    credentialId: issued.credential.id,
    token: issued.token,
  };
}

async function revokeDevelopmentWorkerToken(credential) {
  if (!credential) {
    return;
  }
  try {
    await fetchJson(
      `${credential.coreUrl}/v1/auth/tokens/${encodeURIComponent(credential.credentialId)}`,
      {
        headers: { Authorization: `Bearer ${credential.accessToken}` },
        ignoreNotFound: true,
        method: "DELETE",
      },
    );
    console.log("Revoked the temporary Pixi Worker token.");
  } catch (error) {
    console.error(`Could not revoke the Pixi Worker token: ${error.message}`);
  }
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

function pipeServiceOutput(name, stream, destination, log, onLine) {
  let pending = "";
  stream?.on("data", (chunk) => {
    const text = chunk.toString();
    log.write(text);
    pending += text;
    const lines = pending.split(/\r?\n/);
    pending = lines.pop() ?? "";
    for (const line of lines) {
      onLine?.(line);
      destination.write(`[${name}] ${line}\n`);
    }
  });
  stream?.once("end", () => {
    if (pending) {
      onLine?.(pending);
      destination.write(`[${name}] ${pending}\n`);
    }
  });
}

function startManagedProcess(
  name,
  command,
  arguments_,
  { cwd = repositoryRoot, environment = process.env, layout, onLine },
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
  pipeServiceOutput(name, child.stdout, process.stdout, log, onLine);
  pipeServiceOutput(name, child.stderr, process.stderr, log, onLine);
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

async function waitForProcessStable(
  service,
  shutdownRequested,
  timeoutMilliseconds = 1_500,
) {
  const deadline = Date.now() + timeoutMilliseconds;
  while (Date.now() < deadline) {
    if (shutdownRequested()) {
      throw new Error(`Interrupted while waiting for ${service.name}`);
    }
    if (service.child.exitCode !== null || service.child.signalCode !== null) {
      throw new Error(`${service.name} exited during startup`);
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
}

async function waitForCloudflareTunnelUrl(
  service,
  getUrl,
  shutdownRequested,
) {
  await waitForCondition(
    "the Cloudflare Quick Tunnel URL",
    async () => Boolean(getUrl()),
    service,
    shutdownRequested,
    60_000,
  );
  return getUrl();
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

async function stopService(service, layout, environment) {
  if (service.name === "postgres") {
    await execute(
      "pg_ctl",
      ["stop", "-D", layout.postgresData, "-m", "fast", "-w", "-t", "10"],
      { allowFailure: true, capture: true, environment },
    );
    await waitForExit(service, 10_000);
    return;
  }
  await stopProcessTree(service, environment);
}

async function stopServices(services, layout, environment) {
  for (const service of [...services].reverse()) {
    await stopService(service, layout, environment);
  }
}

async function removeDockerContainer(name, environment) {
  if (!name) {
    return;
  }
  await execute("docker", ["rm", "--force", name], {
    allowFailure: true,
    capture: true,
    environment,
  });
}

function dockerWorkerEnvironment(configuration, environment, token, name) {
  return {
    ...environment,
    ...configuration.environments.worker,
    MMDASH_CORE_URL: dockerAccessibleUrl(configuration.coreUrl),
    MMDASH_WORKER_API_TOKEN: token,
    MMDASH_WORKER_ID: name,
    MMDASH_WORKER_TRANSFER_ORIGIN_OVERRIDE: dockerAccessibleUrl(
      configuration.coreUrl,
    ),
    SOURCE_DATE_EPOCH: "0",
    TEXMFCONFIG: "/tmp/texmf-config",
    TEXMFHOME: "/tmp/texmf-home",
    TEXMFVAR: "/tmp/texmf-var",
    TZ: "UTC",
  };
}

function dockerWorkerArguments(layout, workerEnvironment, containerName) {
  const forwardedNames = [
    "MMDASH_CORE_URL",
    "MMDASH_WORKER_API_TOKEN",
    "MMDASH_WORKER_ID",
    "MMDASH_WORKER_TRANSFER_ORIGIN_OVERRIDE",
    "MMDASH_WORKER_LEASE_SECONDS",
    "MMDASH_WORKER_POLL_SECONDS",
    "MMDASH_WORKER_MODEL_EXPORT_TIMEOUT_SECONDS",
    "MMDASH_WORKER_MODEL_COMPLETION_TIMEOUT_SECONDS",
    "MMDASH_WORKER_PROGRESS_EVALUATION_TIMEOUT_SECONDS",
    "MMDASH_WORKER_EXPERIMENT_RESULT_TIMEOUT_SECONDS",
    "MMDASH_PROGRESS_EVALUATOR_MODE",
    "MMDASH_PREVIEW_MAX_INPUT_BYTES",
    "MMDASH_PREVIEW_MAX_IMAGE_PIXELS",
    "MMDASH_PREVIEW_MAX_PDF_PAGES",
    "MMDASH_PREVIEW_MAX_PDF_TEXT_PAGES",
    "MMDASH_PREVIEW_MAX_CSV_ROWS",
    "MMDASH_PREVIEW_MAX_CSV_COLUMNS",
    "MMDASH_PREVIEW_MAX_SAMPLE_ROWS",
    "MMDASH_PREVIEW_MAX_JSON_BYTES",
    "MMDASH_PREVIEW_MAX_TEXT_BYTES",
    "MMDASH_PREVIEW_MAX_TEXT_CHARS",
    "MMDASH_PREVIEW_MAX_SUMMARY_BYTES",
    "MMDASH_PREVIEW_MAX_THUMBNAIL_BYTES",
    "MMDASH_PREVIEW_THUMBNAIL_DIMENSION",
    "MMDASH_PREVIEW_TIMEOUT_SECONDS",
    "SOURCE_DATE_EPOCH",
    "TZ",
    "TEXMFVAR",
    "TEXMFCONFIG",
    "TEXMFHOME",
  ].filter((name) => workerEnvironment[name] !== undefined);
  const arguments_ = [
    "run",
    "--rm",
    "--name",
    containerName,
    "--add-host",
    "host.docker.internal:host-gateway",
    "--mount",
    `type=bind,source=${path.join(layout.repositoryRoot, "workers", "mmdash-worker", "src")},target=/app/workers/mmdash-worker/src,readonly`,
  ];
  for (const name of forwardedNames) {
    arguments_.push("--env", name);
  }
  arguments_.push(workerImage);
  return arguments_;
}

async function startDevelopmentEnvironment(
  layout,
  environment,
  ports,
  { cloudflareTunnel = false, startupCheck = false } = {},
) {
  await ensureDirectories(layout);
  await assertPortsAvailable(ports);
  await acquireSupervisorLock(layout);

  const services = [];
  let dockerWorkerContainer;
  let cloudflareTunnelContainer;
  let workerCredential;
  let interrupted = false;
  const shutdownRequested = () => interrupted || existsSync(layout.stopRequest);
  const requestShutdown = () => {
    interrupted = true;
  };
  process.once("SIGINT", requestShutdown);
  process.once("SIGTERM", requestShutdown);

  try {
    if (cloudflareTunnel) {
      await ensureCloudflareTunnelDocker(environment);
      cloudflareTunnelContainer = `mmdash-pixi-cloudflared-${process.pid}`;
      let discoveredTunnelUrl;
      const tunnel = startManagedProcess(
        "cloudflared",
        "docker",
        cloudflareTunnelArguments(
          cloudflareTunnelContainer,
          `http://${host}:${ports.web}`,
        ),
        {
          environment,
          layout,
          onLine: (line) => {
            discoveredTunnelUrl ??= line.match(
              /https:\/\/[a-z0-9-]+\.trycloudflare\.com/iu,
            )?.[0];
          },
        },
      );
      services.push(tunnel);
      const publicUrl = await waitForCloudflareTunnelUrl(
        tunnel,
        () => discoveredTunnelUrl,
        shutdownRequested,
      );
      environment = {
        ...environment,
        MMDASH_TESTENV_PUBLIC_URL: publicUrl,
      };
      console.log(
        `Cloudflare Quick Tunnel is running at ${publicUrl}; using it as MMDASH_TESTENV_PUBLIC_URL.`,
      );
    }

    const workerMode = await resolveWorkerMode(environment);
    const configuration = createServiceConfiguration(
      ports,
      layout,
      environment,
      workerMode,
    );
    if (workerMode === "docker") {
      await prepareDockerWorkerImage(layout, environment);
    }
    await buildDevelopmentArtifacts(layout, configuration, environment);

    const askPassPath = configuration.environments.core.REPO_ASKPASS_PATH;
    if (!existsSync(askPassPath)) {
      await execute(
        "go",
        ["build", "-o", askPassPath, "./backend/cmd/mmdash-git-askpass"],
        { environment },
      );
    }

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
        `${configuration.minioBindHost}:${ports.minio}`,
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
    await execute("go", ["run", "./backend/cmd/artifact-storage-init"], {
      environment: {
        ...environment,
        ...configuration.environments.core,
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

    if (workerMode !== "disabled") {
      const configuredToken = environment.MMDASH_WORKER_API_TOKEN?.trim();
      workerCredential = configuredToken
        ? undefined
        : await issueDevelopmentWorkerToken(configuration, environment);
      const workerToken = configuredToken || workerCredential.token;
      let worker;
      if (workerMode === "docker") {
        dockerWorkerContainer = `mmdash-pixi-worker-${process.pid}`;
        const workerEnvironment = dockerWorkerEnvironment(
          configuration,
          environment,
          workerToken,
          dockerWorkerContainer,
        );
        worker = startManagedProcess(
          "worker",
          "docker",
          dockerWorkerArguments(
            layout,
            workerEnvironment,
            dockerWorkerContainer,
          ),
          { environment: workerEnvironment, layout },
        );
      } else {
        worker = startManagedProcess(
          "worker",
          "uv",
          ["run", "--offline", "--package", "mmdash-worker", "mmdash-worker"],
          {
            environment: {
              ...environment,
              ...configuration.environments.worker,
              MMDASH_WORKER_API_TOKEN: workerToken,
            },
            layout,
          },
        );
      }
      services.push(worker);
      await waitForProcessStable(worker, shutdownRequested);
    } else {
      console.warn(
        "Worker is disabled; Article, preview, Model sync, Progress evaluation, semantic-description, and Experiment result Jobs will remain queued.",
      );
    }

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

    const webArguments = [
      "--filter",
      "@mmdash/web",
      "exec",
      "next",
      "dev",
      "--hostname",
      cloudflareTunnel ? "0.0.0.0" : host,
      "--port",
      String(ports.web),
    ];
    if (environment.MMDASH_TESTENV_WEB_WEBPACK === "1") {
      // Escape hatch for hosts where the default Turbopack dev server
      // crashes natively (seen as next exiting with 0xC0000409 on some
      // Windows machines): fall back to the webpack dev server.
      webArguments.push("--webpack");
    }
    const web = startManagedProcess("web", "pnpm", webArguments, {
      environment: { ...environment, ...configuration.environments.web },
      layout,
    });
    services.push(web);
    await waitForHttp(
      `${configuration.webUrl}/projects`,
      web,
      shutdownRequested,
    );

    console.log(
      `Isolated development environment is ready at ${configuration.webUrl}`,
    );
    console.log(`Worker mode: ${workerMode}`);
    console.log(`CLI launcher: ${configuration.cliLauncherPath}`);
    console.log(`Box binary: ${configuration.boxPath}`);
    console.log(`Downloads: ${layout.webDownloads}`);
    console.log(
      `Run the smoke check in another terminal with ${
        process.platform === "win32"
          ? ".\\scripts\\testenv.ps1 smoke"
          : "./scripts/testenv.sh smoke"
      }`,
    );

    if (startupCheck) {
      console.log("Startup check passed; stopping the isolated environment.");
      return;
    }

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
    const cloudflareTunnelIndex = services.findIndex(
      (service) => service.name === "cloudflared",
    );
    if (cloudflareTunnelIndex >= 0) {
      const [tunnel] = services.splice(cloudflareTunnelIndex, 1);
      await stopService(tunnel, layout, environment);
    }
    await removeDockerContainer(cloudflareTunnelContainer, environment);
    const workerIndex = services.findIndex(
      (service) => service.name === "worker",
    );
    if (workerIndex >= 0) {
      const [worker] = services.splice(workerIndex, 1);
      await stopService(worker, layout, environment);
    }
    await removeDockerContainer(dockerWorkerContainer, environment);
    await revokeDevelopmentWorkerToken(workerCredential);
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
  const workerMode = await resolveWorkerMode(environment);
  console.log(`worker mode: ${workerMode}`);
  if (workerMode === "docker") {
    console.log(
      `docker daemon: ${
        (await commandAvailable(
          "docker",
          ["version", "--format", "{{.Server.Version}}"],
          environment,
        ))
          ? "available"
          : "unavailable"
      }`,
    );
  }
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
  const commandArguments = process.argv.slice(3);
  const layout = createLayout();
  const repositoryEnvironment = await loadRepositoryEnvironment();
  const environment = createIsolatedEnvironment(layout, repositoryEnvironment);
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
      await startDevelopmentEnvironment(layout, environment, ports, {
        ...parseDevelopmentArguments(commandArguments),
      });
      break;
    case "dev-check":
      await startDevelopmentEnvironment(layout, environment, ports, {
        startupCheck: true,
      });
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
        `Unknown test environment command '${command}'. Expected install, test, check, doctor, dev, dev-check, smoke, stop, or reset.`,
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
