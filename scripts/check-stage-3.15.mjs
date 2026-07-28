import { spawnSync } from "node:child_process";
import { access, readFile } from "node:fs/promises";
import { resolve } from "node:path";

import yaml from "js-yaml";

const requiredDirectories = [
  "apps",
  "backend",
  "box",
  "clients",
  "contracts",
  "deploy",
  "docs",
  "packages",
  "workers",
];
await Promise.all(
  requiredDirectories.map(async (directory) => {
    await access(directory).catch(() => {
      throw new Error(`Stage 3.15 is missing top-level directory: ${directory}`);
    });
  }),
);

const compose = yaml.load(
  await readFile("deploy/compose/compose.yaml", "utf8"),
);
for (const service of [
  "postgres",
  "minio",
  "migrate",
  "core",
  "web-bff",
  "web",
  "mcp-gateway",
  "worker",
]) {
  if (!compose?.services?.[service]) {
    throw new Error(`Stage 3.15 compose is missing service: ${service}`);
  }
}

const nodeVersion = (await readFile(".node-version", "utf8")).trim();
if (nodeVersion !== "24") {
  throw new Error(`CI/runtime Node version must be 24, got ${nodeVersion}`);
}
const workflow = await readFile(".github/workflows/ci.yml", "utf8");
if (!workflow.includes("node-version-file: .node-version")) {
  throw new Error("CI must consume the repository Node version.");
}

const cli = spawnSync(
  process.execPath,
  ["clients/cli/dist/main.js", "--version"],
  { encoding: "utf8" },
);
if (cli.status !== 0 || cli.stdout.trim() === "") {
  throw new Error(`CLI shell failed:\n${cli.stdout}\n${cli.stderr}`);
}

const worker = spawnSync(
  process.platform === "win32" ? "uv.exe" : "uv",
  [
    "run",
    "--offline",
    "--package",
    "mmdash-worker",
    "mmdash-worker",
    "--status",
  ],
  {
    encoding: "utf8",
    env: {
      ...process.env,
      UV_CACHE_DIR: process.env.UV_CACHE_DIR ?? resolve(".uv-cache"),
    },
  },
);
if (worker.status !== 0) {
  throw new Error(`Worker shell failed:\n${worker.stdout}\n${worker.stderr}`);
}
const workerStatus = JSON.parse(worker.stdout.trim());
if (
  workerStatus.service !== "mmdash-worker" ||
  workerStatus.status !== "ready"
) {
  throw new Error(`Unexpected Worker status: ${worker.stdout}`);
}

const smoke = await readFile("scripts/smoke.mjs", "utf8");
for (const requiredEvidence of [
  "/api/auth/login",
  "/api/projects",
  "/v1/jobs",
  "/v1/events/test",
  "/v1/data/projects/",
  "/v1/audit/events",
  "/health/live",
]) {
  if (!smoke.includes(requiredEvidence)) {
    throw new Error(`Runtime smoke is missing evidence: ${requiredEvidence}`);
  }
}

console.log(
  `Stage 3.15 static acceptance passed (${requiredDirectories.length} directories, ${Object.keys(compose.services).length} services).`,
);
